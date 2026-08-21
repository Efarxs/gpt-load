package proxy

import (
	"net/http"
	"testing"
	"time"

	"gpt-load/internal/models"
	"gpt-load/internal/types"
)

func TestBuildAffinityState_Disabled(t *testing.T) {
	group := &models.Group{EffectiveConfig: types.SystemSettings{}}
	st := buildAffinityState(group, http.Header{}, "/proxy/g/v1/chat/completions", http.MethodPost, []byte(`{}`))
	if st.enabled {
		t.Fatal("affinity must default off")
	}
}

func TestBuildAffinityState_SessionAndVideo(t *testing.T) {
	group := &models.Group{EffectiveConfig: types.SystemSettings{
		EnableChannelAffinity: true,
		SessionAffinityTTL:    "30m",
	}}
	headers := http.Header{}
	headers.Set("X-Session-ID", "abc")
	st := buildAffinityState(group, headers, "/proxy/g/v1/chat/completions", http.MethodPost, []byte(`{"model":"gpt-4"}`))
	if !st.enabled || st.sessionID != "abc" || st.model != "gpt-4" || st.ttl != 30*time.Minute {
		t.Fatalf("%+v", st)
	}

	st = buildAffinityState(group, http.Header{}, "/proxy/g/openai/v1/videos/vid9", http.MethodGet, nil)
	if st.videoID != "vid9" || st.videoCreate {
		t.Fatalf("retrieve state = %+v", st)
	}
	st = buildAffinityState(group, http.Header{}, "/proxy/g/openai/v1/videos", http.MethodPost, []byte(`{"prompt":"x"}`))
	if !st.videoCreate || st.videoID != "" {
		t.Fatalf("create state = %+v", st)
	}
}

func TestParseRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "7")
	if got := parseRetryAfter(h); got != 7*time.Second {
		t.Fatalf("got %v", got)
	}
}

func TestExtractCreatedVideoID(t *testing.T) {
	if got := extractCreatedVideoID([]byte(`{"id":"video_1"}`)); got != "video_1" {
		t.Fatalf("got %q", got)
	}
	if got := extractCreatedVideoID([]byte(`{"request_id":"req_1"}`)); got != "req_1" {
		t.Fatalf("got %q", got)
	}
}
