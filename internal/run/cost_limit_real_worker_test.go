package run_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// TestCostRemainderEndsARealBashWorker holds the worker-side acceptance behind
// the conversation door's RunSpec test: a conversation that has spent $1.50 of
// its $2 --max-cost hands this run $0.50. The real BashWorker pays $0.25 on
// each scripted seat call, reports those calls while it is working, and the
// existing limit road cancels and absorbs its return at the remainder. The seat
// has no artificial delay: the live spend notification lets the limit cancel
// the worker before another
// provider call, making the boundary deterministic without a sleep.
func TestCostRemainderEndsARealBashWorker(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)

	var calls, paid atomic.Int32
	seat := &seat{ever: func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		if calls.Add(1) > 2 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		paid.Add(1)
		reply := textReply("still working")
		cost := 0.25
		reply.Usage.Cost = &cost
		return reply, nil
	}}
	factory := func(plandb.Task) run.Worker {
		return run.NewBashWorker(store, t.TempDir(), "test/model", seat)
	}

	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store,
		Slots: 1,
		Limits: run.Limits{
			CostUSD: 0.50,
		},
		Factory: factory,
	})

	if outcome != run.OutcomeLimit {
		t.Fatalf("outcome = %q, want the existing limit road %q", outcome, run.OutcomeLimit)
	}
	if summary.USD != 0.50 {
		t.Fatalf("run spend = %v, want the $0.50 remainder exactly once after the worker return was absorbed", summary.USD)
	}
	if got := paid.Load(); got != 2 {
		t.Fatalf("paid scripted seat calls = %d, want the limit to stop the real worker at two", got)
	}
}
