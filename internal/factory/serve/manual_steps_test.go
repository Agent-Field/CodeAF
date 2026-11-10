package serve_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// manualAsk is a Manual job standing at the ask before its second step.
func manualAsk(mode string) factory.Item {
	return factory.Item{
		Autonomy: mode, State: factory.StateNeedsYou, Assignee: "sam", Cap: 10,
		QKind: factory.QKindApprove, Question: factory.NextStepQuestion("test"),
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageCheck, On: true}, {Name: "test", Kind: factory.StageCheck, Label: "RunVetAndTest", On: true}},
		Stream: &factory.Stream{Cur: 1, Spent: 1, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1},
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseWaiting},
		}},
	}
}

// The job page reads the ask as an approval the person may press or send back,
// naming the step before as where a no goes.
//
// A MANUAL HOLD IS NOT A PLAN. The live desk walk showed "Plan ready" in Needs
// you for "RunVetAndTest is next": the hold was filed as approve_plan, gate
// plan and reason approval_plan. It is its own kind, names the step that is
// next by its label, and says so in a person's words.
func TestManualAskBeforeAStepOffersApproveAndSendBack(t *testing.T) {
	f := newV2Jobs(t)
	j := f.read(t, f.create(t, manualAsk(factory.AutonomyManual)))
	if j.NeedsYou == nil || j.NeedsYou.Kind != factory.NeedsApproveStep || j.Approval == nil || j.Approval.State != "waiting" || !j.Can.Approve || !j.Can.RejectPlan {
		t.Fatalf("needs=%+v approval=%+v can=%+v", j.NeedsYou, j.Approval, j.Can)
	}
	if j.NeedsYou.Step != "RunVetAndTest" || j.NeedsYou.BackTo != "write" || j.Gate != api.GateStep || j.Reason != "approval_step" || j.Approval.Kind != "step" {
		t.Fatalf("needs=%+v gate=%q reason=%q approval=%+v", j.NeedsYou, j.Gate, j.Reason, j.Approval)
	}
	if p := j.StatePresentation; p.Detail != "RunVetAndTest is next · waiting for you to go on" {
		t.Fatalf("presentation %+v", p)
	}
	// An approve step's own question after a plan is still a plan approval.
	plan := manualAsk(factory.AutonomyManual)
	plan.Question = factory.ApproveQuestion("write")
	plan.Stages = append(plan.Stages[:1], factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, plan.Stages[1])
	plan.Stream.Phases = []factory.Phase{plan.Stream.Phases[0], {Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhaseWaiting}, {Name: "test", Kind: factory.StageCheck, State: factory.PhasePending}}
	if k := factory.NeedsKind(plan); k != factory.NeedsApprovePlan {
		t.Fatalf("an approve step read as %s", k)
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
