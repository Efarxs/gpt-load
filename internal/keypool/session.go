// 会话提取优先级对齐 CLIProxyAPI sdk/cliproxy/auth/selector.go，对照 v7.2.31 (05d1792d)。
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
	if id := extractJSONString(body, "conversation_id"); id != "" {
		return id
	}
	return hashMessages(body)
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

func hashMessages(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	raw, ok := payload["messages"]
	if !ok {
		if input, ok := payload["input"]; ok {
			raw = input
		}
	}
	if raw == nil {
		return ""
	}
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) == 0 {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "hash:" + hex.EncodeToString(sum[:8])
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
