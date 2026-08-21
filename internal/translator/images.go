package translator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// 移植自 CLIProxyAPI sdk/api/handlers/openai/openai_images_handlers.go，对照 v7.2.31 (05d1792d)。
// 覆盖 buildImagesResponsesRequest 以及 Responses 结果回写 Images 响应。

type imagesRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Background     string `json:"background,omitempty"`
	OutputFormat   string `json:"output_format,omitempty"`
}

func imagesToResponses(originalPath string, body []byte) ([]byte, error) {
	var raw imagesRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, &ConvertError{Status: http.StatusBadRequest, Message: "解析 OpenAI Images 请求失败: " + err.Error()}
		}
	}
	prompt := strings.TrimSpace(raw.Prompt)
	if prompt == "" {
		return nil, &ConvertError{Status: http.StatusBadRequest, Message: "Images 请求缺少 prompt"}
	}
	model := strings.TrimSpace(raw.Model)
	if model == "" {
		model = "gpt-image-2"
	}

	tool := map[string]any{
		"type":   "image_generation",
		"action": "generate",
		"model":  model,
	}
	if IsImagesEditsPath(originalPath) {
		tool["action"] = "edit"
	}
	if raw.Size != "" {
		tool["size"] = raw.Size
	}
	if raw.Quality != "" {
		tool["quality"] = raw.Quality
	}
	if raw.Background != "" {
		tool["background"] = raw.Background
	}
	if raw.OutputFormat != "" {
		tool["output_format"] = raw.OutputFormat
	}

	req := map[string]any{
		"model":       "gpt-5.4-mini",
		"stream":      false,
		"tool_choice": map[string]any{"type": "image_generation"},
		"tools":       []any{tool},
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": prompt},
				},
			},
		},
	}
	return json.Marshal(req)
}

func responsesToImages(body []byte) ([]byte, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 Responses 生图结果失败: %w", err)
	}

	created := time.Now().Unix()
	if v, ok := raw["created_at"].(float64); ok && v > 0 {
		created = int64(v)
	}

	items := collectImagePayloads(raw)
	data := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{}
		if item["b64_json"] != nil {
			entry["b64_json"] = item["b64_json"]
		}
		if item["url"] != nil {
			entry["url"] = item["url"]
		}
		if item["revised_prompt"] != nil {
			entry["revised_prompt"] = item["revised_prompt"]
		}
		if len(entry) > 0 {
			data = append(data, entry)
		}
	}
	if len(data) == 0 {
		// 兜底：把文本结果放到 revised_prompt，避免空 data
		if text := extractResponsesText(raw); text != "" {
			data = append(data, map[string]any{"revised_prompt": text})
		}
	}

	out := map[string]any{
		"created": created,
		"data":    data,
	}
	return json.Marshal(out)
}

func collectImagePayloads(raw map[string]any) []map[string]any {
	var found []map[string]any
	output, _ := raw["output"].([]any)
	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if result, ok := m["result"].(string); ok && result != "" {
			found = append(found, map[string]any{"b64_json": result})
		}
		if content, ok := m["content"].([]any); ok {
			for _, part := range content {
				pm, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if b64, ok := pm["b64_json"].(string); ok && b64 != "" {
					found = append(found, map[string]any{"b64_json": b64})
				}
				if url, ok := pm["image_url"].(string); ok && url != "" {
					found = append(found, map[string]any{"url": url})
				}
			}
		}
	}
	return found
}

func extractResponsesText(raw map[string]any) string {
	output, _ := raw["output"].([]any)
	var b strings.Builder
	for _, item := range output {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		content, _ := m["content"].([]any)
		for _, part := range content {
			pm, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := pm["text"].(string); ok {
				b.WriteString(t)
			}
		}
	}
	return b.String()
}
