package config

import (
	"testing"

	"gorm.io/datatypes"
)

func TestValidateGroupConfigOverrides_SessionAffinityTTL(t *testing.T) {
	sm := NewSystemSettingsManager()

	if err := sm.ValidateGroupConfigOverrides(map[string]any{
		"session_affinity_ttl": "1h",
	}); err != nil {
		t.Fatalf("valid ttl rejected: %v", err)
	}

	if err := sm.ValidateGroupConfigOverrides(map[string]any{
		"session_affinity_ttl": "not-a-duration",
	}); err == nil {
		t.Fatal("expected invalid ttl to be rejected")
	}

	if err := sm.ValidateGroupConfigOverrides(map[string]any{
		"session_affinity_ttl": "0s",
	}); err == nil {
		t.Fatal("expected zero ttl to be rejected")
	}
}

func TestGetEffectiveConfig_MissingRoutingKeysStayOff(t *testing.T) {
	sm := NewSystemSettingsManager()
	cfg := sm.GetEffectiveConfig(nil)
	if cfg.EnableProtocolRouting {
		t.Fatal("protocol routing must default to false")
	}
	if cfg.EnableChannelAffinity {
		t.Fatal("channel affinity must default to false")
	}
	if cfg.SessionAffinityTTL != "1h" {
		t.Fatalf("session affinity ttl = %q, want 1h", cfg.SessionAffinityTTL)
	}

	cfg = sm.GetEffectiveConfig(datatypes.JSONMap{
		"max_retries": float64(5),
	})
	if cfg.EnableProtocolRouting || cfg.EnableChannelAffinity {
		t.Fatal("missing routing keys must stay disabled")
	}
	if cfg.MaxRetries != 5 {
		t.Fatalf("max_retries = %d, want 5", cfg.MaxRetries)
	}

	enabled := true
	ttl := "30m"
	cfg = sm.GetEffectiveConfig(datatypes.JSONMap{
		"enable_protocol_routing": enabled,
		"enable_channel_affinity": enabled,
		"session_affinity_ttl":    ttl,
	})
	if !cfg.EnableProtocolRouting || !cfg.EnableChannelAffinity {
		t.Fatal("explicit routing flags must be applied")
	}
	if cfg.SessionAffinityTTL != "30m" {
		t.Fatalf("ttl = %q, want 30m", cfg.SessionAffinityTTL)
	}
}
