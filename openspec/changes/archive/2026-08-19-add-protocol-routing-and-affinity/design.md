## Context

现有代理链路见 `internal/proxy/server.go`：解析分组 → 聚合组则加权选子组 → 按 `channel_type` 取渠道适配器 → `KeyProvider.SelectKey` 原子轮询 → 改鉴权头后原样转发。渠道只负责鉴权、流式判断、探活和模型重定向，没有 translator。Key 失败走 `failure_count` + 黑名单 + `CronChecker` 探活，没有按会话绑定，也没有 429 时间冷却。

约束：默认必须保持透明代理；协议转换和亲和性都是分组级开关；不引入 CLIProxyAPI 整仓 Go 依赖；单机内存存储与 Redis 分布式部署都要能工作。需求合同见 `specs/protocol-routing/spec.md` 与 `specs/channel-affinity/spec.md`。

## Goals / Non-Goals

**Goals:**

- 在现有 `HandleProxy` 主循环中插入可选的「识别入口协议 → 转换请求 → 转发 → 转换响应」层，以及可选的「会话 → Key」粘滞选择层。
- 用分组配置承载开关，管理端可改，热更新后对新请求生效（沿用现有 GroupManager 缓存失效）。
- 亲和性开启时为 429 增加进程/集群内时间冷却，到期自动回到可选池。
- 转换器覆盖本项目已有四种渠道的对话互转，以及 OpenAI 规范生图/视频在可映射渠道上的转换。
- 用同步文档把 CLIProxyAPI 对照版本、文件映射和更新步骤固定下来，便于上游后续演进。

**Non-Goals:**

- 不引入 Codex / Antigravity / xAI / Vertex 等本项目没有的渠道类型。
- 不把三个客户端端点做成中间枢纽（不先统一成某种内部协议再转出）。
- 不改造未开启开关时的黑名单阈值、故障转移状态码和定时探活。
- 不转换 `/v1/models`、embeddings、count_tokens 等未列入识别表的接口。
- 不把 CLIProxyAPI 作为运行时 Go 模块依赖；同步是人工对照拷贝 + 适配，不是 submodule 自动跟踪。
- 不实现 CLIProxyAPI 的 xAI 专用视频账号选路；`openai` 分组指到 xAI 兼容上游时只做恒等转发。

## Decisions

### 1. 配置放在分组 `config` JSON，而不是新表字段

在 `GroupConfig` / `types.SystemSettings` 增加：

- `enable_protocol_routing`（bool，默认 `false`）
- `enable_channel_affinity`（bool，默认 `false`）
- `session_affinity_ttl`（string，默认 `"1h"`，Go duration）

**理由：** `groups.config` 已是 JSON 覆盖层，无需迁移脚本；系统级默认关闭，单个分组可覆盖；管理端已有「分组配置项」渲染通道。

**备选：** 独立列。更易检索，但要写 migration，且这两个开关不是查询条件。

聚合组忽略这三项；`HandleProxy` 在选出子组后读取子组的 effective config。

### 2. 协议转换是「入口格式 → 渠道格式」，不是端点互转枢纽

与 CLIProxyAPI 一致：

1. 用去掉 `/proxy/{group}` 后的路径识别 `SourceFormat`（对话 + Images + Videos）
2. 用分组 `channel_type` 得到 `TargetFormat`
3. 查转换表：请求 client→upstream，响应 upstream→client
4. 同格式则跳过转换体，只走原渠道
5. 无转换器则 400，不把不兼容 body 打到上游

转换后必须改写出站路径为渠道原生对话路径（例如 Messages → `openai` 时改为 `/v1/chat/completions`），否则上游会 404。Gemini 还需按模型名拼 `generateContent` / `streamGenerateContent`。

处理顺序：

1. 读 body、参数覆盖（现有）
2. 识别客户端协议；若需转换，先转换请求
3. 现有 `ApplyModelRedirect` / `ModifyRequest` / HeaderRules
4. 出站
5. 若需转换，再转换响应（流式逐事件，非流式整包）

**备选：** 先重定向模型再转换。客户端模型名和上游字段位置不同（Gemini 在 path 里），先转换再重定向更稳。

### 3. 裁剪移植 CLIProxyAPI translator，放到 `internal/translator`

从 `D:\code\go\CLIProxyAPI\internal\translator` 移植本项目四种格式互转所需的 request/response（含 stream / non-stream），用 blank import 注册。thinking / tool call / 图片块只保留转换正确性必需的部分。

生图/视频不走对话 translator 矩阵，而走独立 media converter（见决策 7），源码同样来自 CLIProxyAPI handler 层，而不是 `internal/translator`。

流式不再 `io.Copy`：开启转换时按 SSE 拆事件，转换后再写回客户端。未开启转换时保持现有零拷贝。

**备选：** go.mod 依赖 CLIProxyAPI。模块巨大、版本耦合、会带入无关 runtime，否决。

### 4. 亲和性是套在 `SelectKey` 外的会话缓存

新增 `internal/keypool/affinity.go`（名称可调整）：

- 缓存键：`groupID + sessionID + model`
- 值：Key ID
- TTL：分组配置，命中续期
- 存储：走现有 `store.Store`（内存或 Redis），保证多实例一致

`SelectKey` 增加可选上下文：sessionID、model、是否启用亲和。命中且 Key 仍在活跃池且未冷却 → 返回该 Key，不旋转列表；未命中或不可用 → 走现有 `Rotate`，再写入缓存。

会话提取顺序按 `channel-affinity` 规格，实现参考 CLIProxyAPI `extractSessionIDs`，但缓存键不含 provider——本项目一个分组只有一种渠道。

视频任务另用 `groupID + videoID` 绑定创建 Key；retrieve/content 走这条绑定，原 Key 不可用时返回错误而不是改绑。

**备选：** 改 `Rotate` 本身做粘滞。会破坏现有轮询原子性，否决。

### 5. 429 冷却只在亲和性开启时生效

亲和性开启且上游 429：

- 写 `cooldown:{groupID}:{keyID}`，TTL = `Retry-After` 或默认短退避（首次 1s，可按连续 429 倍增，上限 30s；本项目 Key 多，不必照搬 CLIProxyAPI 的 30 分钟）
- 本次请求按现有 `MaxRetries` 换下一把
- 429 **不计入** `failure_count` / 黑名单（避免限流 Key 被探活任务反复消耗配额）
- 到期后自然可被选中，无需 CronChecker

亲和性关闭：429 仍走现有 failover + 失败计数。

**备选：** 全局 429 冷却。会改变现网默认行为，超出「只搬亲和性」范围。

### 6. 管理端与兼容性

`GroupFormModal` 在标准分组增加两个开关和 TTL 输入；文案走现有 i18n（zh-CN / en-US / ja-JP）。旧分组缺少这些键时视为关闭。

### 7. OpenAI 生图/视频按「可映射才转换」接入

CLIProxyAPI **支持** OpenAI 规范接口，但不是一份独立 OpenAPI YAML，而是 handler 实现：

| 客户端 | CLIProxyAPI 路径 | 它实际怎么转 |
|---|---|---|
| OpenAI Images | `POST /v1/images/generations`、`/v1/images/edits` | 默认真转成 Responses 的 `image_generation` 工具；xAI 模型另转 grok-imagine-image；OpenAI-compat 恒等 |
| OpenAI Videos | `POST /openai/v1/videos` + GET retrieve/content | 主要转成 xAI `grok-imagine-video`，并绑定 video_id→auth |
| xAI Videos | `POST /v1/videos/generations` 等 | xAI 原生表面 |

本项目没有 xAI 渠道类型。因此：

- **Images → `openai`**：恒等转发 `/v1/images/generations`、`/v1/images/edits`（含 multipart edits）
- **Images → `openai-response`**：移植 CLIProxyAPI 的 `buildImagesResponsesRequest` / 从 Responses 收集图片结果，改写出站路径为 `/v1/responses`
- **Images → anthropic/gemini**：首期无转换器，4xx。CLIProxyAPI 也没有 Images→Claude/Gemini 的 translator
- **Videos → `openai`**：恒等转发 `/v1/videos*` 与 `/openai/v1/videos*`
- **Videos → 其他渠道**：4xx
- 亲和开启时，创建视频返回的 `id` / `request_id` 写入绑定缓存；retrieve/content 必须用原 Key，不可用则报错而不是改绑到无关 Key

**备选：** 把 sora-2→grok 整套搬过来并新增 xai 渠道。超出当前渠道模型，否决。

### 8. 用同步文档保证后续可更新

在仓库增加 `docs/translator-sync.md`（实现阶段落地，规划阶段先定结构），作为对照 CLIProxyAPI 更新转换器的唯一入口。文档固定这些内容：

1. **来源**：`github.com/router-for-me/CLIProxyAPI`，记录本仓库上次对照的 tag / commit
2. **映射表**：CLIProxyAPI 路径 → 本仓库路径（对话 translator、Images/Videos converter、测试）
3. **纳入范围**：四种渠道互转 + Images→Responses + OpenAI Images/Videos 恒等路径
4. **排除范围**：Codex / Antigravity / xAI executor、OAuth、插件、整仓 runtime
5. **适配规则**：改 package/import、去掉 cliproxy auth 依赖、错误码走本项目 `app_errors`、中文注释
6. **回归**：必跑的 Go 测试列表
7. **更新步骤**：拉新版本 → 按映射 diff → 只合入纳入范围 → 补测试 → 更新对照 commit → 若新增方向则先改规格再改代码

转换器文件头用简体中文注明「移植自 CLIProxyAPI `<path>`，对照 `<version>`」，方便下次 diff。

**备选：** git submodule 跟踪整个 CLIProxyAPI。会把无关代码和破坏性改动直接带进构建，否决。

## Risks / Trade-offs

- [转换语义损失] → 工具调用、thinking、多模态无法 100% 无损。首期以 CLIProxyAPI 已覆盖的常见字段为准，缺字段时降级为文本/忽略未知块，并在设计测试中锁住主路径。
- [流式转换增加延迟和内存] → 仅开启转换的分组走解析路径；透传分组保持零拷贝。
- [会话哈希兜底可能误粘滞] → 只在完全没有显式 session 头/字段时启用；文档建议客户端带 `X-Session-ID`。
- [Redis 不可用时亲和缓存失败] → 降级为本次轮询，不得阻断代理。
- [移植代码与上游 CLIProxyAPI 分叉] → 用 `docs/translator-sync.md` + 文件头对照版本做人工同步，不自动跟踪。
- [视频改绑导致 404] → 视频 ID 绑定单独处理：原 Key 不可用时返回错误，不轮询其他 Key。
- [Images multipart] → edits 必须走独立读 body，不能假设全是 JSON。

## Migration Plan

1. 先发后端：新配置键缺省为 false，旧分组行为不变。
2. 再发管理端开关；默认关，由管理员按分组打开。
3. 回滚：关掉开关即回到透传/轮询；代码回滚后未知 JSON 键被忽略，无需清库。
4. 无需数据迁移脚本。

## Open Questions

- 管理端是否需要展示「当前会话绑了哪把 Key / 哪把 Key 在冷却」——不影响协议与选路，可后续加到日志或仪表盘。
- Gemini Imagen / Claude 生图若 CLIProxyAPI 日后补了转换器，按同步文档增补方向即可，不在本次实现。
