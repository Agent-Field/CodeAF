package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"strings"
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
	f := startStepFactory(t)
	wire, id := attachStep(t, f)
	f.reply = `{"factory":{"name":"acme"},"steps":[{"name":"securityrevi","label":"Security review"}],"version":5,"lines":[]}`
	var got appwire.FactoryJobStepEdited
	wire.must("factory.job_step_edit", map[string]any{
		"factory": id, "job": 7, "expect": map[string]any{"version": 4},
		"edit": map[string]any{"add": []any{map[string]any{
			"step": map[string]any{"name": "securityrevi", "label": "Security review", "ask": "review security", "until": "clean"}, "after": "write",
		}}},
	}, &got)
	if len(f.bodies) != 1 || !strings.Contains(f.bodies[0], `"label":"Security review"`) {
		t.Fatalf("desktop dropped added label: %v", f.bodies)
	}
}
