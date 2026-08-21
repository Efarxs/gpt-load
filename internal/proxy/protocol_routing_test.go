package proxy

import (
	"encoding/json"
	"testing"

	"gpt-load/internal/models"
	"gpt-load/internal/translator"
	"gpt-load/internal/types"
)

func TestApplyProtocolRouting_DisabledPassthrough(t *testing.T) {
	group := &models.Group{
		ChannelType: "openai",
		EffectiveConfig: types.SystemSettings{
			EnableProtocolRouting: false,
		},
	}
	body := []byte(`{"model":"claude","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	out, route, err := applyProtocolRouting(group, "openai", "/proxy/openai/v1/messages", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if route.converted {
		t.Fatal("disabled routing must not convert")
	}
	if string(out) != string(body) {
		t.Fatal("body must stay original")
	}
}

func TestApplyProtocolRouting_MessagesToOpenAI(t *testing.T) {
	group := &models.Group{
		ChannelType: "openai",
		EffectiveConfig: types.SystemSettings{
			EnableProtocolRouting: true,
		},
	}
	body := []byte(`{"model":"claude-sonnet","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	out, route, err := applyProtocolRouting(group, "openai", "/proxy/openai/v1/messages", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if !route.converted {
		t.Fatal("expected conversion")
	}
	if route.rewritePath != "/proxy/openai/v1/chat/completions" {
		t.Fatalf("rewritePath = %q", route.rewritePath)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["messages"]; !ok {
		t.Fatal("converted body missing messages")
	}
}

func TestApplyProtocolRouting_ImagesToResponses(t *testing.T) {
	group := &models.Group{
		ChannelType: "openai-response",
		EffectiveConfig: types.SystemSettings{
			EnableProtocolRouting: true,
		},
	}
	body := []byte(`{"prompt":"a cat","model":"gpt-image-2"}`)
	_, route, err := applyProtocolRouting(group, "resp", "/proxy/resp/v1/images/generations", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if route.rewritePath != "/proxy/resp/v1/responses" {
		t.Fatalf("rewritePath = %q", route.rewritePath)
	}
}

func TestApplyProtocolRouting_ImagesToAnthropicRejected(t *testing.T) {
	group := &models.Group{
		ChannelType: "anthropic",
		EffectiveConfig: types.SystemSettings{
			EnableProtocolRouting: true,
		},
	}
	_, _, err := applyProtocolRouting(group, "ant", "/proxy/ant/v1/images/generations", "application/json", []byte(`{"prompt":"a cat"}`))
	if err == nil {
		t.Fatal("expected convert error")
	}
	if _, ok := err.(*translator.ConvertError); !ok {
		t.Fatalf("err type = %T", err)
	}
}

func TestApplyProtocolRouting_UsesSelectedGroupOnly(t *testing.T) {
	// 聚合组选出的子分组未开启转换时，即使父组概念上想开，也不得转换
	child := &models.Group{
		ChannelType: "openai",
		EffectiveConfig: types.SystemSettings{
			EnableProtocolRouting: false,
		},
	}
	body := []byte(`{"model":"claude","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	out, route, err := applyProtocolRouting(child, "agg", "/proxy/agg/v1/messages", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if route.converted {
		t.Fatal("child group flag must win")
	}
	if string(out) != string(body) {
		t.Fatal("must passthrough")
	}
}

func TestApplyProtocolRouting_ModelsPassthrough(t *testing.T) {
	group := &models.Group{
		ChannelType: "openai",
		EffectiveConfig: types.SystemSettings{
			EnableProtocolRouting: true,
		},
	}
	body := []byte(`{}`)
	out, route, err := applyProtocolRouting(group, "openai", "/proxy/openai/v1/models", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if route.converted {
		t.Fatal("models must not convert")
	}
	if string(out) != string(body) {
		t.Fatal("body changed")
	}
}
