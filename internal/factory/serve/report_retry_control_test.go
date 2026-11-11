package serve_test

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestRetrySavedResultsRequestsDeliveryWithoutRequeueingWork(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNeedsYou, Assignee: "sam", Autonomy: factory.AutonomyAuto, Cap: 30, Question: "Report refused", QKind: "results_not_saved", Asking: &factory.Asked{With: factory.AskedYou, Question: "Report refused"}, Stream: recordedRun(), FollowUp: &factory.PRFollowUp{Kind: "pr", State: "running", Attempt: 1}})
	f.post(t, it, "control", `{"mode":"resume_pipeline"}`, 200)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ReportRetryAt.IsZero() || saved.QKind != "results_not_saved" || saved.State != factory.StateNeedsYou || saved.Stream.Spent != it.Stream.Spent || saved.Stream.Phases[1].Chat != it.Stream.Phases[1].Chat || saved.FollowUp.Attempt != 1 {
		t.Fatalf("retry reran or discarded work: state=%v gate=%v", saved.State, saved.QKind)
	}
}
