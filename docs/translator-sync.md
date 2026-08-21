# 协议转换器同步说明

本仓库的协议转换从 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) **完整拷贝**对话转换器（不删减字段/事件），不把它作为 Go 模块依赖。用 `python scripts/vendor_cliproxy_translator.py` 同步。

## 对照版本

| 项 | 值 |
|---|---|
| 来源仓库 | `github.com/router-for-me/CLIProxyAPI` |
| 对照 tag | `v7.2.31` |
| 对照 commit | `05d1792d` |
| 同步日期 | 2026-08-19 |

更新转换器后，必须同时改这张表。

## 文件映射

| CLIProxyAPI | 本仓库 | 说明 |
|---|---|---|
| `internal/translator/{claude,openai,gemini}/**` | `internal/cliproxy/translator/**` | 完整拷贝，只改 import |
| `sdk/api/handlers/openai/openai_images_handlers.go` | `internal/translator/images.go` `multipart.go` | Images → Responses `image_generation` |
| `sdk/api/handlers/openai/openai_videos_handlers.go` | `internal/translator/format.go` + 恒等转发 | 本项目无 xAI 渠道，只在 `openai` 分组恒等转发 |
| `sdk/cliproxy/auth/selector.go` 会话提取 | `internal/keypool/session.go` | 会话 ID 优先级对齐 |
| — | `internal/proxy/protocol_routing.go` | 分组开关与路径改写 |
| — | `internal/keypool/affinity.go` | 会话粘滞与 429 冷却 |

## 纳入范围

- 对话：OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini generateContent
- 生图：`/v1/images/generations`、`/v1/images/edits`（`openai` 恒等，`openai-response` 转 Responses 工具）
- 视频：`/v1/videos*`、`/openai/v1/videos*`（仅 `openai` 渠道恒等）
- 流式 SSE：完整 Anthropic 事件序（message_start / content_block_* / message_delta / message_stop），含 thinking、tool_use

## 排除范围

- Codex / Antigravity / xAI / Vertex 渠道与 executor
- OAuth、插件、整仓 runtime
- sora-2 → grok-imagine-video 的专用转换（除非本项目新增 xAI 渠道）
- CLIProxyAPI 的 go.mod 依赖

## 适配规则

1. package 一律为 `translator` 或 `keypool` / `proxy`，不要保留 `github.com/router-for-me/CLIProxyAPI` 导入
2. 错误走 `translator.ConvertError` 与本项目 `app_errors`
3. 注释使用简体中文；文件头注明来源路径与对照版本
4. 分组开关默认关闭，同步转换器不得改变默认值

## 回归命令

```bash
go test ./internal/translator/ ./internal/proxy/ ./internal/keypool/ ./internal/config/ -count=1
```

至少覆盖：

- Messages ↔ Chat Completions（含一条流式）
- Responses ↔ Messages
- Chat Completions ↔ Gemini
- Images → Responses，以及 Images/Videos 打到无转换器渠道返回 4xx
- 协议转换关闭时透传
- 亲和性粘滞、429 冷却、视频 ID 绑定

## 更新步骤

1. 在 CLIProxyAPI 仓库 `git fetch && git checkout <新 tag>`
2. 对照上表做 diff，只合入「纳入范围」
3. 若新增客户端协议或渠道方向：先改 `openspec` 规格，再改 `DetectFromPath` / `SupportsConversion` / 编解码
4. 补测试，跑回归命令
5. 更新本文「对照版本」和相关文件头
6. 不要顺手打开分组默认开关

## 文件头模板

```go
// 移植自 CLIProxyAPI <path>，对照 v7.2.31 (05d1792d)。
```
