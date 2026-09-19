package run

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The law these tests state: the run knows which limit ended it when it decides
// it, and the fact crosses to the caller beside the outcome word. The outcome
// stays one sentence for both limits — the exit ladder's own — and the limit
// fact is what says which, so a person who set both is told which one fired.

// TestStartNamesTheTimeLimit runs a real supervisor over a store whose worker
// blocks until the run ends it, with both a dollar limit and an elapsed limit
// set and the clock the one that fires. The supervisor's driven clock is not
// reachable through Start, so the limit is small real time — the worker holds
// the run open until the limit cuts it, whatever the box's load.
func TestStartNamesTheTimeLimit(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "time-limit", "root", "root", "run until the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	ended := make(chan struct{})
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			<-ctx.Done()
			close(ended)
			return Report{}, ctx.Err()
		})
	}
	outcome, summary := Start(context.Background(), Spec{
		Store: store, Workspace: t.TempDir(), Title: "run", Brief: "run until the limit",
		Limits:  Limits{CostUSD: 100, Elapsed: 20 * time.Millisecond},
		Factory: factory,
	})
	if outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	<-ended
	if summary.Limit != LimitTime {
		t.Fatalf("limit fact = %q, want the time limit that fired", summary.Limit)
	}
}

// TestStartNamesTheCostLimit runs a real supervisor whose worker banks past the
// run's dollar limit while it works, the road a long task crosses the limit on.
func TestStartNamesTheCostLimit(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "cost-limit", "root", "root", "run until the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			bankSpend(ctx, 1.25)
			<-ctx.Done()
			return Report{USD: 1.25}, ctx.Err()
		})
	}
	outcome, summary := Start(context.Background(), Spec{
		Store: store, Workspace: t.TempDir(), Title: "run", Brief: "run until the limit",
		Limits:  Limits{CostUSD: 1, Elapsed: time.Hour},
		Factory: factory,
	})
	if outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	if summary.Limit != LimitCost {
		t.Fatalf("limit fact = %q, want the dollar limit that fired", summary.Limit)
	}
}

// TestRunLimitCrossesTheSeamAsItself keeps the fact a fact across the engine
// wire: each limit maps one for one onto the session's own words, and a limit
// this build does not know reads as none rather than as a guess.
func TestRunLimitCrossesTheSeamAsItself(t *testing.T) {
	if got := runLimitOf(LimitTime); got != session.RunLimitTime {
		t.Fatalf("time limit crossed the seam as %q", got)
	}
	if got := runLimitOf(LimitCost); got != session.RunLimitCost {
		t.Fatalf("cost limit crossed the seam as %q", got)
	}
	if got := runLimitOf(Limit("unheard")); got != "" {
		t.Fatalf("an unknown limit crossed the seam as %q, want none", got)
	}
}
