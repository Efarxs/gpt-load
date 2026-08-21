## Why

对照 any-load 后，本仓库在协议转换完整度上已经领先，但选路层有三处缺口：单 Key 没有并发上限、会话只粘 Key 不粘上游/子分组、会话标识缺少 OpenAI 缓存与 Responses 多轮字段。同时协议转换对使用者不够直观。现在补齐这四项，默认行为保持不变。

## What Changes

- 新增每把 Key 的在途并发上限（全局默认 `0` = 不限，分组可覆盖）。超限的 Key 被跳过并轮询下一把；进程崩溃导致的计数泄漏 MUST 能自动恢复，不得永久占满槽位。
- 渠道亲和性绑定从「Key」扩展为「上游 + Key + 子分组」，绑定记在**入口分组**上。命中后跳过聚合组子组轮询，并重放同一上游；上游列表变更或 Key 不可用时改绑。保留现有 429 冷却与视频 ID 绑定。
- 会话提取在现有 Claude / 请求头 / `conversation_id` 之后，补上 `previous_response_id`、`prompt_cache_key`、顶层 `session_id`；无显式标识时的兜底改为「模型 + 首条 user 消息」的稳定哈希，而不是整段 messages。
- 协议转换**不换引擎、不引入与 `channel_type` 脱钩的上游格式多选**。管理端分组表单与 README 补充路径→格式说明，强调「同协议恒等透传」。识别 Gemini OpenAI 兼容路径 `/v1beta/openai/` 为 Chat Completions。

无 **BREAKING**：新配置默认关闭或为 0，未改配置的分组行为与现在一致。

## Capabilities

### New Capabilities

- `key-concurrency`: 每把上游 Key 的在途并发上限、超限跳过、崩溃后槽位自愈

### Modified Capabilities

- `channel-affinity`: 绑定对象改为上游+Key+子分组并挂在入口组；会话提取优先级与哈希兜底
- `protocol-routing`: Gemini OpenAI 兼容路径识别；管理端/文档的路径→格式说明；禁止引入独立上游格式列表

## Impact

- 后端：`internal/types`、`internal/models` 配置项；`internal/keypool` 并发槽与选 Key；`internal/channel` 按 index 重放上游；`internal/proxy` 入口组亲和解析与重试；`internal/translator` 路径识别；i18n
- 前端：系统设置（并发上限）、分组配置（覆盖项）、分组表单协议转换说明
- 文档：`README_CN.md` / `README.md` 增加面向使用者的协议转换与选路说明
- 不改：CLIProxyAPI 转换器、渠道类型集合、密钥探测、分组暂停、出站清洗
