package translator

import "encoding/json"

// canonicalRequest 是对话协议之间的内部表示。
type canonicalRequest struct {
	Model       string
	Messages    []canonicalMessage
	System      string
	MaxTokens   int
	Stream      bool
	Temperature *float64
	TopP        *float64
	Tools       []canonicalTool
}

type canonicalMessage struct {
	Role       string
	Content    string
	Name       string
	ToolCallID string
	ToolCalls  []canonicalToolCall
}

type canonicalToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type canonicalTool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type canonicalResponse struct {
	ID           string
	Model        string
	Text         string
	FinishReason string
	ToolCalls    []canonicalToolCall
	Usage        canonicalUsage
}

type canonicalUsage struct {
	InputTokens  int
	OutputTokens int
}

func decodeRequest(f Format, body []byte) (*canonicalRequest, error) {
	switch f {
	case FormatOpenAI:
		return decodeOpenAIRequest(body)
	case FormatOpenAIResponse:
		return decodeResponsesRequest(body)
	case FormatClaude:
		return decodeClaudeRequest(body)
	case FormatGemini:
		return decodeGeminiRequest(body)
	default:
		return nil, unsupportedPair(f, "")
	}
}

func encodeRequest(f Format, req *canonicalRequest) ([]byte, error) {
	switch f {
	case FormatOpenAI:
		return encodeOpenAIRequest(req)
	case FormatOpenAIResponse:
		return encodeResponsesRequest(req)
	case FormatClaude:
		return encodeClaudeRequest(req)
	case FormatGemini:
		return encodeGeminiRequest(req)
	default:
		return nil, unsupportedPair("", f)
	}
}

func decodeResponse(f Format, body []byte) (*canonicalResponse, error) {
	switch f {
	case FormatOpenAI:
		return decodeOpenAIResponse(body)
	case FormatOpenAIResponse:
		return decodeResponsesResponse(body)
	case FormatClaude:
		return decodeClaudeResponse(body)
	case FormatGemini:
		return decodeGeminiResponse(body)
	default:
		return nil, unsupportedPair(f, "")
	}
}

func encodeResponse(f Format, resp *canonicalResponse) ([]byte, error) {
	switch f {
	case FormatOpenAI:
		return encodeOpenAIResponse(resp)
	case FormatOpenAIResponse:
		return encodeResponsesResponse(resp)
	case FormatClaude:
		return encodeClaudeResponse(resp)
	case FormatGemini:
		return encodeGeminiResponse(resp)
	default:
		return nil, unsupportedPair("", f)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
