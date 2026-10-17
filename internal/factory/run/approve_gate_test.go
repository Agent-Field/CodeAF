package run

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func misfiled(result *factory.StageResult, gate factory.PhaseState) factory.Item {
	return factory.Item{
		State: factory.StateNeedsYou, Question: factory.ApproveQuestion("plan"), QKind: factory.QKindApprove,
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageCheck, State: factory.PhaseWaiting, Round: 1, Result: result},
			{Name: factory.ApproveName, Kind: factory.StageGate, State: gate},
			{Name: "write", Kind: factory.StageCheck, State: factory.PhasePending},
		}},
	}
}

func TestAnswerHeldRepairsOnlyTheMisfiledGateShape(t *testing.T) {
	now := time.Now()
	t.Run("finished step before a pending gate is mended by a yes", func(t *testing.T) {
		it := misfiled(&factory.StageResult{Done: true}, factory.PhasePending)
		if err := AnswerHeld(&it, true, now); err != nil {
			t.Fatal(err)
		}
		p := it.Stream.Phases
		if p[0].State != factory.PhaseDone || p[1].State != factory.PhaseDone || p[2].State != factory.PhasePending || it.Stream.Cur != 1 {
			t.Fatalf("phases %+v cur %d", p, it.Stream.Cur)
		}
	})
	t.Run("no result: not restored, the ordinary path runs the step again", func(t *testing.T) {
		it := misfiled(nil, factory.PhasePending)
		if err := AnswerHeld(&it, true, now); err != nil {
			t.Fatal(err)
		}
		p := it.Stream.Phases
		if p[0].State != factory.PhaseRunning || p[1].State != factory.PhasePending || it.Stream.Cur != 0 {
			t.Fatalf("phases %+v cur %d", p, it.Stream.Cur)
		}
	})
	t.Run("a result that is not done is not restored", func(t *testing.T) {
		it := misfiled(&factory.StageResult{}, factory.PhasePending)
		_ = AnswerHeld(&it, true, now)
		if it.Stream.Phases[0].State != factory.PhaseRunning || it.Stream.Phases[1].State != factory.PhasePending {
			t.Fatalf("phases %+v", it.Stream.Phases)
		}
	})
	t.Run("a no never repairs or passes the gate", func(t *testing.T) {
		it := misfiled(&factory.StageResult{Done: true}, factory.PhasePending)
		_ = AnswerHeld(&it, false, now)
		if it.Stream.Phases[0].State != factory.PhaseRunning || it.Stream.Phases[1].State != factory.PhasePending {
			t.Fatalf("phases %+v", it.Stream.Phases)
		}
	})
	t.Run("a gate that is not pending is not skipped", func(t *testing.T) {
		it := misfiled(&factory.StageResult{Done: true}, factory.PhaseDone)
		_ = AnswerHeld(&it, true, now)
		if it.Stream.Phases[0].State != factory.PhaseRunning || it.Stream.Cur != 0 {
			t.Fatalf("phases %+v cur %d", it.Stream.Phases, it.Stream.Cur)
		}
	})
	t.Run("a gate further on is not skipped to", func(t *testing.T) {
		it := misfiled(&factory.StageResult{Done: true}, factory.PhasePending)
		it.Stream.Phases[1] = factory.Phase{Name: "mid", Kind: factory.StageCheck, State: factory.PhasePending}
		it.Stream.Phases[2] = factory.Phase{Name: factory.ApproveName, Kind: factory.StageGate, State: factory.PhasePending}
		_ = AnswerHeld(&it, true, now)
		if it.Stream.Phases[0].State != factory.PhaseRunning || it.Stream.Phases[2].State != factory.PhasePending {
			t.Fatalf("phases %+v", it.Stream.Phases)
		}
	})
	t.Run("another kind of question never repairs", func(t *testing.T) {
		it := misfiled(&factory.StageResult{Done: true}, factory.PhasePending)
		it.QKind = qkindStep
		_ = AnswerHeld(&it, true, now)
		if it.Stream.Phases[0].State != factory.PhaseRunning || it.Stream.Phases[1].State != factory.PhasePending {
			t.Fatalf("phases %+v", it.Stream.Phases)
		}
	})
}

func TestAnswerUnheldNoSendsBackWithTheWords(t *testing.T) {
	now := time.Now()
	gateWaiting := func() factory.Item {
		it := misfiled(&factory.StageResult{Done: true}, factory.PhaseWaiting)
		it.Stream.Phases[0].State = factory.PhaseDone
		it.Stream.Cur = 1
		it.Asking = &factory.Asked{Stage: factory.ApproveName, With: factory.AskedYou}
		return it
	}
	check := func(t *testing.T, it factory.Item) {
		t.Helper()
		p := it.Stream.Phases
		if p[0].State != factory.PhasePending || p[0].Round != 2 || p[1].State != factory.PhasePending || p[2].State != factory.PhasePending || it.Stream.Cur != 0 {
			t.Fatalf("phases %+v cur %d", p, it.Stream.Cur)
		}
		if it.State != factory.StateQueued || it.Question != "" || it.QKind != "" || it.Asking != nil {
			t.Fatalf("state %s question %q qkind %q asking %+v", it.State, it.Question, it.QKind, it.Asking)
		}
		if len(it.Notes) != 1 || it.Notes[0] != "use the other file" {
			t.Fatalf("notes %q", it.Notes)
		}
	}
	t.Run("the gate waiting", func(t *testing.T) {
		it := gateWaiting()
		if err := AnswerUnheld(&it, false, " use the other file ", now); err != nil {
			t.Fatal(err)
		}
		check(t, it)
	})
	t.Run("the stored misfiled shape", func(t *testing.T) {
		it := misfiled(&factory.StageResult{Done: true}, factory.PhasePending)
		if err := AnswerUnheld(&it, false, "use the other file", now); err != nil {
			t.Fatal(err)
		}
		check(t, it)
	})
	t.Run("a yes passes only its own gate", func(t *testing.T) {
		it := gateWaiting()
		if err := AnswerUnheld(&it, true, "go", now); err != nil {
			t.Fatal(err)
		}
		p := it.Stream.Phases
		if p[1].State != factory.PhaseDone || p[2].State != factory.PhasePending || it.State != factory.StateQueued || len(it.Notes) != 0 {
			t.Fatalf("phases %+v state %s notes %q", p, it.State, it.Notes)
		}
	})
	t.Run("no step before the gate stops, as the runner does", func(t *testing.T) {
		it := gateWaiting()
		it.Stream.Phases[0].State = factory.PhasePending
		if err := AnswerUnheld(&it, false, "", now); err != nil {
			t.Fatal(err)
		}
		if it.State != factory.StateNew || it.Asking != nil || it.Question != "" || it.QKind != "" || it.Stream.Phases[1].State != factory.PhasePending {
			t.Fatalf("stopped job retained its question: state %s asking %+v phases %+v", it.State, it.Asking, it.Stream.Phases)
		}
	})
}
