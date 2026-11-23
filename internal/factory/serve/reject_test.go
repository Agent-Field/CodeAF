package serve_test

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A REJECTION ENDS THE JOB. A job waiting on an approval offers Reject; the
// person's no with words closes it as rejected: done, out of Needs you, the
// run's waiting step closed, the words on it, no Run or carry-on offered and
// nothing a rule would start again.
func TestARejectedApprovalEndsTheJobWithTheWords(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Num: 4, Origin: factory.OriginForge, Assignee: "sam", State: factory.StateNeedsYou, QKind: factory.QKindApprove,
		Question: factory.ApproveQuestion("plan"), Cap: 5,
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting}, {Name: "write"}}}})
	j := f.read(t, it)
	if !j.Can.Reject || j.State != "needs_you" {
		t.Fatalf("a waiting approval does not offer Reject: state %s can %+v", j.State, j.Can)
	}
	f.post(t, it, "reject", fmt.Sprintf(`{"words":"we will not change Total()","expect":{"version":%d}}`, j.Version), 200)
	got := f.read(t, it)
	if got.State != "done" || got.Rejection == nil || got.Rejection.Words != "we will not change Total()" || got.StatePresentation.Title != "Rejected" {
		t.Fatalf("a rejected job does not read rejected: state %s rejection %+v presentation %+v", got.State, got.Rejection, got.StatePresentation)
	}
	if got.Can.Run || got.Can.StartNow || got.Can.Reject || got.Can.Answer || got.Can.Stop || !got.Can.Reopen {
		t.Fatalf("a rejected job offers the wrong doors: %+v", got.Can)
	}
	stored, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Stream.Ended.IsZero() || stored.Stream.Phases[1].State == factory.PhaseWaiting || stored.Question != "" {
		t.Fatalf("the run's record was left open: %+v", stored.Stream)
	}
	// A JOB WAITING ON NOTHING HAS NOTHING TO REJECT.
	other := f.create(t, factory.Item{Num: 5, Origin: factory.OriginForge, Assignee: "sam", State: factory.StateNew})
	f.post(t, other, "reject", "", 409)
}
