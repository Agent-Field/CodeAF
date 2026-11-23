package worker_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
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

// MANUAL HOLDS NO STEP FOR THE PERSON'S WORD BEFORE IT RUNS (the owner's
// decision of 2026-11-17). Manual is about the plan and about anything
// public; it used to ask "test is next · continue?" before every step, on the
// real service, lease and worker road, and now runs them on.
func TestManualRunsEachStepWithoutAskingOnTheRealRoad(t *testing.T) {
	g := newRig(t)
	id := manualJob(t, g, named("write"), named("test"))
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateLanded)
	if phaseState(it, "write") != factory.PhaseDone || phaseState(it, "test") != factory.PhaseDone {
		t.Fatalf("steps at landing: %+v", it.Stream.Phases)
	}
	for _, line := range it.Stream.Log {
		if line.Tone == "ask" {
			t.Fatalf("Manual asked before a step: %q", line.Text)
		}
	}
	if ph := it.Stream.Phases[1]; ph.Result == nil || ph.Round != 1 {
		t.Fatalf("the step did not run once: %+v", ph)
	}
}

// The pipeline's own approve step still asks, once, and the step after it runs.
func TestManualStillAsksAtAnApproveStep(t *testing.T) {
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

// A JOB SAVED WHILE IT WAITED ON MANUAL'S OLD HOLD STILL TAKES ITS ANSWER. The
// hold is no longer made, but a job stopped on "test is next" before the
// change is still there: a yes queues the step, and it runs when a worker
// pulls it; a no sends the step before it back with the words.
func TestAJobSavedOnManualsOldHoldStillTakesItsAnswer(t *testing.T) {
	it := factory.Item{ID: 1, Autonomy: factory.AutonomyManual, State: factory.StateNeedsYou, QKind: factory.QKindApprove, Question: factory.NextStepQuestion("test"),
		Stages: []factory.Stage{named("write"), named("test")},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1},
			{Name: "test", Kind: factory.StageCheck, State: factory.PhaseWaiting},
		}}}
	if kind := factory.NeedsKind(it); kind != factory.NeedsApproveStep {
		t.Fatalf("the saved hold reads as %q", kind)
	}
	no := it
	no.Stream = &factory.Stream{Cur: 1, Phases: append([]factory.Phase(nil), it.Stream.Phases...)}
	if err := run.AnswerUnheld(&it, true, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.State != factory.StateQueued || it.Question != "" || it.Stream.Phases[1].State != factory.PhaseRunning {
		t.Fatalf("after a yes: %s %q %+v", it.State, it.Question, it.Stream.Phases)
	}
	if err := run.AnswerUnheld(&no, false, "use the other file", time.Now()); err != nil {
		t.Fatal(err)
	}
	if p := no.Stream.Phases; no.State != factory.StateQueued || p[0].State != factory.PhasePending || p[0].Round != 2 || p[1].State != factory.PhasePending || p[1].Round != 0 {
		t.Fatalf("after a no: %s, %+v", no.State, p)
	}
}
