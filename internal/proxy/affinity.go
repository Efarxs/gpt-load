package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gpt-load/internal/keypool"
	"gpt-load/internal/models"
	"gpt-load/internal/translator"
)

type affinityState struct {
	enabled     bool
	sessionID   string
	model       string
	ttl         time.Duration
	videoID     string
	videoCreate bool
}

func buildAffinityState(group *models.Group, headers http.Header, path, method string, body []byte) *affinityState {
	if group == nil || !group.EffectiveConfig.EnableChannelAffinity {
		return &affinityState{}
	}
	model := ""
	var probe struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &probe) == nil {
		model = probe.Model
	}
	videoID := keypool.ExtractVideoID(path)
	isVideo := translator.DetectFromPath(path) == translator.FormatVideos
	return &affinityState{
		enabled:     true,
		sessionID:   keypool.ExtractSessionID(headers, body),
		model:       model,
		ttl:         group.EffectiveConfig.SessionAffinityDuration(),
		videoID:     videoID,
		videoCreate: isVideo && videoID == "" && strings.EqualFold(method, http.MethodPost),
	}
}

func parseRetryAfter(header http.Header) time.Duration {
	if header == nil {
		return 0
	}
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(raw); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}

func extractCreatedVideoID(body []byte) string {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if id, ok := payload["id"].(string); ok && id != "" {
		return id
	}
	if id, ok := payload["request_id"].(string); ok && id != "" {
		return id
	}
	return ""
}
