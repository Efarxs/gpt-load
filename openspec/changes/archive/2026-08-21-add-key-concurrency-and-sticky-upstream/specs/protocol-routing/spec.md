## MODIFIED Requirements

### Requirement: 按入口路径识别客户端协议

当协议转换已开启时，系统 MUST 根据去掉分组前缀后的请求路径识别客户端协议：

- `/v1/chat/completions` 视为 OpenAI Chat Completions
- 路径包含 `/v1beta/openai/` 视为 OpenAI Chat Completions（Gemini 的 OpenAI 兼容层）
- `/v1/responses` 视为 OpenAI Responses
- `/v1/messages` 视为 Anthropic Messages
- Gemini 原生 generateContent / streamGenerateContent 路径视为 Gemini
- `/v1/images/generations`、`/v1/images/edits` 视为 OpenAI Images
- `/v1/videos`、`/v1/videos/generations`、`/v1/videos/edits`、`/v1/videos/extensions`、`/v1/videos/{id}` 以及 `/openai/v1/videos`、`/openai/v1/videos/{id}`、`/openai/v1/videos/{id}/content` 视为 OpenAI / xAI 兼容视频

无法识别为上述协议的路径 MUST 保持透传，不得尝试转换。客户端协议与渠道协议恒等（含 `openai` 渠道对 Images / Videos）时，系统 MUST 不改写协议语义。

#### Scenario: Claude Code 访问 OpenAI 分组

- **WHEN** 协议转换已开启的 `openai` 分组收到 `POST /v1/messages`
- **THEN** 系统 MUST 将请求识别为 Anthropic Messages，并转换成 OpenAI Chat Completions 后再转发

#### Scenario: OpenAI SDK 访问 Anthropic 分组

- **WHEN** 协议转换已开启的 `anthropic` 分组收到 `POST /v1/chat/completions`
- **THEN** 系统 MUST 将请求识别为 OpenAI Chat Completions，并转换成 Anthropic Messages 后再转发

#### Scenario: Gemini OpenAI 兼容路径视为 Chat Completions

- **WHEN** 协议转换已开启的 `anthropic` 分组收到 `POST /v1beta/openai/chat/completions`
- **THEN** 系统 MUST 将请求识别为 OpenAI Chat Completions，并转换成 Anthropic Messages 后再转发

#### Scenario: 未识别的接口不转换

- **WHEN** 协议转换已开启的分组收到 `GET /v1/models` 或 embeddings 等未列入识别表的路径
- **THEN** 系统 MUST 按透明代理转发，不得改写协议

## ADDED Requirements

### Requirement: 协议转换保持按渠道类型转换且说明入口路径

系统 MUST 继续按分组 `channel_type` 决定上游协议，MUST NOT 增加与渠道类型脱钩的「上游格式」多选作为转换目标。管理端 MUST 在分组配置中展示协议转换说明，列出入口路径与客户端格式的对应关系，并说明同协议恒等透传。

#### Scenario: 不出现独立上游格式多选

- **WHEN** 管理员打开标准分组的新建或编辑表单
- **THEN** 系统 MUST NOT 提供用于选择转换目标的上游格式多选列表；转换目标 MUST 仍由该分组的渠道类型决定

#### Scenario: 表单展示路径识别说明

- **WHEN** 管理员查看标准分组表单中的协议转换相关配置
- **THEN** 系统 MUST 展示入口路径与格式对应说明（至少包含 Chat Completions、Responses、Messages、Gemini 原生）
