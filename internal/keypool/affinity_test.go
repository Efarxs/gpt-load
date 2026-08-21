package keypool

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
)

func TestExtractSessionID_Priority(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Session-ID", "header-session")
	body := []byte(`{"metadata":{"user_id":"user_abc_account__session_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},"conversation_id":"conv-1"}`)
	got := ExtractSessionID(headers, body)
	if got != "claude:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("got %q", got)
	}

	got = ExtractSessionID(headers, []byte(`{"foo":1}`))
	if got != "header-session" {
		t.Fatalf("header fallback = %q", got)
	}

	got = ExtractSessionID(http.Header{}, []byte(`{"previous_response_id":"resp-1","conversation_id":"c-1"}`))
	if got != "resp-1" {
		t.Fatalf("previous_response_id = %q", got)
	}

	got = ExtractSessionID(http.Header{}, []byte(`{"prompt_cache_key":"cache-1","conversation_id":"c-1"}`))
	if got != "cache-1" {
		t.Fatalf("prompt_cache_key = %q", got)
	}

	got = ExtractSessionID(http.Header{}, []byte(`{"conversation_id":"c-1"}`))
	if got != "c-1" {
		t.Fatalf("conversation = %q", got)
	}

	got = ExtractSessionID(http.Header{}, []byte(`{"session_id":"s-top"}`))
	if got != "s-top" {
		t.Fatalf("session_id = %q", got)
	}

	got = ExtractSessionID(http.Header{}, []byte(`{"metadata":{"session_id":"s-meta"}}`))
	if got != "s-meta" {
		t.Fatalf("metadata.session_id = %q", got)
	}

	got = ExtractSessionID(http.Header{}, []byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	if got == "" {
		t.Fatal("expected message hash fallback")
	}

	if ExtractSessionID(http.Header{}, []byte(`{"foo":1}`)) != "" {
		t.Fatal("empty traits must yield empty session")
	}
}

func TestExtractSessionID_FirstUserHashStableAcrossTurns(t *testing.T) {
	first := ExtractSessionID(http.Header{}, []byte(`{"model":"gpt","messages":[{"role":"user","content":"hi"}]}`))
	second := ExtractSessionID(http.Header{}, []byte(`{"model":"gpt","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"more"}]}`))
	if first == "" || first != second {
		t.Fatalf("hash should stick on first user text, %q vs %q", first, second)
	}

	gemini := ExtractSessionID(http.Header{}, []byte(`{"model":"g","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	if gemini == "" {
		t.Fatal("gemini contents hash missing")
	}
}

func TestExtractVideoID(t *testing.T) {
	if got := ExtractVideoID("/proxy/g/openai/v1/videos/vid_1/content"); got != "vid_1" {
		t.Fatalf("got %q", got)
	}
	if got := ExtractVideoID("/v1/videos"); got != "" {
		t.Fatalf("create path should have empty id, got %q", got)
	}
}

func newTestProvider(t *testing.T) (*KeyProvider, store.Store) {
	t.Helper()
	mem := store.NewMemoryStore()
	enc, err := encryption.NewService("")
	if err != nil {
		t.Fatal(err)
	}
	p := NewProvider(nil, mem, nil, enc)
	return p, mem
}

func seedKey(t *testing.T, p *KeyProvider, mem store.Store, groupID, keyID uint, value string) {
	t.Helper()
	key := &models.APIKey{
		ID:        keyID,
		KeyValue:  value,
		Status:    models.KeyStatusActive,
		GroupID:   groupID,
		CreatedAt: time.Now(),
	}
	if err := mem.HSet("key:"+strconv.FormatUint(uint64(keyID), 10), p.apiKeyToMap(key)); err != nil {
		t.Fatal(err)
	}
	if err := mem.LPush("group:"+strconv.FormatUint(uint64(groupID), 10)+":active_keys", keyID); err != nil {
		t.Fatal(err)
	}
}

func TestSelectKeyWithOptions_StickyAndRebind(t *testing.T) {
	p, mem := newTestProvider(t)
	seedKey(t, p, mem, 1, 11, "sk-a")
	seedKey(t, p, mem, 1, 12, "sk-b")

	opts := SelectOptions{EnableAffinity: true, SessionID: "s1", Model: "gpt", TTL: time.Hour}
	first, err := p.SelectKeyWithOptions(1, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.SelectKeyWithOptions(1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("same session should stick, %d vs %d", first.ID, second.ID)
	}

	other, err := p.SelectKeyWithOptions(1, SelectOptions{EnableAffinity: true, SessionID: "s2", Model: "gpt", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	_ = other

	// 关闭亲和时应轮询
	a, err := p.SelectKeyWithOptions(1, SelectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.SelectKeyWithOptions(1, SelectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("disabled affinity should rotate")
	}
}

func TestSelectKeyWithOptions_CooldownAndVideoBind(t *testing.T) {
	p, mem := newTestProvider(t)
	seedKey(t, p, mem, 2, 21, "sk-a")
	seedKey(t, p, mem, 2, 22, "sk-b")

	first, err := p.SelectKeyWithOptions(2, SelectOptions{EnableAffinity: true, SessionID: "s", Model: "m", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	p.MarkCooldown(2, first.ID, time.Hour)
	next, err := p.SelectKeyWithOptions(2, SelectOptions{EnableAffinity: true, SessionID: "s", Model: "m", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID {
		t.Fatal("cooling key must be skipped")
	}

	p.BindVideo(2, first.ID, "vid-1", time.Hour)
	_, err = p.SelectKeyWithOptions(2, SelectOptions{RequireVideoBound: true, VideoID: "vid-1"})
	if err == nil {
		t.Fatal("cooling video key must fail instead of rebinding")
	}

	// 冷却到期
	_ = mem.Delete(cooldownKey(2, first.ID))
	got, err := p.SelectKeyWithOptions(2, SelectOptions{RequireVideoBound: true, VideoID: "vid-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID {
		t.Fatalf("video bind restored to %d", got.ID)
	}
}

func TestSelectKeyWithOptions_TTLExpire(t *testing.T) {
	p, mem := newTestProvider(t)
	seedKey(t, p, mem, 3, 31, "sk-a")
	seedKey(t, p, mem, 3, 32, "sk-b")
	opts := SelectOptions{EnableAffinity: true, SessionID: "ttl", Model: "m", TTL: 20 * time.Millisecond}
	first, err := p.SelectKeyWithOptions(3, opts)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	_ = first
	// 过期后允许重新选择；不强制不同，但绑定键必须消失
	if _, ok := p.GetBinding(3, "ttl", "m"); ok {
		t.Fatal("expired bind should disappear")
	}
}

func TestAffinityBinding_JSONAndLegacyMiss(t *testing.T) {
	p, mem := newTestProvider(t)
	seedKey(t, p, mem, 4, 41, "sk-a")
	seedKey(t, p, mem, 4, 42, "sk-b")

	opts := SelectOptions{
		EnableAffinity:  true,
		SessionID:       "s",
		Model:           "m",
		TTL:             time.Hour,
		UpstreamIdx:     1,
		UpstreamBaseURL: "https://api.example.com",
		SubGroup:        "child",
	}
	first, err := p.SelectKeyWithOptions(4, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := p.GetBinding(4, "s", "m")
	if !ok {
		t.Fatal("expected json binding")
	}
	if b.KeyID != first.ID || b.UpstreamIdx != 1 || b.BaseURL != "https://api.example.com" || b.SubGroup != "child" {
		t.Fatalf("binding = %+v", b)
	}

	second, err := p.SelectKeyWithOptions(4, opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatal("same session should stick")
	}

	if err := mem.Set(bindKey(4, "legacy", "m"), []byte("41"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.GetBinding(4, "legacy", "m"); ok {
		t.Fatal("legacy numeric binding must miss")
	}
}

func TestAffinityBinding_StaleUpstreamTreatedAsMissByCaller(t *testing.T) {
	p, mem := newTestProvider(t)
	seedKey(t, p, mem, 5, 51, "sk-a")
	p.SetBinding(5, "s", "m", AffinityBinding{
		KeyID:       51,
		UpstreamIdx: 0,
		BaseURL:     "https://old.example.com",
	}, time.Hour)
	b, ok := p.GetBinding(5, "s", "m")
	if !ok || b.BaseURL != "https://old.example.com" {
		t.Fatal("stored base url should round-trip")
	}
	p.DeleteBinding(5, "s", "m")
	if _, ok := p.GetBinding(5, "s", "m"); ok {
		t.Fatal("deleted binding must miss")
	}
}
