package translator

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDetectFromPath(t *testing.T) {
	cases := map[string]Format{
		"/v1/chat/completions": FormatOpenAI,
		"/v1beta/openai/chat/completions": FormatOpenAI,
		"/v1/responses":        FormatOpenAIResponse,
		"/v1/messages":         FormatClaude,
		"/v1beta/models/gemini-pro:generateContent":       FormatGemini,
		"/v1beta/models/gemini-pro:streamGenerateContent": FormatGemini,
		"/v1/images/generations":                          FormatImages,
		"/v1/images/edits":                                FormatImages,
		"/v1/videos":                                      FormatVideos,
		"/openai/v1/videos":                               FormatVideos,
		"/openai/v1/videos/abc/content":                   FormatVideos,
		"/v1/models":                                      FormatUnknown,
		"/v1/embeddings":                                  FormatUnknown,
	}
	for path, want := range cases {
		if got := DetectFromPath(path); got != want {
			t.Fatalf("DetectFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestFormatFromChannel(t *testing.T) {
	if FormatFromChannel("anthropic") != FormatClaude {
		t.Fatal("anthropic should map to claude")
	}
	if FormatFromChannel("openai-response") != FormatOpenAIResponse {
		t.Fatal("openai-response mapping")
	}
}

func TestConvertRequest_UnknownPathPassthrough(t *testing.T) {
	body := []byte(`{"input":"hi"}`)
	out, err := ConvertRequest(FormatUnknown, FormatOpenAI, "/v1/embeddings", body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Converted {
		t.Fatal("unknown path must not convert")
	}
	if string(out.Body) != string(body) {
		t.Fatal("body must stay unchanged")
	}
}

func TestConvertRequest_MessagesToChatCompletions(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet","max_tokens":64,"messages":[{"role":"user","content":"你好"}]}`)
	out, err := ConvertRequest(FormatClaude, FormatOpenAI, "/v1/messages", body)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Converted {
		t.Fatal("expected conversion")
	}
	if out.Path != "/v1/chat/completions" {
		t.Fatalf("path = %q", out.Path)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "claude-sonnet" {
		t.Fatalf("model = %v", got["model"])
	}
	msgs := got["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("missing messages")
	}
}

func TestConvertResponse_ChatCompletionsToMessages(t *testing.T) {
	upstream := []byte(`{"id":"cmpl-1","model":"gpt-4","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"世界"}}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`)
	out, err := ConvertResponse(FormatClaude, FormatOpenAI, upstream, false)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "message" {
		t.Fatalf("type = %v", got["type"])
	}
	content := got["content"].([]any)
	first := content[0].(map[string]any)
	if first["text"] != "世界" {
		t.Fatalf("text = %v", first["text"])
	}
}

func TestConvertRequest_ResponsesToMessages(t *testing.T) {
	body := []byte(`{"model":"gpt-4.1","input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	out, err := ConvertRequest(FormatOpenAIResponse, FormatClaude, "/v1/responses", body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/v1/messages" {
		t.Fatalf("path = %q", out.Path)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["max_tokens"]; !ok {
		t.Fatal("claude request must have max_tokens")
	}
}

func TestConvertRequest_ChatCompletionsToGemini(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-pro","messages":[{"role":"user","content":"hello"}]}`)
	out, err := ConvertRequest(FormatOpenAI, FormatGemini, "/v1/chat/completions", body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Path, ":generateContent") {
		t.Fatalf("path = %q", out.Path)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatal(err)
	}
	contents := got["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents = %v", contents)
	}
}

func TestConvertStream_ChatToResponsesEmitsCompletedWithoutDONE(t *testing.T) {
	upstream := []byte("data: {\"id\":\"cmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n")
	out, err := ConvertResponse(FormatOpenAIResponse, FormatOpenAI, upstream, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "event: response.completed\n") {
		t.Fatalf("missing response.completed event in:\n%s", text)
	}
	if !strings.Contains(text, "\n\n") {
		t.Fatalf("SSE events must be blank-line terminated:\n%s", text)
	}
}

func TestConvertStream_NonStreamChatObjectToResponses(t *testing.T) {
	upstream := []byte(`{"id":"cmpl-1","object":"chat.completion","created":1,"model":"gpt","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	out, err := ConvertResponse(FormatOpenAIResponse, FormatOpenAI, upstream, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "event: response.completed\n") {
		t.Fatalf("missing response.completed event in:\n%s", text)
	}
	if !strings.Contains(text, "data: ") || !strings.Contains(text, "\n\n") {
		t.Fatalf("SSE framing incomplete:\n%s", text)
	}
}

func TestConvertStream_OpenAIToClaude(t *testing.T) {
	upstream := []byte("data: {\"id\":\"cmpl-1\",\"model\":\"gpt\",\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\ndata: [DONE]\n\n")
	out, err := ConvertResponse(FormatClaude, FormatOpenAI, upstream, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{"event: message_start", "event: content_block_start", "event: content_block_delta", "event: content_block_stop", "event: message_stop", "你", "好"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestConvertRequest_SameProtocolIdentity(t *testing.T) {
	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)
	out, err := ConvertRequest(FormatOpenAI, FormatOpenAI, "/v1/chat/completions", body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Converted {
		t.Fatal("same protocol must stay identity")
	}
	if string(out.Body) != string(body) {
		t.Fatal("identity must keep original body")
	}
}

func TestConvertRequest_UnsupportedImagesToClaude(t *testing.T) {
	body := []byte(`{"prompt":"a cat","model":"gpt-image-2"}`)
	_, err := ConvertRequest(FormatImages, FormatClaude, "/v1/images/generations", body)
	if err == nil {
		t.Fatal("expected 4xx")
	}
	ce, ok := err.(*ConvertError)
	if !ok || ce.Status < 400 || ce.Status >= 500 {
		t.Fatalf("err = %v", err)
	}
}

func TestConvertRequest_ImagesToResponses(t *testing.T) {
	body := []byte(`{"prompt":"a cat","model":"gpt-image-2","size":"1024x1024"}`)
	out, err := ConvertRequest(FormatImages, FormatOpenAIResponse, "/v1/images/generations", body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != "/v1/responses" {
		t.Fatalf("path = %q", out.Path)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "image_generation" {
		t.Fatalf("tool = %v", tool)
	}
}

func TestConvertResponse_ResponsesToImages(t *testing.T) {
	upstream := []byte(`{"id":"resp_1","output":[{"type":"image_generation_call","result":"aGVsbG8="}]}`)
	out, err := ConvertResponse(FormatImages, FormatOpenAIResponse, upstream, false)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	data := got["data"].([]any)
	first := data[0].(map[string]any)
	if first["b64_json"] != "aGVsbG8=" {
		t.Fatalf("data = %v", data)
	}
}

func TestConvertRequest_VideosIdentityOnOpenAI(t *testing.T) {
	body := []byte(`{"model":"sora-2","prompt":"a bird"}`)
	out, err := ConvertRequest(FormatVideos, FormatOpenAI, "/openai/v1/videos", body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Converted {
		t.Fatal("videos on openai channel must be identity")
	}
	if out.Path != "/openai/v1/videos" {
		t.Fatalf("path = %q", out.Path)
	}
}

func TestConvertRequest_VideosUnsupportedOnAnthropic(t *testing.T) {
	_, err := ConvertRequest(FormatVideos, FormatClaude, "/openai/v1/videos", []byte(`{"prompt":"x"}`))
	if err == nil {
		t.Fatal("expected 4xx")
	}
}
