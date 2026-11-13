package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE OWNER'S #22, replayed on the service's copy. The worker reported the
// plan done with the approve step's question, then — before Auto's approval
// was replayed — reported the plan step again (its round costs and chat
// settled a moment later). That second report moved the run's current step
// back to the plan, so the approval's answer looked at the plan, found nothing
// waiting there, and took the question down without passing the approve step.
// The run went on, and the proof approval later refused with "an approve step
// nobody has passed yet". The approval must pass the approve step it answers,
// wherever a later report left the current step.
func TestAnApprovalPassesItsApproveStepAfterALaterReportMovedTheCurrentStep(t *testing.T) {
	now := time.Date(2026, 11, 7, 13, 18, 39, 0, time.UTC)
	it := factory.Item{ID: 70, Autonomy: factory.AutonomyAuto, State: factory.StateRunning, Starter: "me", Cap: 5,
		Stages: []factory.Stage{
			{Name: "plan", Kind: factory.StageChat, On: true},
			{Name: "approve", Kind: factory.StageGate, On: true},
			{Name: "write", Kind: factory.StageChat, On: true},
		},
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1},
			{Name: "approve", Kind: factory.StageGate, State: factory.PhasePending},
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
		}}}
	first := factory.RunReport{
		Stages:       []factory.StageReport{{Name: "plan", State: factory.PhaseDone, Round: 1}},
		Question:     &factory.Asked{Question: factory.ApproveQuestion("plan"), Stage: "approve"},
		QuestionKind: factory.QKindApprove,
	}
	if err := ApplyReport(&it, first, now); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Cur != 1 || it.Stream.Phases[1].State != factory.PhaseWaiting {
		t.Fatalf("the question did not stand at the approve step: cur %d %+v", it.Stream.Cur, it.Stream.Phases[1])
	}
	// The plan step reported again: its round's cost settled.
	again := factory.RunReport{Stages: []factory.StageReport{{Name: "plan", State: factory.PhaseDone, Round: 1, RoundCosts: map[int]float64{1: 0.02}}}}
	if err := ApplyReport(&it, again, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Cur != 1 {
		t.Fatalf("a report of a finished step moved the run off the approve step it waits at: cur %d", it.Stream.Cur)
	}
	if err := AnswerHeld(&it, true, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := it.Stream.Phases[1].State; got != factory.PhaseDone {
		t.Fatalf("the approval did not pass the approve step: %s", got)
	}
}

// Even when the current step points elsewhere (a record written before the
// report kept its place), a yes to an approval passes the one approve step
// that waits, never nothing.
func TestAYesPassesTheWaitingApproveStepWhereverTheCurrentStepPoints(t *testing.T) {
	now := time.Date(2026, 11, 7, 13, 18, 41, 0, time.UTC)
	it := factory.Item{ID: 70, State: factory.StateNeedsYou, Question: factory.ApproveQuestion("plan"), QKind: factory.QKindApprove,
		Stages: []factory.Stage{{Name: "plan", On: true}, {Name: "approve", Kind: factory.StageGate, On: true}, {Name: "write", On: true}},
		Asking: &factory.Asked{Question: factory.ApproveQuestion("plan"), Stage: "approve"},
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
			{Name: "approve", Kind: factory.StageGate, State: factory.PhaseWaiting},
			{Name: "write", Kind: factory.StageChat, State: factory.PhasePending},
		}}}
	if err := AnswerHeld(&it, true, now); err != nil {
		t.Fatal(err)
	}
	if it.Stream.Phases[1].State != factory.PhaseDone || it.Stream.Cur != 1 {
		t.Fatalf("the waiting approve step was not passed: cur %d %+v", it.Stream.Cur, it.Stream.Phases[1])
	}
	if factory.UnpassedGate(it) != "" {
		t.Fatalf("an approve step still reads unpassed: %s", factory.UnpassedGate(it))
	}
}
