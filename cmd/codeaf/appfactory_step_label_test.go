package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAppFactoryStepLabelKeepsTheRoutingName(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	for _, label := range []string{"Security review", ""} {
		it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: []factory.Stage{{Name: "securityrevi", Label: label, Kind: factory.StageChat, On: true}}})
		got := alice.job(t, it.ID)
		if len(got.Steps) != 1 || got.Steps[0].Name != "securityrevi" || got.Steps[0].Label != label {
			t.Fatalf("desktop job lost label or routing id: %+v", got.Steps)
		}
	}
}

func TestAppFactoryAddedStepKeepsItsLabel(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true}}})
	alice.act(t, "factory.job_step_edit", it.ID, map[string]any{
		"edit": map[string]any{"add": []any{map[string]any{
			"step": map[string]any{"name": "securityrevi", "label": "Security review", "ask": "review security", "until": "clean"}, "after": "write",
		}}},
	})
	got := alice.job(t, it.ID)
	for _, step := range got.Steps {
		if step.Name == "securityrevi" {
			if step.Label != "Security review" { t.Fatalf("added step lost label: %+v", step) }
			return
		}
	}
	t.Fatal("added step missing")
}
