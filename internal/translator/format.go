// Package translator 在客户端协议与分组渠道协议之间转换请求/响应。
// 对话转换器裁剪自 CLIProxyAPI internal/translator，对照版本见 docs/translator-sync.md。
package translator

import (
	"strings"

	cliproxytrans "gpt-load/internal/cliproxy/translator/translator"
)

// Format 表示一种 API 协议。
type Format string

const (
	FormatUnknown        Format = ""
	FormatOpenAI         Format = "openai"
	FormatOpenAIResponse Format = "openai-response"
	FormatClaude         Format = "claude"
	FormatGemini         Format = "gemini"
	FormatImages         Format = "openai-image"
	FormatVideos         Format = "openai-video"
)

// DetectFromPath 根据去掉分组前缀后的路径识别客户端协议。
// 无法识别时返回 FormatUnknown，调用方应保持透传。
func DetectFromPath(path string) Format {
	p := strings.TrimSpace(path)
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimRight(p, "/")

	switch {
	case strings.Contains(p, "/v1/chat/completions"):
		return FormatOpenAI
	case strings.Contains(p, "/v1beta/openai/"):
		return FormatOpenAI
	case strings.Contains(p, "/v1/responses"):
		return FormatOpenAIResponse
	case strings.HasSuffix(p, "/v1/messages") || strings.Contains(p, "/v1/messages/"):
		return FormatClaude
	case strings.Contains(p, ":generateContent") || strings.Contains(p, ":streamGenerateContent"):
		return FormatGemini
	case strings.Contains(p, "/v1/images/generations") || strings.Contains(p, "/v1/images/edits"):
		return FormatImages
	case isVideoPath(p):
		return FormatVideos
	default:
		return FormatUnknown
	}
}

func isVideoPath(p string) bool {
	if strings.Contains(p, "/openai/v1/videos") {
		return true
	}
	if strings.Contains(p, "/v1/videos") {
		return true
	}
	return false
}

// FormatFromChannel 把分组渠道类型映射为上游协议。
func FormatFromChannel(channelType string) Format {
	switch strings.TrimSpace(channelType) {
	case "openai":
		return FormatOpenAI
	case "openai-response":
		return FormatOpenAIResponse
	case "anthropic":
		return FormatClaude
	case "gemini":
		return FormatGemini
	default:
		return FormatUnknown
	}
}

// CompatibleIdentity 判断客户端协议与渠道是否可恒等转发（不改写路径语义）。
func CompatibleIdentity(source, target Format) bool {
	if source == FormatUnknown || target == FormatUnknown {
		return false
	}
	if source == target {
		return true
	}
	// openai 渠道同时承载 Chat Completions、Images、Videos 原生路径
	if target == FormatOpenAI && (source == FormatImages || source == FormatVideos) {
		return true
	}
	return false
}

// SupportsConversion 报告 source→target 是否有转换器或可恒等。
func SupportsConversion(source, target Format) bool {
	if CompatibleIdentity(source, target) {
		return true
	}
	if source == FormatImages && target == FormatOpenAIResponse {
		return true
	}
	if isChatFormat(source) && isChatFormat(target) {
		return cliproxytrans.NeedConvert(string(source), string(target))
	}
	return false
}

func IsChatFormat(f Format) bool {
	return isChatFormat(f)
}

func isChatFormat(f Format) bool {
	switch f {
	case FormatOpenAI, FormatOpenAIResponse, FormatClaude, FormatGemini:
		return true
	default:
		return false
	}
}
