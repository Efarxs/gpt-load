// 移植自 CLIProxyAPI internal/translator/claude 与 openai/claude，对照 v7.2.31 (05d1792d)。
package translator

import (
	"encoding/json"
	"fmt"
)

type claudeRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Stream      bool            `json:"stream,omitempty"`
	System      json.RawMessage `json:"system,omitempty"`
	Messages    []claudeMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Tools       []claudeTool    `json:"tools,omitempty"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type claudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

func decodeClaudeRequest(body []byte) (*canonicalRequest, error) {
	var raw claudeRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 请求失败: %w", err)
	}
	req := &canonicalRequest{
		Model:       raw.Model,
		MaxTokens:   raw.MaxTokens,
		Stream:      raw.Stream,
		Temperature: raw.Temperature,
		TopP:        raw.TopP,
		System:      rawMessageText(raw.System),
		Messages:    make([]canonicalMessage, 0, len(raw.Messages)),
	}
	for _, msg := range raw.Messages {
		req.Messages = append(req.Messages, canonicalMessage{
			Role:    msg.Role,
			Content: rawMessageText(msg.Content),
		})
	}
	for _, tool := range raw.Tools {
		req.Tools = append(req.Tools, canonicalTool{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
		})
	}
	return req, nil
}

func encodeClaudeRequest(req *canonicalRequest) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	out := claudeRequest{
		Model:       req.Model,
		MaxTokens:   maxTokens,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Messages:    make([]claudeMessage, 0, len(req.Messages)),
	}
	if req.System != "" {
		out.System = mustRawString(req.System)
	}
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "system" {
			if out.System == nil {
				out.System = mustRawString(msg.Content)
			}
			continue
		}
		if role != "user" && role != "assistant" {
			role = "user"
		}
		out.Messages = append(out.Messages, claudeMessage{
			Role:    role,
			Content: mustRawString(msg.Content),
		})
	}
	for _, tool := range req.Tools {
		out.Tools = append(out.Tools, claudeTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.Parameters,
		})
	}
	return json.Marshal(out)
}

type claudeResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func decodeClaudeResponse(body []byte) (*canonicalResponse, error) {
	var raw claudeResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 Anthropic 响应失败: %w", err)
	}
	resp := &canonicalResponse{
		ID:           raw.ID,
		Model:        raw.Model,
		FinishReason: mapClaudeStop(raw.StopReason),
		Usage: canonicalUsage{
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
		},
	}
	for _, block := range raw.Content {
		if block.Type == "text" || block.Text != "" {
			resp.Text += block.Text
		}
	}
	return resp, nil
}

func encodeClaudeResponse(resp *canonicalResponse) ([]byte, error) {
	out := claudeResponse{
		ID:         firstNonEmpty(resp.ID, "msg_translated"),
		Type:       "message",
		Role:       "assistant",
		Model:      resp.Model,
		StopReason: mapToClaudeStop(resp.FinishReason),
	}
	out.Content = append(out.Content, struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}{Type: "text", Text: resp.Text})
	out.Usage.InputTokens = resp.Usage.InputTokens
	out.Usage.OutputTokens = resp.Usage.OutputTokens
	return json.Marshal(out)
}

func mapClaudeStop(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return firstNonEmpty(reason, "stop")
	}
}

func mapToClaudeStop(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return firstNonEmpty(reason, "end_turn")
	}
}
