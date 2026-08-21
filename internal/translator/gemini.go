// 移植自 CLIProxyAPI internal/translator/gemini 与 openai/gemini，对照 v7.2.31 (05d1792d)。
package translator

import (
	"encoding/json"
	"fmt"
	"strings"
)

type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  *struct {
		MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
		Temperature     *float64 `json:"temperature,omitempty"`
		TopP            *float64 `json:"topP,omitempty"`
	} `json:"generationConfig,omitempty"`
	Tools []struct {
		FunctionDeclarations []struct {
			Name        string          `json:"name"`
			Description string          `json:"description,omitempty"`
			Parameters  json.RawMessage `json:"parameters,omitempty"`
		} `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

func decodeGeminiRequest(body []byte) (*canonicalRequest, error) {
	var raw geminiRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 Gemini 请求失败: %w", err)
	}
	req := &canonicalRequest{
		Messages: make([]canonicalMessage, 0, len(raw.Contents)),
	}
	if raw.SystemInstruction != nil {
		req.System = joinGeminiParts(raw.SystemInstruction.Parts)
	}
	if raw.GenerationConfig != nil {
		req.MaxTokens = raw.GenerationConfig.MaxOutputTokens
		req.Temperature = raw.GenerationConfig.Temperature
		req.TopP = raw.GenerationConfig.TopP
	}
	for _, content := range raw.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}
		if role == "" {
			role = "user"
		}
		req.Messages = append(req.Messages, canonicalMessage{
			Role:    role,
			Content: joinGeminiParts(content.Parts),
		})
	}
	return req, nil
}

func encodeGeminiRequest(req *canonicalRequest) ([]byte, error) {
	out := geminiRequest{
		Contents: make([]geminiContent, 0, len(req.Messages)),
	}
	if req.System != "" {
		out.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}
	if req.MaxTokens > 0 || req.Temperature != nil || req.TopP != nil {
		out.GenerationConfig = &struct {
			MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
			Temperature     *float64 `json:"temperature,omitempty"`
			TopP            *float64 `json:"topP,omitempty"`
		}{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
			TopP:            req.TopP,
		}
	}
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "system" {
			if out.SystemInstruction == nil {
				out.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: msg.Content}}}
			}
			continue
		}
		out.Contents = append(out.Contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: msg.Content}},
		})
	}
	return json.Marshal(out)
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Role  string       `json:"role"`
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func decodeGeminiResponse(body []byte) (*canonicalResponse, error) {
	var raw geminiResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 Gemini 响应失败: %w", err)
	}
	resp := &canonicalResponse{
		Model: raw.ModelVersion,
		Usage: canonicalUsage{
			InputTokens:  raw.UsageMetadata.PromptTokenCount,
			OutputTokens: raw.UsageMetadata.CandidatesTokenCount,
		},
	}
	if len(raw.Candidates) > 0 {
		resp.Text = joinGeminiParts(raw.Candidates[0].Content.Parts)
		resp.FinishReason = mapGeminiFinish(raw.Candidates[0].FinishReason)
	}
	return resp, nil
}

func encodeGeminiResponse(resp *canonicalResponse) ([]byte, error) {
	out := geminiResponse{
		ModelVersion: resp.Model,
	}
	out.Candidates = append(out.Candidates, struct {
		Content struct {
			Role  string       `json:"role"`
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	}{
		FinishReason: mapToGeminiFinish(resp.FinishReason),
	})
	out.Candidates[0].Content.Role = "model"
	out.Candidates[0].Content.Parts = []geminiPart{{Text: resp.Text}}
	out.UsageMetadata.PromptTokenCount = resp.Usage.InputTokens
	out.UsageMetadata.CandidatesTokenCount = resp.Usage.OutputTokens
	return json.Marshal(out)
}

func joinGeminiParts(parts []geminiPart) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Text)
	}
	return b.String()
}

func mapGeminiFinish(reason string) string {
	switch strings.ToUpper(reason) {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	default:
		return firstNonEmpty(strings.ToLower(reason), "stop")
	}
}

func mapToGeminiFinish(reason string) string {
	switch reason {
	case "length":
		return "MAX_TOKENS"
	default:
		return "STOP"
	}
}
