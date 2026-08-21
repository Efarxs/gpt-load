package translator

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ConvertStream 把上游 SSE / 分块响应转换成客户端协议的事件流。
func ConvertStream(source, target Format, upstream []byte) ([]byte, error) {
	if CompatibleIdentity(source, target) {
		return upstream, nil
	}
	if source == FormatClaude && target == FormatOpenAI {
		var out bytes.Buffer
		state := &ClaudeStreamState{}
		if err := ConvertOpenAIStreamToClaude(bytes.NewReader(upstream), &out, state); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}

	events := splitSSE(upstream)
	if len(events) == 0 {
		resp, err := decodeResponse(target, upstream)
		if err != nil {
			return nil, err
		}
		return encodeStreamEvent(source, resp, true)
	}

	var out bytes.Buffer
	for _, ev := range events {
		if ev == "[DONE]" {
			continue
		}
		delta := extractStreamText(target, []byte(ev))
		if delta != "" {
			chunk, err := encodeStreamEvent(source, &canonicalResponse{Text: delta}, false)
			if err != nil {
				return nil, err
			}
			out.Write(chunk)
		}
	}
	done, err := encodeStreamEvent(source, &canonicalResponse{Text: "", FinishReason: "stop"}, true)
	if err != nil {
		return nil, err
	}
	out.Write(done)
	return out.Bytes(), nil
}

// ClaudeStreamState 维护 OpenAI chunk → Anthropic SSE 的事件顺序。
// Claude Code 必须先收到 message_start，否则会把 200 当成空/畸形响应。
type ClaudeStreamState struct {
	started     bool
	textStarted bool
	msgID       string
	model       string
	finish      string
	usageIn     int
	usageOut    int
}

// ConvertOpenAIStreamToClaude 逐块读取上游 OpenAI SSE，写出完整 Anthropic 事件流。
func ConvertOpenAIStreamToClaude(r io.Reader, w io.Writer, state *ClaudeStreamState) error {
	if state == nil {
		state = &ClaudeStreamState{}
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		events := state.feedOpenAIChunk([]byte(payload))
		if _, err := w.Write(events); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	_, err := w.Write(state.finishEvents())
	return err
}

func (s *ClaudeStreamState) feedOpenAIChunk(payload []byte) []byte {
	var chunk openAIChatResponse
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil
	}
	var b bytes.Buffer
	if s.msgID == "" {
		s.msgID = firstNonEmpty(chunk.ID, "msg_translated")
	}
	if s.model == "" {
		s.model = chunk.Model
	}
	if !s.started {
		s.started = true
		start := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            s.msgID,
				"type":          "message",
				"role":          "assistant",
				"model":         s.model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		}
		writeClaudeEvent(&b, "message_start", start)
	}

	var deltaText string
	var finish string
	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.Delta != nil {
			deltaText = choice.Delta.Content
		} else {
			deltaText = choice.Message.Content
		}
		finish = choice.FinishReason
	}
	if chunk.Usage.PromptTokens > 0 {
		s.usageIn = chunk.Usage.PromptTokens
	}
	if chunk.Usage.CompletionTokens > 0 {
		s.usageOut = chunk.Usage.CompletionTokens
	}
	if finish != "" {
		s.finish = finish
	}
	if deltaText == "" {
		return b.Bytes()
	}
	if !s.textStarted {
		s.textStarted = true
		writeClaudeEvent(&b, "content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "text", "text": ""},
		})
	}
	writeClaudeEvent(&b, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "text_delta", "text": deltaText},
	})
	return b.Bytes()
}

func (s *ClaudeStreamState) finishEvents() []byte {
	var b bytes.Buffer
	if !s.started {
		s.started = true
		writeClaudeEvent(&b, "message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            firstNonEmpty(s.msgID, "msg_translated"),
				"type":          "message",
				"role":          "assistant",
				"model":         s.model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		})
	}
	if s.textStarted {
		writeClaudeEvent(&b, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
	}
	writeClaudeEvent(&b, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": mapToClaudeStop(s.finish), "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": s.usageOut, "input_tokens": s.usageIn},
	})
	writeClaudeEvent(&b, "message_stop", map[string]any{"type": "message_stop"})
	return b.Bytes()
}

func writeClaudeEvent(w io.Writer, event string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}

func splitSSE(raw []byte) []string {
	parts := strings.Split(string(raw), "\n")
	events := make([]string, 0)
	for _, line := range parts {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload != "" {
				events = append(events, payload)
			}
		}
	}
	return events
}

func extractStreamText(upstream Format, payload []byte) string {
	switch upstream {
	case FormatOpenAI:
		var chunk openAIChatResponse
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return ""
		}
		if len(chunk.Choices) == 0 {
			return ""
		}
		if chunk.Choices[0].Delta != nil {
			return chunk.Choices[0].Delta.Content
		}
		return chunk.Choices[0].Message.Content
	case FormatClaude:
		var ev map[string]any
		if err := json.Unmarshal(payload, &ev); err != nil {
			return ""
		}
		delta, _ := ev["delta"].(map[string]any)
		if delta != nil {
			if t, ok := delta["text"].(string); ok {
				return t
			}
		}
		return ""
	case FormatGemini:
		resp, err := decodeGeminiResponse(payload)
		if err != nil {
			return ""
		}
		return resp.Text
	case FormatOpenAIResponse:
		var ev map[string]any
		if err := json.Unmarshal(payload, &ev); err != nil {
			return ""
		}
		if t, ok := ev["delta"].(string); ok {
			return t
		}
		if delta, ok := ev["delta"].(map[string]any); ok {
			if t, ok := delta["text"].(string); ok {
				return t
			}
		}
		return ""
	default:
		return ""
	}
}

func encodeStreamEvent(client Format, resp *canonicalResponse, done bool) ([]byte, error) {
	switch client {
	case FormatOpenAI:
		if done {
			return []byte("data: [DONE]\n\n"), nil
		}
		chunk := map[string]any{
			"id":      "chatcmpl-translated",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   resp.Model,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         map[string]any{"content": resp.Text},
					"finish_reason": nil,
				},
			},
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			return nil, err
		}
		return []byte("data: " + string(b) + "\n\n"), nil
	case FormatClaude:
		state := &ClaudeStreamState{model: resp.Model, finish: resp.FinishReason}
		if done {
			return state.finishEvents(), nil
		}
		return state.feedOpenAIChunk(mustOpenAIDelta(resp.Text)), nil
	case FormatGemini:
		b, err := encodeGeminiResponse(resp)
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	case FormatOpenAIResponse:
		if done {
			return []byte("event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"), nil
		}
		ev := map[string]any{
			"type":  "response.output_text.delta",
			"delta": resp.Text,
		}
		b, err := json.Marshal(ev)
		if err != nil {
			return nil, err
		}
		return []byte("event: response.output_text.delta\ndata: " + string(b) + "\n\n"), nil
	default:
		return nil, fmt.Errorf("不支持的流式客户端协议: %s", client)
	}
}

func mustOpenAIDelta(text string) []byte {
	b, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-translated",
		"choices": []map[string]any{
			{"delta": map[string]any{"content": text}},
		},
	})
	return b
}
