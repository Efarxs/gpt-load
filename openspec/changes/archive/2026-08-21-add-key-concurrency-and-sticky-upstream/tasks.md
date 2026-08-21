## 1. 每 Key 并发上限配置

- [x] 1.1 在 `types.SystemSettings` 与 `models.GroupConfig` 增加 `max_concurrency_per_key`（int，默认 0），归入 routing 分类
- [x] 1.2 补齐 zh-CN / en-US / ja-JP 的设置名称与说明；确认聚合组不展示该项覆盖
- [x] 1.3 确认未配置或为 0 时 effective config 不限制并发，热更新后对新请求生效

## 2. 在途槽位与选 Key

- [x] 2.1 实现 `AcquireKey` / `ReleaseKey`：`HIncrBy` 计数 + `inflight:exp` TTL 键（`max(RequestTimeout, 60s)`）；`maxConc<=0` 不碰 store；Release 后负数归零
- [x] 2.2 实现带并发的选 Key：优先尝试亲和 Key；在途满则跳过并轮询；全部打满返回现有无可用密钥错误；亲和 Key 仅在途满时不删绑定
- [x] 2.3 单测：不限制时行为不变、打满改选、全部打满失败、exp 键过期后槽位收回、Release 防负数、亲和满不拆绑定

## 3. 会话提取

- [x] 3.1 扩展 `ExtractSessionID`：在现有 Claude / 请求头之后增加 `previous_response_id`、`prompt_cache_key`、`conversation_id`、顶层与 `metadata.session_id`
- [x] 3.2 无显式标识时改为 `sha256(模型 + NUL + 首条 user 文本)` 兜底，覆盖 Chat / Messages / Gemini contents
- [x] 3.3 单测：优先级顺序、Responses 多轮字段、多轮后首条 user 不变仍同哈希、无法提取时返回空

## 4. 上游重放与绑定结构

- [x] 4.1 `ChannelProxy` / `BaseChannel` 增加 `BuildUpstreamURLAt`、`UpstreamBaseURL`；`BuildUpstreamURL` 返回选中的上游 index
- [x] 4.2 绑定值改为 JSON（`key_id`、`upstream_idx`、`base_url`、`sub_group`），键仍为入口组 ID + session + model；非 JSON 旧值视为未命中
- [x] 4.3 单测：同会话重放同一上游、上游列表变更后改绑、旧字符串绑定价视为未命中

## 5. 代理主循环

- [x] 5.1 `HandleProxy` 提前读 body：按入口组查绑定；命中且子组仍开亲和、上游/Key 有效则跳过子组轮询
- [x] 5.2 冷路径保持现有子组选择；仅当实际服务的标准组开启亲和时写入绑定；`retryCount==0` 才写/续期
- [x] 5.3 每次尝试 `defer ReleaseKey`；选上游失败时释放已占用槽位
- [x] 5.4 单测或代理层测试：聚合组二次请求不换子组、多上游不换 URL、绑定子组暂停时返回暂停错误而不是静默换组

## 6. 协议转换路径与说明

- [x] 6.1 `DetectFromPath` 将含 `/v1beta/openai/` 的路径识别为 Chat Completions（排在 Gemini 原生 generateContent 之前）
- [x] 6.2 单测：该路径在转换开启的 anthropic 分组会按 Chat Completions 处理；`/v1/models` 仍不转换
- [x] 6.3 分组表单在协议转换配置旁展示只读路径→格式说明，不增加上游格式多选
- [x] 6.4 更新 `enable_protocol_routing` 的 i18n 说明，以及 README_CN / README 的使用者向协议转换与选路小节

## 7. 回归

- [x] 7.1 跑 `go test ./internal/keypool/ ./internal/proxy/ ./internal/translator/ ./internal/config/ ./internal/channel/ -count=1`
- [x] 7.2 确认未改配置时：无并发限制、亲和关闭仍轮询、协议转换关闭仍透传
