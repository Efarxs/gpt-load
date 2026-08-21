## 1. 分组配置与管理端

- [x] 1.1 在 `types.SystemSettings` 与 `models.GroupConfig` 增加 `enable_protocol_routing`、`enable_channel_affinity`、`session_affinity_ttl`，默认分别为 false / false / `"1h"`
- [x] 1.2 补齐分组配置校验与 i18n（zh-CN / en-US / ja-JP），非法 TTL 拒绝保存
- [x] 1.3 标准分组表单增加协议转换开关、渠道亲和性开关和 TTL 输入；聚合分组不展示这三项
- [x] 1.4 确认旧分组缺少这些键时 effective config 视为关闭，创建/更新后 GroupManager 缓存能读到新值

## 2. 协议转换器

- [x] 2.1 新增 `internal/translator`，裁剪移植 CLIProxyAPI 中 openai / openai-response / anthropic / gemini 互转所需的请求与响应转换（含 stream / non-stream），不引入 CLIProxyAPI 模块依赖
- [x] 2.2 实现入口路径识别与 `channel_type` → 上游格式映射；未列入识别表的路径返回「不转换」
- [x] 2.3 实现转换后的上游路径改写（Chat Completions / Responses / Messages / Gemini generateContent 或 streamGenerateContent / Images / Videos）
- [x] 2.4 缺少转换器时返回 4xx，不得把不兼容请求体发给上游
- [x] 2.5 为至少 Messages↔Chat Completions、Responses↔Messages、Chat Completions↔Gemini 编写请求/响应（含一条流式）单测
- [x] 2.6 移植 OpenAI Images：`openai` 恒等；`openai-response` 转为 Responses `image_generation` 并还原 Images 响应；edits 支持 JSON 与 multipart
- [x] 2.7 接入 OpenAI / xAI 兼容视频路径：`openai` 渠道恒等转发 create/retrieve/content；其他渠道 4xx
- [x] 2.8 为 Images→Responses 与 Images/Videos 恒等、无转换器 4xx 编写单测

## 3. 代理主循环接入协议转换

- [x] 3.1 在 `HandleProxy` 中于参数覆盖之后、选 Key / 出站之前读取子分组 `enable_protocol_routing`，关闭则走原透传
- [x] 3.2 开启时转换请求体并改写出站路径，随后继续走模型重定向、渠道鉴权与 HeaderRules
- [x] 3.3 开启转换时对流式按 SSE 事件转换后写回客户端，非流式整包转换；关闭时保持现有零拷贝
- [x] 3.4 补充代理层测试：开关关闭透传 `/v1/messages` 到 openai 分组；开关开启则改写为 Chat Completions 并还原 Messages 响应
- [x] 3.5 补充代理层测试：开启转换时 `/v1/images/generations` 打到 `openai-response` 分组会改写为 `/v1/responses`；打到 `anthropic` 返回 4xx

## 4. 渠道亲和性与 429 冷却

- [x] 4.1 实现会话标识提取（Claude session、`X-Session-ID`、`Session-Id`、`X-Client-Request-Id`、`conversation_id`、消息哈希兜底）
- [x] 4.2 基于现有 `store.Store` 实现会话绑定缓存（键：groupID+sessionID+model，TTL 可续期）与 429 冷却键（`Retry-After` 或短退避，上限 30s）
- [x] 4.3 扩展选 Key：亲和关闭保持 Rotate；开启则命中可用绑定不旋转，未命中或 Key 不可用（拉黑/冷却/不在活跃池）则 Rotate 并改绑；缓存失败降级为本次轮询
- [x] 4.4 亲和开启且上游 429 时写入冷却、本次重试换 Key，且不增加 `failure_count`；亲和关闭时 429 保持原黑名单逻辑
- [x] 4.5 编写亲和性测试：同会话粘滞、跨会话隔离、TTL 过期重绑、拉黑改绑、429 冷却跳过与到期恢复、关闭时仍轮询
- [x] 4.6 实现视频 ID → 创建 Key 绑定；retrieve/content 必须用原 Key，原 Key 不可用时返回错误而不是改绑

## 5. 回归与文档

- [x] 5.1 确认聚合组使用子分组开关，父分组配置不影响转换与亲和
- [x] 5.2 跑通现有 keypool / proxy / group 相关测试，并更新 README_CN 中关于可选协议转换、生图/视频与渠道亲和性的说明
- [x] 5.3 撰写 `docs/translator-sync.md`：记录 CLIProxyAPI 对照版本、文件映射、纳入/排除范围、适配规则、回归命令和后续更新步骤
- [x] 5.4 在已移植转换器文件头用简体中文注明来源路径与对照版本
