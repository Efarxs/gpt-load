// 会话提取优先级对齐 CLIProxyAPI sdk/cliproxy/auth/selector.go，对照 v7.2.31 (05d1792d)，并补 OpenAI 缓存/Responses 字段。
package keypool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var claudeSessionPattern = regexp.MustCompile(`_session_([a-f0-9-]+)$`)

// ExtractSessionID 按规格优先级提取会话标识。
func ExtractSessionID(headers http.Header, body []byte) string {
	if id := extractClaudeSession(body); id != "" {
		return id
	}
	if headers != nil {
		if id := strings.TrimSpace(headers.Get("X-Session-ID")); id != "" {
			return id
		}
		if id := strings.TrimSpace(firstHeader(headers, "Session-Id", "Session_id", "Session-ID")); id != "" {
			return id
		}
		if id := strings.TrimSpace(headers.Get("X-Client-Request-Id")); id != "" {
			return id
		}
	}
	if id := extractJSONString(body, "previous_response_id"); id != "" {
		return id
	}
	if id := extractJSONString(body, "prompt_cache_key"); id != "" {
		return id
	}
	if id := extractJSONString(body, "conversation_id"); id != "" {
		return id
	}
	if id := extractJSONString(body, "session_id"); id != "" {
		return id
	}
	if id := extractMetadataSessionID(body); id != "" {
		return id
	}
	return hashFirstUserMessage(body)
}

func extractClaudeSession(body []byte) string {
	userID := extractNestedUserID(body)
	if userID == "" {
		return ""
	}
	if matches := claudeSessionPattern.FindStringSubmatch(userID); len(matches) >= 2 {
		return "claude:" + matches[1]
	}
	if strings.HasPrefix(strings.TrimSpace(userID), "{") {
		if sid := extractJSONString([]byte(userID), "session_id"); sid != "" {
			return "claude:" + sid
		}
	}
	return ""
}

func extractNestedUserID(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	meta, _ := payload["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	if id, ok := meta["user_id"].(string); ok {
		return id
	}
	return ""
}

func extractMetadataSessionID(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	meta, _ := payload["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	id, _ := meta["session_id"].(string)
	return strings.TrimSpace(id)
}

func extractJSONString(body []byte, key string) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if v, ok := payload[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func firstHeader(headers http.Header, names ...string) string {
	for _, name := range names {
		if v := headers.Get(name); v != "" {
			return v
		}
	}
	return ""
}

func hashFirstUserMessage(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	model, _ := payload["model"].(string)
	text := firstUserMessageText(payload)
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(model + "\x00" + text))
	return "hash:" + hex.EncodeToString(sum[:8])
}

func firstUserMessageText(payload map[string]any) string {
	if msgs, ok := payload["messages"].([]any); ok {
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			if mm == nil {
				continue
			}
			role, _ := mm["role"].(string)
			if role != "user" {
				continue
			}
			if text := messageText(mm); text != "" {
				return text
			}
		}
	}
	if contents, ok := payload["contents"].([]any); ok {
		for _, c := range contents {
			cm, _ := c.(map[string]any)
			if cm == nil {
				continue
			}
			role, _ := cm["role"].(string)
			if role != "" && role != "user" {
				continue
			}
			if text := geminiPartsText(cm); text != "" {
				return text
			}
		}
	}
	return ""
}

func messageText(m map[string]any) string {
	switch c := m["content"].(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			pm, _ := p.(map[string]any)
			if pm == nil {
				continue
			}
			if t, ok := pm["text"].(string); ok {
				b.WriteString(t)
			}
		}
		return b.String()
	default:
		return ""
	}
}

func geminiPartsText(m map[string]any) string {
	parts, ok := m["parts"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		if pm == nil {
			continue
		}
		if t, ok := pm["text"].(string); ok {
			b.WriteString(t)
		}
	}
	return b.String()
}

// ExtractVideoID 从视频路径中取出任务 ID。
func ExtractVideoID(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, "/")
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "/content")
	markers := []string{"/openai/v1/videos/", "/v1/videos/generations/", "/v1/videos/edits/", "/v1/videos/extensions/", "/v1/videos/"}
	for _, marker := range markers {
		if idx := strings.LastIndex(path, marker); idx >= 0 {
			id := strings.TrimPrefix(path[idx+len(marker):], "/")
			if id == "" || id == "generations" || id == "edits" || id == "extensions" {
				return ""
			}
			return id
		}
	}
	return ""
}
