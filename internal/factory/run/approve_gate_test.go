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
