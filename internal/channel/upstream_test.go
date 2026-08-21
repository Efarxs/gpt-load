package channel

import (
	"net/url"
	"testing"
)

func TestBuildUpstreamURLAt_ReplaysSameIndex(t *testing.T) {
	u1, _ := url.Parse("https://a.example.com")
	u2, _ := url.Parse("https://b.example.com")
	ch := &BaseChannel{
		Name: "openai",
		Upstreams: []UpstreamInfo{
			{URL: u1, Weight: 1},
			{URL: u2, Weight: 1},
		},
	}
	req, _ := url.Parse("/proxy/g/v1/chat/completions")
	got, err := ch.BuildUpstreamURLAt(req, "g", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://b.example.com/v1/chat/completions" {
		t.Fatalf("got %q", got)
	}
	if ch.UpstreamBaseURL(1) != "https://b.example.com" {
		t.Fatalf("base = %q", ch.UpstreamBaseURL(1))
	}
	if _, err := ch.BuildUpstreamURLAt(req, "g", 9); err == nil {
		t.Fatal("out of range must fail")
	}
}

func TestBuildUpstreamURL_ReturnsIndex(t *testing.T) {
	u1, _ := url.Parse("https://only.example.com")
	ch := &BaseChannel{
		Name:      "openai",
		Upstreams: []UpstreamInfo{{URL: u1, Weight: 1}},
	}
	req, _ := url.Parse("/proxy/g/v1/chat/completions")
	got, idx, err := ch.BuildUpstreamURL(req, "g")
	if err != nil {
		t.Fatal(err)
	}
	if idx != 0 || got != "https://only.example.com/v1/chat/completions" {
		t.Fatalf("got %q idx=%d", got, idx)
	}
}
