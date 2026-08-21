package proxy

import (
	"testing"

	"gpt-load/internal/keypool"
	"gpt-load/internal/models"
	"gpt-load/internal/types"
)

func TestEvaluateBoundSubGroup_ReplaySkipAndPause(t *testing.T) {
	b := &keypool.AffinityBinding{KeyID: 1, SubGroup: "child"}

	replay, paused := evaluateBoundSubGroup(b, &models.Group{
		Name: "child",
		EffectiveConfig: types.SystemSettings{
			EnableChannelAffinity: true,
		},
	})
	if !replay || paused {
		t.Fatalf("want replay, replay=%v paused=%v", replay, paused)
	}

	replay, paused = evaluateBoundSubGroup(b, &models.Group{
		Name:   "child",
		Paused: true,
		EffectiveConfig: types.SystemSettings{
			EnableChannelAffinity: true,
		},
	})
	if replay || !paused {
		t.Fatalf("paused sub-group must 403, replay=%v paused=%v", replay, paused)
	}

	replay, paused = evaluateBoundSubGroup(b, &models.Group{
		Name: "child",
		EffectiveConfig: types.SystemSettings{
			EnableChannelAffinity: false,
		},
	})
	if replay || paused {
		t.Fatalf("affinity off must drop bind, replay=%v paused=%v", replay, paused)
	}

	replay, paused = evaluateBoundSubGroup(b, nil)
	if replay || paused {
		t.Fatal("missing group must cold-start")
	}
}
