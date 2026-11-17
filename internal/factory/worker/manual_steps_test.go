package worker_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// manualJob is a Manual job of named checks, past the rig's legacy mode reset.
func manualJob(t *testing.T, g rig, stages ...factory.Stage) int {
	t.Helper()
	id := g.add(t, stages...)
	if err := g.st.Update(id, func(it *factory.Item) error { it.Autonomy = factory.AutonomyManual; return nil }); err != nil {
		t.Fatal(err)
	}
	return id
}

func named(name string) factory.Stage {
	s := check("true")
	s.Name = name
	return s
}

func phaseState(it factory.Item, name string) factory.PhaseState {
	for _, ph := range it.Stream.Phases {
		if ph.Name == name {
			return ph.State
		}
	}
	return ""
}

// Start is the person's word for the first step and every later step waits for
// its own, on the real service, lease and worker road: "Manual: before every step".
func TestManualAsksBeforeEachStepOnTheRealRoad(t *testing.T) {
	g := newRig(t)
	id := manualJob(t, g, named("write"), named("test"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	held := g.waitFor(t, id, factory.StateNeedsYou)
	if phaseState(held, "write") != factory.PhaseDone || phaseState(held, "test") != factory.PhaseWaiting {
		t.Fatalf("steps at the ask: %+v", held.Stream.Phases)
	}
	if held.QKind != factory.QKindApprove || held.Question != factory.NextStepQuestion("test") {
		t.Fatalf("the ask = %q (%s)", held.Question, held.QKind)
	}
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateLanded)
	if ph := it.Stream.Phases[1]; ph.State != factory.PhaseDone || ph.Result == nil || ph.Round != 1 {
		t.Fatalf("the step did not run once after the yes: %+v", ph)
	}
}

// An approve step just passed already asked about the step after it.
func TestManualDoesNotAskTwiceAfterAnApproveStep(t *testing.T) {
	g := newRig(t)
	id := manualJob(t, g, named("plan"), factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate, On: true}, named("write"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	if it := g.waitFor(t, id, factory.StateLanded); phaseState(it, "write") != factory.PhaseDone {
		t.Fatalf("steps: %+v", it.Stream.Phases)
	}
}

// A no is the approve step's no: the step before runs again with the words, and
// the next step asks again once it finishes.
func TestManualNoBeforeAStepSendsTheOneBeforeBack(t *testing.T) {
	g := newRig(t)
	id := manualJob(t, g, named("write"), named("test"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.doors.Answer(id, false, "use the other file"); err != nil {
		t.Fatal(err)
	}
	var again factory.Item
	for again = g.waitFor(t, id, factory.StateNeedsYou); again.Stream.Phases[0].Round < 2; again = g.waitFor(t, id, factory.StateNeedsYou) {
	}
	if again.Stream.Phases[1].Round != 0 || again.Stream.Phases[1].State != factory.PhaseWaiting {
		t.Fatalf("after the no: %+v", again.Stream.Phases)
	}
}

// Auto stops only at OK steps; a pipeline without one never asks on the way.
func TestAutoDoesNotAskBeforeSteps(t *testing.T) {
	g := newRig(t)
	id := g.add(t, named("write"), named("test"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Autonomy, it.Cap, it.Assignee = factory.AutonomyAuto, 5, "me"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	if it := g.waitFor(t, id, factory.StateLanded); it.Question != "" {
		t.Fatalf("Auto asked %q", it.Question)
	}
}

// The person's yes reaches a job no worker holds (the worker restarted while the
// ask stood): the step runs when a worker pulls it, without asking a second time.
func TestManualYesOnAnUnheldJobIsNotAskedAgain(t *testing.T) {
	it := factory.Item{ID: 1, Autonomy: factory.AutonomyManual, State: factory.StateNeedsYou, QKind: factory.QKindApprove, Question: factory.NextStepQuestion("test"),
		Stages: []factory.Stage{named("write"), named("test")},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1},
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseWaiting},
		}}}
	no := it
	no.Stream = &factory.Stream{Cur: 1, Phases: append([]factory.Phase(nil), it.Stream.Phases...)}
	if err := run.AnswerUnheld(&it, true, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.State != factory.StateQueued || factory.AsksBeforeStep(it, 1) {
		t.Fatalf("after a yes: %s, asks again %v, %+v", it.State, factory.AsksBeforeStep(it, 1), it.Stream.Phases)
	}
	if err := run.AnswerUnheld(&no, false, "use the other file", time.Now()); err != nil {
		t.Fatal(err)
	}
	if p := no.Stream.Phases; no.State != factory.StateQueued || p[0].State != factory.PhasePending || p[0].Round != 2 || p[1].State != factory.PhasePending || p[1].Round != 0 {
		t.Fatalf("after a no: %s, %+v", no.State, p)
	}
}

// A step that has run is the same step when it runs again: retries, and a later
// check sending it back, do not ask. A switch to Manual applies from the next
// step that has not run, as the job page says.
func TestManualAsksOnlyBeforeAStepsFirstRound(t *testing.T) {
	it := factory.Item{Autonomy: factory.AutonomyManual, Stream: &factory.Stream{Phases: []factory.Phase{
		{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
		{Name: "test", Kind: factory.StageCheck, State: factory.PhasePending, Round: 2},
		{Name: "review", Kind: factory.StageChat, State: factory.PhasePending},
	}}}
	if factory.AsksBeforeStep(it, 1) {
		t.Fatal("a step sent back asked as if it were new")
	}
	it.Stream.Phases[1] = factory.Phase{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 2}
	if !factory.AsksBeforeStep(it, 2) {
		t.Fatal("a new step did not ask")
	}
	it.Autonomy = ""
	if factory.AsksBeforeStep(it, 2) {
		t.Fatal("a record with no saved mode gained asks it never had")
	}
}

// The job page's Request changes goes through the revision door, with the job's
// version, and the step sent back runs again and the ask comes back.
func TestManualRequestChangesThroughTheRevisionDoorAsksAgain(t *testing.T) {
	g := newRig(t)
	id := manualJob(t, g, named("write"), named("verify"), named("review"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.doors.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	var held factory.Item
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		held, _ = g.st.Get(id)
		if held.Question == factory.NextStepQuestion("review") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never asked before review: %s %+v", held.State, held.Stream)
		}
	}
	desk, err := lease.Open(g.st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	version, err := g.st.Revision(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.NewDoors(desk).RejectPlan(id, version, "check it once more", nil); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		it, _ := g.st.Get(id)
		if it.State == factory.StateNeedsYou && it.Question == factory.NextStepQuestion("review") && it.Stream.Phases[1].Round == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("never asked again: %s %q %+v", it.State, it.Question, it.Stream)
		}
	}
}
