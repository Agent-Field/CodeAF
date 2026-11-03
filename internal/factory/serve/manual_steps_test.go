package serve_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// manualAsk is a Manual job standing at the ask before its second step.
func manualAsk(mode string) factory.Item {
	return factory.Item{
		Autonomy: mode, State: factory.StateNeedsYou, Assignee: "sam", Cap: 10,
		QKind: factory.QKindApprove, Question: factory.NextStepQuestion("test"),
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageCheck, On: true}, {Name: "test", Kind: factory.StageCheck, On: true}},
		Stream: &factory.Stream{Cur: 1, Spent: 1, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1},
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseWaiting},
		}},
	}
}

// The job page reads the ask as an approval the person may press or send back,
// naming the step before as where a no goes.
func TestManualAskBeforeAStepOffersApproveAndSendBack(t *testing.T) {
	f := newV2Jobs(t)
	j := f.read(t, f.create(t, manualAsk(factory.AutonomyManual)))
	if j.NeedsYou == nil || j.NeedsYou.Kind != factory.NeedsApprovePlan || j.Approval == nil || j.Approval.State != "waiting" || !j.Can.Approve || !j.Can.RejectPlan {
		t.Fatalf("needs=%+v approval=%+v can=%+v", j.NeedsYou, j.Approval, j.Can)
	}
}

// A switch to Auto passes the standing ask through the recorded door: Auto stops
// only at OK steps, so nobody is left answering a question the mode took back.
func TestSwitchToAutoPassesTheStandingAsk(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, manualAsk(factory.AutonomyAuto))
	f.read(t, it)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Approval == nil || !saved.Approval.Automatic {
		t.Fatalf("Auto left the ask standing: state=%s approval=%+v", saved.State, saved.Approval)
	}
}
