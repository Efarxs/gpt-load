## Why

当前项目是按分组渠道类型透传的 Key 池代理：客户端必须发送与分组 `channel_type` 完全一致的协议，且同一会话的连续请求会在活跃 Key 之间轮询。这导致 Claude Code、OpenAI SDK、Codex 等客户端无法共用同一分组，thinking / prompt cache / tool call 也容易因换 Key 中断。需要把 CLIProxyAPI 的「入口格式 → 上游格式」协议转换和「会话粘滞同一把 Key」渠道亲和性迁入本项目，同时保持默认透明代理行为不变。

## What Changes

- 为标准分组增加「协议转换」路由开关。默认关闭：请求体与路径仍原样转发。开启后，按客户端入口协议转换成该分组 `channel_type` 对应的上游协议，再把上游响应转回客户端格式。覆盖对话协议（`/v1/chat/completions`、`/v1/responses`、`/v1/messages`、Gemini 原生路径）以及 CLIProxyAPI 已实现的 **OpenAI 规范生图/视频**：`/v1/images/generations`、`/v1/images/edits`、`/v1/videos`、`/openai/v1/videos`（含 retrieve / content）。
- 为标准分组增加「渠道亲和性」开关。默认关闭：Key 仍按现有原子轮询选择。开启后，同一会话尽量绑定同一把活跃 Key；绑定 Key 不可用（拉黑、429 冷却）时自动改绑并换 Key 重试。视频创建成功后，后续按 `video_id` / `request_id` 查询或拉内容 MUST 粘滞到创建时的 Key。
- 亲和性开启时，429 按时间冷却（优先上游 `Retry-After`，否则短退避），到期自动回到可选池，不再只依赖失败计数 + 定时探活。
- 管理端分组表单增加上述开关与亲和性 TTL 配置；聚合分组本身不转换协议，是否转换、是否亲和由选中的子分组决定。
- 协议转换代码按「可再同步」方式裁剪移植 CLIProxyAPI，并提供同步文档与清单，便于上游后续新增转换方向时更新，而不是一次性抄死。
- 未开启对应开关的分组行为与现网完全一致，不视为 **BREAKING**。

## Capabilities

### New Capabilities

- `protocol-routing`: 分组级协议转换路由。仅当分组开启时，在客户端协议与分组渠道协议之间转换请求和响应（含流式），覆盖对话以及 OpenAI 规范的生图/视频接口；转换器必须可对照 CLIProxyAPI 再同步。
- `channel-affinity`: 分组级渠道亲和性。将会话粘滞到同一把 Key，并在 Key 不可用时失效重绑；配合 429 时间冷却自动恢复；视频任务 ID 粘滞到创建 Key。

### Modified Capabilities

- 无。仓库主规格目录目前为空，本次只引入新能力。

## Impact

- 代理主循环：`internal/proxy/server.go` 及流式/非流式响应处理。
- 分组配置：`models.Group` / `GroupConfig`、分组创建/更新校验、管理端 `GroupFormModal` 与 i18n。
- Key 选择：`internal/keypool/provider.go` 的 `SelectKey` 需支持会话粘滞与冷却跳过。
- 新增协议转换包（裁剪移植 CLIProxyAPI 的 translator 与 Images/Videos handler 转换逻辑，不引入其整仓依赖），以及 `docs/translator-sync.md` 同步方案。
- 现有透明代理、模型重定向、故障转移状态码、黑名单探活保持兼容。转换发生在识别入口协议之后、模型重定向与出站鉴权之前。
