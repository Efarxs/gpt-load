## Context

现状见 `internal/proxy/server.go`：先选子组，再读 body，再协议转换，再按子组做 Key 亲和（只存 Key ID）。上游每次 `BuildUpstreamURL` 重新加权。`internal/store` 已有 `HIncrBy` 与带 TTL 的 `Set`，没有字符串 INCR。动机见 `proposal.md`；行为合同见本变更下三份 delta spec。

## Goals / Non-Goals

**Goals:**

- 在现有 `GroupConfig` JSON 覆盖层上增加 `max_concurrency_per_key`，默认 0，热更新生效。
- 在途槽位用现有 store 原语实现，崩溃后靠 TTL 自愈，不新增 Redis 模块。
- `HandleProxy` 改为：读 body → 按入口组查绑定 → 命中则跳过子组轮询并重放上游。
- 绑定值从「Key ID 字符串」升级为 JSON（Key + 上游 + 子组）；旧值解析失败即视为未命中。
- 会话提取与路径识别按 spec 扩展；分组表单与 README 补说明。

**Non-Goals:**

- 不换 CLIProxyAPI 转换器，不引入 `upstream_formats` 多选。
- 不新增渠道类型，不改 429 冷却与视频 ID 绑定语义。
- 不把并发上限做成对客户端的排队等待（满则换 Key 或 503，不等槽）。
- 不扩展 `Store` 接口（避免 Memory/Redis 双实现分叉）。

## Decisions

### 1. 并发配置走现有 settings 反射，不建新表

`types.SystemSettings` 与 `models.GroupConfig` 增加 `max_concurrency_per_key`（int，默认 0）。分类 `config.category.routing`，与亲和性开关并列。聚合组忽略该覆盖，有效值来自实际服务的标准组。

**备选：** 独立 DB 列。无查询需求，否决。

### 2. 在途计数 = `HIncrBy` + 旁路 TTL 键

- 计数：`group:{gid}:inflight` HASH，field = Key ID，`HIncrBy ±1`
- 存活：`group:{gid}:inflight:exp:{kid}`，`Set` 值为 `1`，TTL = `max(RequestTimeout, 60s)`；每次成功 Acquire 刷新
- Acquire：若 exp 键不存在，先把 field 置 0 再 +1（崩溃泄漏收回）
- `maxConc <= 0` 时不碰 store
- 请求结束 `defer Release`；Release 后计数 < 0 则归零

**备选 A：** 给 Store 加 `Incr(key, ttl)`。更干净，但要改 Memory/Redis 两套，超出本变更。  
**备选 B：** 只用 HASH、靠重启清内存。Redis 部署会永久泄漏，否决。

亲和命中但在途满：本次改选其他 Key，**不删绑定**（与 429 冷却改绑区分）。

### 3. 绑定 JSON 记在入口组，HandleProxy 提前读 body

缓存键保持 `affinity:bind:{entryGroupID}:{sessionID}:{model}`。值改为：

```json
{"key_id":1,"upstream_idx":0,"base_url":"https://api.example.com","sub_group":"child"}
```

标准组 `sub_group` 为空。旧缓存若不是 JSON，当作未命中并重绑。

`HandleProxy` 顺序：

1. 解析入口组；暂停检查不变
2. 读 body
3. 提会话 ID；用入口组 ID 读绑定
4. 命中且子组仍开启亲和、上游 `BaseURL` 一致、Key 可用 → 用绑定子组，跳过 `SelectSubGroup`
5. 否则冷路径：现有子组选择 → 子组开关决定是否写入绑定
6. 选 Key（可带并发）+ `BuildUpstreamURLAt` 或加权选择
7. `retryCount == 0` 才写/续期绑定；重试不覆盖

渠道接口在 `BaseChannel` 增加 `BuildUpstreamURLAt` 与 `UpstreamBaseURL`；`BuildUpstreamURL` 改为同时返回选中的 index。这是进程内接口，无对外 API。

**备选：** 绑定仍挂在子组 ID 上。聚合组无法跳过子组轮询，否决。

### 4. 会话提取只改 `ExtractSessionID`，不换包

继续在 `internal/keypool/session.go` 扩展优先级（见 channel-affinity spec）。哈希兜底改为 `sha256(model + NUL + 首条 user 文本)`，覆盖 OpenAI `messages`、Anthropic `messages`、Gemini `contents` 的首条 user。Claude `metadata.user_id` 仍最高优先。

**备选：** 抽成 any-load 式独立 `affinity` 包。本仓库亲和已在 keypool，再拆包无收益。

### 5. 协议转换只加路径与文案

`DetectFromPath`：含 `/v1beta/openai/` 且尚未命中其他规则时视为 Chat Completions（放在 `:generateContent` 之前，避免误伤）。目标仍是 `FormatFromChannel`。

管理端：`enable_protocol_routing` 的 i18n 说明补路径表；`GroupFormModal` 在该配置旁展示只读说明，不新增表单字段。README_CN/README 增加使用者向小节。

## Risks / Trade-offs

- [HIncrBy 与 TTL 键非原子] → 极端并发下可能多放行 1 个槽位；上限是保护而非硬配额，可接受
- [TTL 短于超长流式] → TTL 至少 60s 且跟 RequestTimeout；流式结束仍 defer Release
- [旧亲和缓存不兼容] → 解析失败即冷启动，TTL 内最多丢一次粘滞
- [聚合组会话钉死子组] → 该子组暂停或无 Key 时走现有改绑/暂停错误，不静默换组

## Migration Plan

1. 先发后端：默认 `max_concurrency_per_key=0`，未开亲和的流量不变
2. 亲和开启的实例：滚动升级时旧绑定自然失效并重写 JSON，无需脚本
3. 回滚：回退二进制后新 JSON 绑定被当成无效，退回只粘 Key 的旧逻辑；并发上限字段被忽略

## Open Questions

无。在途 TTL 取 `max(RequestTimeout, 60s)`，不另开配置项。
