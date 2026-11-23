package run

import (
	"context"
	"math"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestReviewOldPendingFallbackJobTotal(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
		return factory.StageResult{Done: true, Spent: .0153, Chat: "second"}, nil
	})}, nil)
	id := g.add("old pending", chat("proof"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Stream = &factory.Stream{Spent: .2025, Paused: true, Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending,
			Round: 2, RoundLimit: 2, Result: &factory.StageResult{Spent: .2025, Chat: "first"}, Rounds: []factory.RoundLine{{Round: 1, Chat: "first"}}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Hold(id, false); err != nil {
		t.Fatal(err)
	}
	got := g.waitState(id, factory.StateLanded)
	if math.Abs(factory.JobSpent(got)-.2178) > 1e-9 {
		t.Fatalf("old pending fallback job total = %.4f, want .2178", factory.JobSpent(got))
	}
}
