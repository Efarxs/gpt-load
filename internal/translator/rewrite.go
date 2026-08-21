package translator

import (
	"fmt"
	"strings"
)

// RewritePath 返回转换后应打到上游的路径。
// originalPath 为去掉 /proxy/{group} 后的客户端路径。
func RewritePath(source, target Format, originalPath, model string, stream bool) string {
	if CompatibleIdentity(source, target) {
		if originalPath == "" {
			return defaultPath(source, model, stream)
		}
		return originalPath
	}

	return defaultPath(target, model, stream)
}

func defaultPath(f Format, model string, stream bool) string {
	switch f {
	case FormatOpenAI:
		return "/v1/chat/completions"
	case FormatOpenAIResponse:
		return "/v1/responses"
	case FormatClaude:
		return "/v1/messages"
	case FormatGemini:
		name := strings.TrimSpace(model)
		if name == "" {
			name = "gemini-pro"
		}
		action := "generateContent"
		if stream {
			action = "streamGenerateContent"
		}
		return fmt.Sprintf("/v1beta/models/%s:%s", name, action)
	case FormatImages:
		return "/v1/images/generations"
	case FormatVideos:
		return "/v1/videos"
	default:
		return ""
	}
}

// IsImagesEditsPath 判断是否为改图接口。
func IsImagesEditsPath(path string) bool {
	return strings.Contains(path, "/v1/images/edits")
}
