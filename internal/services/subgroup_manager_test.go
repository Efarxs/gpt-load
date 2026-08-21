package services

import (
	"errors"
	"testing"

	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
)

func TestCreateSelectorSkipsPausedSubGroups(t *testing.T) {
	m := NewSubGroupManager(nil)
	agg := &models.Group{
		ID:        1,
		Name:      "agg",
		GroupType: "aggregate",
		SubGroups: []models.GroupSubGroup{
			{SubGroupID: 2, SubGroupName: "paused", SubGroupPaused: true, Weight: 80},
			{SubGroupID: 3, SubGroupName: "ok", SubGroupPaused: false, Weight: 20},
		},
	}

	sel := m.createSelector(agg)
	if sel == nil {
		t.Fatal("expected selector when at least one sub-group is enabled")
	}
	if len(sel.subGroups) != 1 || sel.subGroups[0].name != "ok" {
		t.Fatalf("paused sub-groups must be skipped, got %+v", sel.subGroups)
	}
}

func TestCreateSelectorNilWhenAllPaused(t *testing.T) {
	m := NewSubGroupManager(nil)
	agg := &models.Group{
		ID:        1,
		Name:      "agg",
		GroupType: "aggregate",
		SubGroups: []models.GroupSubGroup{
			{SubGroupID: 2, SubGroupName: "paused", SubGroupPaused: true, Weight: 100},
		},
	}

	if sel := m.createSelector(agg); sel != nil {
		t.Fatal("expected nil selector when every sub-group is paused")
	}
}

func TestSelectSubGroupAllPausedReturns403Error(t *testing.T) {
	m := NewSubGroupManager(nil)
	agg := &models.Group{
		ID:        1,
		Name:      "agg",
		GroupType: "aggregate",
		SubGroups: []models.GroupSubGroup{
			{SubGroupID: 2, SubGroupName: "paused", SubGroupPaused: true, Weight: 100},
		},
	}

	_, err := m.SelectSubGroup(agg)
	if !errors.Is(err, app_errors.ErrGroupPaused) {
		t.Fatalf("want ErrGroupPaused, got %v", err)
	}
}

func TestAllSubGroupsPaused(t *testing.T) {
	if allSubGroupsPaused(nil) {
		t.Fatal("nil group is not paused")
	}
	if allSubGroupsPaused(&models.Group{}) {
		t.Fatal("empty sub-groups is not paused")
	}

	mixed := &models.Group{
		SubGroups: []models.GroupSubGroup{
			{SubGroupPaused: true},
			{SubGroupPaused: false},
		},
	}
	if allSubGroupsPaused(mixed) {
		t.Fatal("mixed pause state should not count as all paused")
	}

	allPaused := &models.Group{
		SubGroups: []models.GroupSubGroup{
			{SubGroupPaused: true},
			{SubGroupPaused: true},
		},
	}
	if !allSubGroupsPaused(allPaused) {
		t.Fatal("all paused sub-groups should be detected")
	}
}
