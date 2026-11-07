package api

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A job an AgentField reasoner runs is filed with the factory's default
// pipeline but never runs it: until its run reports a tree it reads as plain
// Backlog with no steps, and once the run's steps are on the job they are what
// the rail draws and counts.
func TestARunOnJobReadsAsBacklogWithNoStepsUntilItsRunBegins(t *testing.T) {
	it := factory.Item{Num: 7, Kind: factory.KindIssue, Title: "ledger total wrong", State: factory.StateNew,
		RunOn: "workaf-support.handle_ticket",
		Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "approve", Kind: factory.StageGate, On: true}}}
	j := FromItem(it)
	PresentJob(&j)
	if j.StatePresentation.Title != "Backlog" || j.StatePresentation.Detail != "" {
		t.Fatalf("a run_on job waiting to start reads %q / %q, want plain Backlog", j.StatePresentation.Title, j.StatePresentation.Detail)
	}
	if len(j.Steps) != 0 || j.StepsTotal != 0 {
		t.Fatalf("a run_on job not yet run shows %d steps (%d counted); its steps are the run's", len(j.Steps), j.StepsTotal)
	}

	it.AgentField = &factory.AgentFieldRun{ExecutionID: "exec_1", Status: "running"}
	it.Stages = []factory.Stage{{Name: "triage", Kind: factory.StageChat, On: true}, {Name: "investigate", Kind: factory.StageChat, On: true}}
	j = FromItem(it)
	if len(j.Steps) != 2 || j.Steps[0].Name != "triage" || j.StepsTotal != 2 {
		t.Fatalf("once the run began the job reads %d steps (%d counted), want the run's two", len(j.Steps), j.StepsTotal)
	}

	plain := factory.Item{Num: 8, Kind: factory.KindIssue, State: factory.StateNew}
	j = FromItem(plain)
	PresentJob(&j)
	if j.StatePresentation.Title != factory.NoRepoTitle {
		t.Fatalf("outside work with no repository reads %q, want %q", j.StatePresentation.Title, factory.NoRepoTitle)
	}
}
