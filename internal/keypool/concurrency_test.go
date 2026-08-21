package keypool

import (
	"errors"
	"testing"
	"time"

	app_errors "gpt-load/internal/errors"
)

func TestAcquireKey_UnlimitedNoStore(t *testing.T) {
	p, _ := newTestProvider(t)
	ok, err := p.AcquireKey(1, 11, 0, time.Second)
	if err != nil || !ok {
		t.Fatalf("unlimited acquire: ok=%v err=%v", ok, err)
	}
	p.ReleaseKey(1, 11, 0)
}

func TestAcquireKey_CapAndRelease(t *testing.T) {
	p, _ := newTestProvider(t)
	if ok, err := p.AcquireKey(1, 11, 2, time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := p.AcquireKey(1, 11, 2, time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	ok, err := p.AcquireKey(1, 11, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("third acquire must fail")
	}
	p.ReleaseKey(1, 11, 2)
	if ok, err := p.AcquireKey(1, 11, 2, time.Minute); err != nil || !ok {
		t.Fatal("after release should acquire")
	}
}

func TestReleaseKey_PreventsNegative(t *testing.T) {
	p, mem := newTestProvider(t)
	if _, err := p.AcquireKey(1, 11, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	p.ReleaseKey(1, 11, 1)
	p.ReleaseKey(1, 11, 1)
	got, err := mem.HGetAll(inflightHashKey(1))
	if err != nil {
		t.Fatal(err)
	}
	if got["11"] != "0" {
		t.Fatalf("inflight = %q, want 0", got["11"])
	}
}

func TestAcquireKey_ExpiredTTLResets(t *testing.T) {
	p, mem := newTestProvider(t)
	if _, err := p.AcquireKey(1, 11, 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := mem.Delete(inflightExpKey(1, 11)); err != nil {
		t.Fatal(err)
	}
	ok, err := p.AcquireKey(1, 11, 1, time.Minute)
	if err != nil || !ok {
		t.Fatalf("leaked slot should reset, ok=%v err=%v", ok, err)
	}
}

func TestSelectKeyWithOptions_ConcurrencySkipAndFull(t *testing.T) {
	p, mem := newTestProvider(t)
	seedKey(t, p, mem, 6, 61, "sk-a")
	seedKey(t, p, mem, 6, 62, "sk-b")

	opts := SelectOptions{MaxConcurrency: 1, RequestTimeout: time.Minute}
	first, err := p.SelectKeyWithOptions(6, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.SelectKeyWithOptions(6, opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("full key must be skipped")
	}
	_, err = p.SelectKeyWithOptions(6, opts)
	if !errors.Is(err, app_errors.ErrNoActiveKeys) {
		t.Fatalf("want no active keys, got %v", err)
	}
	p.ReleaseKey(6, first.ID, 1)
	p.ReleaseKey(6, second.ID, 1)
}

func TestSelectKeyWithOptions_AffinityCapacityKeepsBinding(t *testing.T) {
	p, mem := newTestProvider(t)
	seedKey(t, p, mem, 7, 71, "sk-a")
	seedKey(t, p, mem, 7, 72, "sk-b")

	opts := SelectOptions{
		EnableAffinity: true,
		SessionID:      "s",
		Model:          "m",
		TTL:            time.Hour,
		MaxConcurrency: 1,
		RequestTimeout: time.Minute,
	}
	bound, err := p.SelectKeyWithOptions(7, opts)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := p.SelectKeyWithOptions(7, opts)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.ID == bound.ID {
		t.Fatal("capacity miss should use another key")
	}
	got, ok := p.GetBinding(7, "s", "m")
	if !ok || got.KeyID != bound.ID {
		t.Fatalf("binding must stay on original key, got %+v", got)
	}
	p.ReleaseKey(7, bound.ID, 1)
	again, err := p.SelectKeyWithOptions(7, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != bound.ID {
		t.Fatalf("after slot free want bound key %d, got %d", bound.ID, again.ID)
	}
}
