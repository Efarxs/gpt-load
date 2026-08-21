## Purpose

让标准分组按需把客户端协议转换成该分组渠道的上游协议，并在返回时还原为客户端格式。覆盖对话以及 OpenAI 规范的生图/视频接口；未开启时继续透明转发，转换器可对照 CLIProxyAPI 再同步。

## ADDED Requirements

### Requirement: 协议转换默认关闭

系统 MUST 将分组的协议转换视为默认关闭。关闭时，代理 MUST 保持现有透明转发：不改写请求路径语义、不转换请求体或响应体协议。

#### Scenario: 未开启时按原协议透传

- **WHEN** 客户端向未开启协议转换的 `openai` 分组发送 `POST /v1/messages`
- **THEN** 系统 MUST 将请求按原路径和原请求体转发到上游，不得改写成 `/v1/chat/completions`

#### Scenario: 关闭后与现网行为一致

- **WHEN** 客户端向未开启协议转换的分组发送该渠道的原生请求
- **THEN** 系统 MUST 按现有透明代理行为转发，且响应协议与上游一致

### Requirement: 分组级协议转换开关

系统 MUST 允许在标准分组上独立开启或关闭协议转换。聚合分组本身 MUST NOT 持有独立的协议转换开关；其一次请求是否转换 MUST 由被选中的子分组配置决定。

#### Scenario: 仅开启的分组执行转换

- **WHEN** 管理员开启分组 A 的协议转换并保持分组 B 关闭
- **THEN** 系统 MUST 只对发往分组 A 的请求执行协议转换，发往分组 B 的请求 MUST 保持透传

#### Scenario: 聚合组跟随子分组

- **WHEN** 客户端请求打到聚合分组，且被选中的子分组已开启协议转换
- **THEN** 系统 MUST 按该子分组的渠道类型执行协议转换

#### Scenario: 聚合组选中未开启的子分组

- **WHEN** 客户端请求打到聚合分组，且被选中的子分组未开启协议转换
- **THEN** 系统 MUST 对该请求保持透传

### Requirement: 按入口路径识别客户端协议

当协议转换已开启时，系统 MUST 根据去掉分组前缀后的请求路径识别客户端协议：

- `/v1/chat/completions` 视为 OpenAI Chat Completions
- `/v1/responses` 视为 OpenAI Responses
- `/v1/messages` 视为 Anthropic Messages
- Gemini 原生 generateContent / streamGenerateContent 路径视为 Gemini
- `/v1/images/generations`、`/v1/images/edits` 视为 OpenAI Images
- `/v1/videos`、`/v1/videos/generations`、`/v1/videos/edits`、`/v1/videos/extensions`、`/v1/videos/{id}` 以及 `/openai/v1/videos`、`/openai/v1/videos/{id}`、`/openai/v1/videos/{id}/content` 视为 OpenAI / xAI 兼容视频

无法识别为上述协议的路径 MUST 保持透传，不得尝试转换。

#### Scenario: Claude Code 访问 OpenAI 分组

- **WHEN** 协议转换已开启的 `openai` 分组收到 `POST /v1/messages`
- **THEN** 系统 MUST 将请求识别为 Anthropic Messages，并转换成 OpenAI Chat Completions 后再转发

#### Scenario: OpenAI SDK 访问 Anthropic 分组

- **WHEN** 协议转换已开启的 `anthropic` 分组收到 `POST /v1/chat/completions`
- **THEN** 系统 MUST 将请求识别为 OpenAI Chat Completions，并转换成 Anthropic Messages 后再转发

#### Scenario: 未识别的接口不转换

- **WHEN** 协议转换已开启的分组收到 `GET /v1/models` 或 embeddings 等未列入识别表的路径
- **THEN** 系统 MUST 按透明代理转发，不得改写协议

### Requirement: 按分组渠道类型转换上下游协议

当协议转换已开启且识别到客户端对话协议时，系统 MUST 把请求转换成该分组 `channel_type` 对应的上游协议，并把上游响应转换回客户端协议。支持的客户端协议与渠道类型组合为：

| 客户端协议 | openai | openai-response | anthropic | gemini |
|---|---|---|---|---|
| Chat Completions | 恒等（只保证可转发） | 转换 | 转换 | 转换 |
| Responses | 转换 | 恒等（只保证可转发） | 转换 | 转换 |
| Messages | 转换 | 转换 | 恒等（只保证可转发） | 转换 |
| Gemini 原生 | 转换 | 转换 | 转换 | 恒等（只保证可转发） |
| OpenAI Images | 恒等 | 转为 Responses 的 `image_generation` 工具调用 | 无转换器则 4xx | 无转换器则 4xx |
| OpenAI / xAI Videos | 恒等 | 无转换器则 4xx | 无转换器则 4xx | 无转换器则 4xx |

客户端协议与渠道协议相同时，系统 MUST 不改变协议语义。缺少对应转换器时，系统 MUST 向客户端返回 4xx 错误，且 MUST NOT 把不兼容的请求体原样打到上游。

#### Scenario: Messages 转到 Chat Completions

- **WHEN** 开启协议转换的 `openai` 分组收到合法的 `/v1/messages` 请求
- **THEN** 系统 MUST 向上游发送 Chat Completions 格式的请求体和对应路径，并向客户端返回 Anthropic Messages 格式的响应

#### Scenario: Responses 转到 Messages

- **WHEN** 开启协议转换的 `anthropic` 分组收到合法的 `/v1/responses` 请求
- **THEN** 系统 MUST 向上游发送 Anthropic Messages 格式的请求，并向客户端返回 OpenAI Responses 格式的响应

#### Scenario: 同协议不改写语义

- **WHEN** 开启协议转换的 `openai` 分组收到 `/v1/chat/completions` 请求
- **THEN** 系统 MUST 按 OpenAI Chat Completions 语义转发，不得把它改成其他协议

### Requirement: 流式与非流式均需还原客户端协议

协议转换 MUST 同时覆盖非流式和流式响应。客户端以流式发起时，系统 MUST 保持流式，并把上游事件流转换成客户端协议的事件流。

#### Scenario: 流式 Messages 访问 OpenAI 分组

- **WHEN** 开启协议转换的 `openai` 分组收到 `stream=true` 的 `/v1/messages` 请求
- **THEN** 系统 MUST 向上游发起流式 Chat Completions 请求，并向客户端输出 Anthropic SSE 事件

#### Scenario: 非流式 Chat Completions 访问 Anthropic 分组

- **WHEN** 开启协议转换的 `anthropic` 分组收到非流式 `/v1/chat/completions` 请求
- **THEN** 系统 MUST 向上游发送非流式 Messages 请求，并向客户端返回单个 Chat Completions JSON 响应

### Requirement: 转换后仍应用现有出站处理

协议转换 MUST 发生在识别客户端协议之后、发送到上游之前。转换后的上游请求 MUST 继续走现有渠道鉴权、模型重定向、参数覆盖和自定义请求头规则。

#### Scenario: 转换后仍替换上游 Key

- **WHEN** 开启协议转换的分组完成请求转换并准备转发
- **THEN** 系统 MUST 使用该分组选出的上游 API Key 按渠道类型设置鉴权头，不得把客户端代理密钥转给上游

#### Scenario: 转换后仍执行模型重定向

- **WHEN** 开启协议转换的分组存在模型重定向规则，且转换后的上游请求包含匹配的模型名
- **THEN** 系统 MUST 按现有规则改写上游模型名后再发送

### Requirement: OpenAI 生图接口可按渠道转换

当协议转换已开启时，系统 MUST 把 `POST /v1/images/generations` 与 `POST /v1/images/edits` 识别为 OpenAI Images 协议。`openai` 渠道 MUST 按该路径恒等转发。`openai-response` 渠道 MUST 把生图/改图请求转换成 Responses 协议中的 `image_generation` 工具调用，并把上游响应还原为 OpenAI Images 响应（`data[].b64_json` 或 `url`）。`anthropic`、`gemini` 在缺少转换器时 MUST 返回 4xx，且 MUST NOT 把 Images 请求体原样打到 Messages / generateContent。

`/v1/images/edits` MUST 同时接受 `application/json` 与 `multipart/form-data`。

#### Scenario: Images 打到 openai 分组恒等转发

- **WHEN** 开启协议转换的 `openai` 分组收到合法的 `POST /v1/images/generations`
- **THEN** 系统 MUST 按 `/v1/images/generations` 和 OpenAI Images 请求体转发上游，并向客户端返回 OpenAI Images 响应

#### Scenario: Images 打到 openai-response 分组转为 Responses 工具

- **WHEN** 开启协议转换的 `openai-response` 分组收到合法的 `POST /v1/images/generations`
- **THEN** 系统 MUST 向上游发送带 `image_generation` 工具的 Responses 请求，并向客户端返回 OpenAI Images 格式响应

#### Scenario: Images 打到无转换器渠道返回 4xx

- **WHEN** 开启协议转换的 `anthropic` 分组收到 `POST /v1/images/generations`
- **THEN** 系统 MUST 返回 4xx，且 MUST NOT 把该请求体作为 `/v1/messages` 发给上游

### Requirement: OpenAI 规范视频接口可按渠道转发或拒绝

当协议转换已开启时，系统 MUST 识别 OpenAI Videos（`/openai/v1/videos` 及其 retrieve / content）以及 `/v1/videos` 系列路径。`openai` 渠道 MUST 按对应路径恒等转发。其他渠道在缺少转换器时 MUST 返回 4xx。系统 MUST NOT 引入本项目没有的 xAI 专用渠道类型；若 `openai` 分组的上游本身是 xAI 兼容地址，则走恒等转发即可。

#### Scenario: OpenAI Videos 打到 openai 分组恒等转发

- **WHEN** 开启协议转换的 `openai` 分组收到合法的 `POST /openai/v1/videos` 或 `POST /v1/videos`
- **THEN** 系统 MUST 按原视频路径和请求体转发上游，并向客户端返回对应的视频资源 JSON

#### Scenario: Videos 打到无转换器渠道返回 4xx

- **WHEN** 开启协议转换的 `anthropic` 分组收到 `POST /openai/v1/videos`
- **THEN** 系统 MUST 返回 4xx，且 MUST NOT 把该请求体作为 Messages 发给上游

### Requirement: 转换器可对照 CLIProxyAPI 再同步

系统 MUST 将协议转换实现为可再同步的独立模块，并提供同步文档。文档 MUST 记录：CLIProxyAPI 来源路径与对照版本、本仓库文件映射、纳入/排除范围、导入适配规则、回归测试命令，以及上游新增转换方向时的更新步骤。后续同步 MUST 能在不改动分组开关语义的前提下替换或增补转换器。

#### Scenario: 同步文档可独立执行

- **WHEN** 维护者拿到一份更新后的 CLIProxyAPI
- **THEN** 系统提供的同步文档 MUST 给出可执行的对照步骤，使其能定位新增或变更的转换器并合入本仓库

#### Scenario: 同步不改变开关默认值

- **WHEN** 仅更新转换器实现或新增一对转换方向
- **THEN** 未开启协议转换的分组 MUST 仍保持透传，已开启分组 MUST 继续使用原开关
