// 移植自 CLIProxyAPI internal/translator/openai/openai/responses，对照 v7.2.31 (05d1792d)。
package translator

import (
	"encoding/json"
	"fmt"
	"strings"
)

type responsesRequest struct {
	Model     string           `json:"model"`
	Input     json.RawMessage  `json:"input"`
	Stream    bool             `json:"stream,omitempty"`
	MaxOutput int              `json:"max_output_tokens,omitempty"`
	Tools     []map[string]any `json:"tools,omitempty"`
}

func decodeResponsesRequest(body []byte) (*canonicalRequest, error) {
	var raw responsesRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 OpenAI Responses 请求失败: %w", err)
	}
	req := &canonicalRequest{
		Model:     raw.Model,
		Stream:    raw.Stream,
		MaxTokens: raw.MaxOutput,
	}
	req.Messages = parseResponsesInput(raw.Input)
	for _, tool := range raw.Tools {
		name, _ := tool["name"].(string)
		if name == "" {
			if fn, ok := tool["function"].(map[string]any); ok {
				name, _ = fn["name"].(string)
			}
		}
		if name == "" {
			continue
		}
		desc, _ := tool["description"].(string)
		var params json.RawMessage
		if p, ok := tool["parameters"]; ok {
			params, _ = json.Marshal(p)
		}
		req.Tools = append(req.Tools, canonicalTool{
			Name:        name,
			Description: desc,
			Parameters:  params,
		})
	}
	return req, nil
}

func parseResponsesInput(raw json.RawMessage) []canonicalMessage {
	if len(raw) == 0 {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []canonicalMessage{{Role: "user", Content: text}}
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return []canonicalMessage{{Role: "user", Content: strings.TrimSpace(string(raw))}}
	}
	msgs := make([]canonicalMessage, 0, len(items))
	for _, item := range items {
		role, _ := item["role"].(string)
		if role == "" {
			role = "user"
		}
		content := extractAnyText(item["content"])
		if content == "" {
			if t, ok := item["text"].(string); ok {
				content = t
			}
		}
		msgs = append(msgs, canonicalMessage{Role: role, Content: content})
	}
	return msgs
}

func extractAnyText(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case []any:
		var b strings.Builder
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

func encodeResponsesRequest(req *canonicalRequest) ([]byte, error) {
	items := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		items = append(items, map[string]any{
			"type": "message",
			"role": "system",
			"content": []map[string]any{
				{"type": "input_text", "text": req.System},
			},
		})
	}
	for _, msg := range req.Messages {
		items = append(items, map[string]any{
			"type": "message",
			"role": msg.Role,
			"content": []map[string]any{
				{"type": "input_text", "text": msg.Content},
			},
		})
	}
	out := map[string]any{
		"model":  req.Model,
		"input":  items,
		"stream": req.Stream,
	}
	if req.MaxTokens > 0 {
		out["max_output_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			item := map[string]any{
				"type":        "function",
				"name":        tool.Name,
				"description": tool.Description,
			}
			if len(tool.Parameters) > 0 {
				var params any
				_ = json.Unmarshal(tool.Parameters, &params)
				item["parameters"] = params
			}
			tools = append(tools, item)
		}
		out["tools"] = tools
	}
	return json.Marshal(out)
}

type responsesAPIResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Model  string `json:"model"`
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func decodeResponsesResponse(body []byte) (*canonicalResponse, error) {
	var raw responsesAPIResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 OpenAI Responses 响应失败: %w", err)
	}
	resp := &canonicalResponse{
		ID:    raw.ID,
		Model: raw.Model,
		Usage: canonicalUsage{
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
		},
		FinishReason: "stop",
	}
	for _, item := range raw.Output {
		for _, part := range item.Content {
			if part.Text != "" {
				resp.Text += part.Text
			}
		}
	}
	return resp, nil
}

func encodeResponsesResponse(resp *canonicalResponse) ([]byte, error) {
	out := responsesAPIResponse{
		ID:     firstNonEmpty(resp.ID, "resp_translated"),
		Object: "response",
		Model:  resp.Model,
		Status: "completed",
	}
	out.Output = append(out.Output, struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}{
		Type: "message",
		Role: "assistant",
		Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{
			{Type: "output_text", Text: resp.Text},
		},
	})
	out.Usage.InputTokens = resp.Usage.InputTokens
	out.Usage.OutputTokens = resp.Usage.OutputTokens
	return json.Marshal(out)
}
