package proxy

import (
	"testing"

	"gpt-load/internal/models"
	"gpt-load/internal/types"

	"github.com/tidwall/gjson"
	"gorm.io/datatypes"
)

func TestStripOrphanToolFlags(t *testing.T) {
	t.Parallel()
	in := []byte(`{"model":"gpt-5.6-luna","tool_choice":"auto","parallel_tool_calls":true,"messages":[{"role":"user","content":"hi"}]}`)
	out := stripOrphanToolFlags(in)
	if gjson.GetBytes(out, "tool_choice").Exists() || gjson.GetBytes(out, "parallel_tool_calls").Exists() {
		t.Fatalf("flags should be stripped: %s", out)
	}

	withTools := []byte(`{"tools":[{"type":"function"}],"tool_choice":"auto","parallel_tool_calls":true}`)
	kept := stripOrphanToolFlags(withTools)
	if !gjson.GetBytes(kept, "tool_choice").Exists() || !gjson.GetBytes(kept, "parallel_tool_calls").Exists() {
		t.Fatalf("flags should stay when tools exist: %s", kept)
	}
}

func TestApplyOutboundBodyFixesModelOverride(t *testing.T) {
	t.Parallel()
	group := &models.Group{
		ChannelType: "openai",
		EffectiveConfig: types.SystemSettings{
			StripOrphanToolFlags: true,
		},
		ModelParamOverrides: datatypes.JSONMap{
			"gpt-5.6-luna": map[string]any{
				"reasoning_effort": "none",
			},
		},
	}
	in := []byte(`{"model":"gpt-5.6-luna","tools":[{"type":"function","function":{"name":"ping"}}],"tool_choice":"auto"}`)
	out := applyOutboundBodyFixes(in, group)
	if gjson.GetBytes(out, "reasoning_effort").String() != "none" {
		t.Fatalf("expected reasoning_effort=none, got %s", out)
	}
	if !gjson.GetBytes(out, "tool_choice").Exists() {
		t.Fatal("tool_choice should remain when tools exist")
	}

	noTools := []byte(`{"model":"gpt-5.6-luna","tool_choice":"auto","parallel_tool_calls":true}`)
	cleaned := applyOutboundBodyFixes(noTools, group)
	if gjson.GetBytes(cleaned, "tool_choice").Exists() || gjson.GetBytes(cleaned, "parallel_tool_calls").Exists() {
		t.Fatalf("orphan flags remain: %s", cleaned)
	}
}
