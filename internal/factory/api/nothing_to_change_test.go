package api

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A RUN THAT CHANGED NOTHING READS SO (live e2e round 8, job 79): the job is
// done with no pull request, and its state's detail — which the desk reads
// for a finished job with no pull request — is the plain sentence, never a
// bare "Done" with no word why.
func TestADoneJobWhoseRunChangedNothingSaysSoInItsStateDetail(t *testing.T) {
	stage := factory.Stage{Name: "build", Label: "Build on AgentField", Kind: factory.StageAgentField, Target: "planner.build", On: true}
	it := factory.Item{ID: 79, Num: 286, Kind: factory.KindIssue, Repo: "o/r", State: factory.StateShipped, Stages: []factory.Stage{stage},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "build", Kind: factory.StageAgentField, State: factory.PhaseDone, Round: 1,
			Result: &factory.StageResult{Done: true, Output: factory.AgentFieldNothingToChange}}}}}
	j := FromItem(it)
	if j.State != StateDone {
		t.Fatalf("state %q, want done", j.State)
	}
	if p := j.StatePresentation; p.Title != "Nothing to change" || p.Detail != factory.AgentFieldNothingToChange {
		t.Fatalf("presentation %+v", p)
	}
	// Any other finished job with no pull request still reads plain Done.
	it.Stream.Phases[0].Result.Output = "finished"
	if p := FromItem(it).StatePresentation; p.Title != "Done" || p.Detail != "" {
		t.Fatalf("presentation %+v", p)
	}
}
