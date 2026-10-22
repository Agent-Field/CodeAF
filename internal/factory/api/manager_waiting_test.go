package api

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestPendingDecisionProjectsCurrentPhaseAsWaiting(t *testing.T) {
	it := factory.Item{State: factory.StateNeedsYou, Question: "Which path?", QKind: "step", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning}, {Name: "write", State: factory.PhasePending}}}}
	got := FromItem(it)
	if len(got.Steps) != 2 || got.Steps[0].State != StepWaiting || got.Steps[1].State != StepPending || it.Stream.Phases[0].State != factory.PhaseRunning {
		t.Fatal("projection changed work or left an idle working phase")
	}
}
