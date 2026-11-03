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
