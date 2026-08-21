package channel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
)

const maxValidationSnippet = 240

// requireValidationContent 是否校验探测响应正文。配置未加载时默认开启。
func requireValidationContent(group *models.Group) bool {
	if group == nil {
		return true
	}
	cfg := group.EffectiveConfig
	if cfg.AppUrl == "" && cfg.KeyValidationTimeoutSeconds == 0 && !cfg.KeyValidationRequireContent {
		return true
	}
	return cfg.KeyValidationRequireContent
}

const maxProbeBodyBytes = 16 * 1024

// KeyProbeResult 是一次密钥探测的详细结果。
type KeyProbeResult struct {
	Valid      bool
	StatusCode int
	Body       string
	Err        error
}

func newKeyProbeResult(status int, body []byte, group *models.Group, kind string) KeyProbeResult {
	valid, err := concludeKeyValidation(status, body, group, kind)
	return KeyProbeResult{
		Valid:      valid,
		StatusCode: status,
		Body:       limitProbeBody(body),
		Err:        err,
	}
}

func limitProbeBody(body []byte) string {
	if len(body) <= maxProbeBodyBytes {
		return string(body)
	}
	return string(body[:maxProbeBodyBytes]) + "\n...[truncated]"
}

// concludeKeyValidation 根据状态码和正文判定探测是否成功。
func concludeKeyValidation(status int, body []byte, group *models.Group, kind string) (bool, error) {
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("[status %d] %s", status, app_errors.ParseUpstreamError(body))
	}
	if !requireValidationContent(group) {
		return true, nil
	}
	if err := inspectValidationResponse(kind, body); err != nil {
		return false, fmt.Errorf("[status %d] %s", status, err.Error())
	}
	return true, nil
}

func inspectValidationResponse(kind string, body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return fmt.Errorf("empty response body")
	}
	if looksLikeSSE(trimmed) {
		return fmt.Errorf("got SSE stream instead of a non-stream JSON response")
	}
	if !json.Valid(trimmed) {
		return fmt.Errorf("response is not valid JSON: %s", snippet(trimmed))
	}
	if hasJSONErrorObject(trimmed) {
		return fmt.Errorf("upstream returned error object: %s", app_errors.ParseUpstreamError(trimmed))
	}

	switch kind {
	case "openai":
		return inspectOpenAIChat(trimmed)
	case "openai-response":
		return inspectOpenAIResponse(trimmed)
	case "anthropic":
		return inspectAnthropic(trimmed)
	case "gemini":
		return inspectGemini(trimmed)
	default:
		return inspectOpenAIChat(trimmed)
	}
}

func looksLikeSSE(body []byte) bool {
	if bytes.HasPrefix(body, []byte("data:")) || bytes.HasPrefix(body, []byte("event:")) {
		return true
	}
	return bytes.Contains(body, []byte("\ndata:"))
}

func hasJSONErrorObject(body []byte) bool {
	var wrap struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return false
	}
	raw := bytes.TrimSpace(wrap.Error)
	return len(raw) > 0 && string(raw) != "null"
}

func inspectOpenAIChat(body []byte) error {
	var resp struct {
		Choices []struct {
			Message struct {
				Content   any   `json:"content"`
				ToolCalls []any `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("invalid chat.completion JSON")
	}
	if len(resp.Choices) == 0 {
		return fmt.Errorf("chat.completion has no choices")
	}
	msg := resp.Choices[0].Message
	if textFromAny(msg.Content) != "" || len(msg.ToolCalls) > 0 {
		return nil
	}
	return fmt.Errorf("chat.completion has empty message content")
}

func inspectOpenAIResponse(body []byte) error {
	var resp struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("invalid responses JSON")
	}
	for _, item := range resp.Output {
		for _, part := range item.Content {
			if strings.TrimSpace(part.Text) != "" {
				return nil
			}
		}
	}
	if len(resp.Output) > 0 {
		return nil
	}
	return fmt.Errorf("responses payload has no output")
}

func inspectAnthropic(body []byte) error {
	var resp struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("invalid messages JSON")
	}
	if resp.Type != "" && resp.Type != "message" {
		return fmt.Errorf("unexpected anthropic type %q", resp.Type)
	}
	if len(resp.Content) == 0 {
		return fmt.Errorf("messages payload has no content")
	}
	for _, part := range resp.Content {
		if strings.TrimSpace(part.Text) != "" {
			return nil
		}
	}
	return fmt.Errorf("messages payload has empty text")
}

func inspectGemini(body []byte) error {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("invalid generateContent JSON")
	}
	if len(resp.Candidates) == 0 {
		return fmt.Errorf("generateContent has no candidates")
	}
	for _, cand := range resp.Candidates {
		for _, part := range cand.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				return nil
			}
		}
	}
	return fmt.Errorf("generateContent has empty text")
}

func textFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []any:
		var b strings.Builder
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

func snippet(body []byte) string {
	s := string(body)
	if len(s) > maxValidationSnippet {
		return s[:maxValidationSnippet]
	}
	return s
}
