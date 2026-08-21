package channel

import (
	"testing"

	"gpt-load/internal/models"
	"gpt-load/internal/types"
)

func TestInspectValidationResponse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		kind    string
		body    string
		wantErr bool
	}{
		{name: "openai ok", kind: "openai", body: `{"choices":[{"message":{"content":"hello"}}]}`},
		{name: "openai empty choices", kind: "openai", body: `{"choices":[]}`, wantErr: true},
		{name: "openai empty content", kind: "openai", body: `{"choices":[{"message":{"content":""}}]}`, wantErr: true},
		{name: "openai 200 error object", kind: "openai", body: `{"error":{"message":"nope"}}`, wantErr: true},
		{name: "empty body", kind: "openai", body: "", wantErr: true},
		{name: "sse", kind: "openai", body: "data: {\"choices\":[]}\n\n", wantErr: true},
		{name: "anthropic ok", kind: "anthropic", body: `{"type":"message","content":[{"type":"text","text":"hi"}]}`},
		{name: "anthropic empty", kind: "anthropic", body: `{"type":"message","content":[]}`, wantErr: true},
		{name: "gemini ok", kind: "gemini", body: `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`},
		{name: "gemini empty", kind: "gemini", body: `{"candidates":[]}`, wantErr: true},
		{name: "responses ok", kind: "openai-response", body: `{"output":[{"content":[{"text":"hi"}]}]}`},
		{name: "responses empty", kind: "openai-response", body: `{"status":"completed"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := inspectValidationResponse(tc.kind, []byte(tc.body))
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConcludeKeyValidationRespectsSwitch(t *testing.T) {
	t.Parallel()
	empty200 := []byte(`{}`)
	groupOn := &models.Group{EffectiveConfig: types.SystemSettings{
		AppUrl:                      "http://localhost",
		KeyValidationTimeoutSeconds: 20,
		KeyValidationRequireContent: true,
	}}
	if ok, err := concludeKeyValidation(200, empty200, groupOn, "openai"); ok || err == nil {
		t.Fatalf("strict mode should fail empty body, ok=%v err=%v", ok, err)
	}

	groupOff := &models.Group{EffectiveConfig: types.SystemSettings{
		AppUrl:                      "http://localhost",
		KeyValidationTimeoutSeconds: 20,
		KeyValidationRequireContent: false,
	}}
	if ok, err := concludeKeyValidation(200, empty200, groupOff, "openai"); !ok || err != nil {
		t.Fatalf("loose mode should accept 2xx, ok=%v err=%v", ok, err)
	}

	if ok, err := concludeKeyValidation(200, nil, nil, "openai"); ok || err == nil {
		t.Fatalf("nil group should default to strict, ok=%v err=%v", ok, err)
	}
}

func TestNewKeyProbeResultKeepsBody(t *testing.T) {
	t.Parallel()
	body := []byte(`{"choices":[{"message":{"content":"hello"}}]}`)
	got := newKeyProbeResult(200, body, nil, "openai")
	if !got.Valid || got.StatusCode != 200 || got.Body == "" {
		t.Fatalf("probe=%+v", got)
	}
}
