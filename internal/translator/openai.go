// 移植自 CLIProxyAPI internal/translator/openai 与 claude/openai/chat-completions，对照 v7.2.31 (05d1792d)。
package translator

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

func decodeOpenAIRequest(body []byte) (*canonicalRequest, error) {
	var raw openAIChatRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 请求失败: %w", err)
	}
	req := &canonicalRequest{
		Model:       raw.Model,
		MaxTokens:   raw.MaxTokens,
		Stream:      raw.Stream,
		Temperature: raw.Temperature,
		TopP:        raw.TopP,
		Messages:    make([]canonicalMessage, 0, len(raw.Messages)),
	}
	for _, msg := range raw.Messages {
		text := rawMessageText(msg.Content)
		if msg.Role == "system" && req.System == "" {
			req.System = text
			continue
		}
		cm := canonicalMessage{
			Role:       msg.Role,
			Content:    text,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		for _, tc := range msg.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, canonicalToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		req.Messages = append(req.Messages, cm)
	}
	for _, tool := range raw.Tools {
		req.Tools = append(req.Tools, canonicalTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return req, nil
}

func encodeOpenAIRequest(req *canonicalRequest) ([]byte, error) {
	out := openAIChatRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Messages:    make([]openAIMessage, 0, len(req.Messages)+1),
	}
	if req.System != "" {
		out.Messages = append(out.Messages, openAIMessage{
			Role:    "system",
			Content: mustRawString(req.System),
		})
	}
	for _, msg := range req.Messages {
		om := openAIMessage{
			Role:       msg.Role,
			Content:    mustRawString(msg.Content),
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		for _, tc := range msg.ToolCalls {
			item := openAIToolCall{ID: tc.ID, Type: "function"}
			item.Function.Name = tc.Name
			item.Function.Arguments = tc.Arguments
			om.ToolCalls = append(om.ToolCalls, item)
		}
		out.Messages = append(out.Messages, om)
	}
	for _, tool := range req.Tools {
		item := openAITool{Type: "function"}
		item.Function.Name = tool.Name
		item.Function.Description = tool.Description
		item.Function.Parameters = tool.Parameters
		out.Tools = append(out.Tools, item)
	}
	return json.Marshal(out)
}

type openAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage,omitempty"`
}

type openAIChoice struct {
	Index        int               `json:"index"`
	FinishReason string            `json:"finish_reason"`
	Message      openAIOutMessage  `json:"message"`
	Delta        *openAIOutMessage `json:"delta,omitempty"`
}

type openAIOutMessage struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func decodeOpenAIResponse(body []byte) (*canonicalResponse, error) {
	var raw openAIChatResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 响应失败: %w", err)
	}
	resp := &canonicalResponse{
		ID:    raw.ID,
		Model: raw.Model,
		Usage: canonicalUsage{
			InputTokens:  raw.Usage.PromptTokens,
			OutputTokens: raw.Usage.CompletionTokens,
		},
	}
	if len(raw.Choices) > 0 {
		choice := raw.Choices[0]
		resp.Text = choice.Message.Content
		resp.FinishReason = choice.FinishReason
		for _, tc := range choice.Message.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, canonicalToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}
	return resp, nil
}

func encodeOpenAIResponse(resp *canonicalResponse) ([]byte, error) {
	out := openAIChatResponse{
		ID:      firstNonEmpty(resp.ID, "chatcmpl-translated"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   resp.Model,
		Choices: []openAIChoice{{
			Index:        0,
			FinishReason: firstNonEmpty(resp.FinishReason, "stop"),
			Message: openAIOutMessage{
				Role:    "assistant",
				Content: resp.Text,
			},
		}},
		Usage: openAIUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
		},
	}
	for _, tc := range resp.ToolCalls {
		item := openAIToolCall{ID: tc.ID, Type: "function"}
		item.Function.Name = tc.Name
		item.Function.Arguments = tc.Arguments
		out.Choices[0].Message.ToolCalls = append(out.Choices[0].Message.ToolCalls, item)
	}
	return json.Marshal(out)
}

func rawMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if t, ok := part["text"].(string); ok {
				b.WriteString(t)
			}
		}
		return b.String()
	}
	return strings.Trim(string(raw), `"`)
}

func mustRawString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
