package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The law these tests state: the run knows which limit ended it when it decides
// it, and the fact crosses to the caller beside the outcome word. The outcome
// stays one sentence for both limits, the exit ladder's own, and the limit
// fact is what says which, so a person who set both is told which one fired.

// TestStartNamesTheTimeLimit runs a real supervisor over a store whose worker
// blocks until the run ends it, with both a dollar limit and an elapsed limit
// set and the clock the one that fires. The supervisor's driven clock is not
// reachable through Start, so the limit is small real time: the worker holds
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

// TestTheLimitNamedIsTheFirstOneReached cuts a run by its time limit, and the
// worker that cut ends comes home with a receipt past the dollar limit. The
// time limit ended the run; the receipt arrived because of that ending and does
// not rename it. Read only at the return, the dollar limit took the name.
func TestTheLimitNamedIsTheFirstOneReached(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "first-limit", "root", "root", "run until the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			<-ctx.Done()
			return Report{USD: 5}, ctx.Err()
		})
	}
	outcome, summary := Start(context.Background(), Spec{
		Store: store, Workspace: t.TempDir(), Title: "run", Brief: "run until the limit",
		Limits:  Limits{CostUSD: 1, Elapsed: 20 * time.Millisecond},
		Factory: factory,
	})
	if outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	if summary.Limit != LimitTime {
		t.Fatalf("limit fact = %q, want the time limit, which was reached first", summary.Limit)
	}
	if summary.USD != 5 {
		t.Fatalf("run spend = %v, want the receipt counted whichever limit is named", summary.USD)
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

// TestStartNamesTheRowsItsEndingCut keeps the cut fact a fact at the source: a
// run ended by its time limit answers with the store ids of the tasks its own
// ending cut mid-flight, and a task that failed on its own before the ending
// is in no such answer.
func TestStartNamesTheRowsItsEndingCut(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "cut-fact", "root", "root", "run until the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", Title: "leaf", Description: "leaf work", ParentID: "root"}}); err != nil {
		t.Fatalf("add leaf: %v", err)
	}
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if task.ID == "leaf" {
				// A WORKER THAT FAILED ON ITS OWN, before any limit fired.
				return Report{}, errors.New("the leaf broke on its own")
			}
			<-ctx.Done()
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
	cut := map[string]bool{}
	for _, id := range summary.Cut {
		cut[id] = true
	}
	if !cut["root"] {
		t.Fatalf("cut = %v, want the root the time limit took down", summary.Cut)
	}
	if cut["leaf"] {
		t.Fatal("a task that failed on its own before the ending is recorded as cut by it")
	}
}

// A PART A PERSON STOPPED BY ITSELF IS NOT CUT BY THE LIMIT THAT ENDS THE RUN
// LATER. The store cancels the one part, its worker comes home with a cancelled
// context like any worker the run's ending takes down, and the run carries on.
// When the time limit then ends the run, the cut set names the work the limit
// took down and not the part the person had already stopped.
func TestAPartStoppedAloneIsNotCutByTheLimitThatEndsTheRunLater(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "cut-fact", "root", "root", "one part stopped, then the limit")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "stopped", Title: "the part a person stops", ParentID: store.RootID()},
		{ID: "working", Title: "the part still working", ParentID: store.RootID()},
	}); err != nil {
		t.Fatal(err)
	}
	started := make(chan string, 3)
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if task.ID == store.RootID() {
				return Report{Result: "dispatched", Steps: 1}, nil
			}
			started <- task.ID
			<-ctx.Done()
			return Report{}, ctx.Err()
		})
	}
	elapsed := make(chan time.Time)
	supervisor := NewSupervisor(store, t.TempDir(), 3, Limits{Elapsed: time.Hour}, factory)
	supervisor.after = func(time.Duration) <-chan time.Time { return elapsed }
	answered := make(chan Outcome, 1)
	go func() { answered <- supervisor.Run(context.Background()) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(20 * time.Second):
			t.Fatal("the two parts never started")
		}
	}
	if _, err := store.Cancel("stopped", "stopped"); err != nil {
		t.Fatal(err)
	}
	// The limit fires at once. Whichever comes home first, the fact is read
	// from the store and not from the order.
	close(elapsed)
	select {
	case outcome := <-answered:
		if outcome != OutcomeLimit {
			t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the run never answered its limit")
	}
	cut := supervisor.cutIDs()
	if len(cut) != 1 || cut[0] != "working" {
		t.Fatalf("cut = %q, want only the part the limit took down", cut)
	}
}
