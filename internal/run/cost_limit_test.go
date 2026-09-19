package run

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestCostLimitEndsAndAbsorbsWorkingWorker(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "cost-limit", "root", "root", "run until the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ended := make(chan struct{})
	var launches atomic.Int32
	factory := func(task plandb.Task) Worker {
		launches.Add(1)
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			bankSpend(ctx, 1.25)
			<-ctx.Done()
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "after-limit", Title: "must not launch", ParentID: task.ID}}); err != nil {
				t.Errorf("add post-limit task: %v", err)
			}
			close(ended)
			return Report{USD: 1.25}, ctx.Err()
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 2, Limits{CostUSD: 1}, factory)
	if outcome := supervisor.Run(context.Background()); outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	select {
	case <-ended:
	default:
		t.Fatal("Run returned before its in-flight worker ended and was absorbed")
	}
	if supervisor.spent != 1.25 {
		t.Fatalf("run spend = %v, want 1.25 exactly once", supervisor.spent)
	}
	if supervisor.inFlight != 0 || len(supervisor.cancels) != 0 {
		t.Fatalf("run retained workers: inFlight=%d cancels=%d", supervisor.inFlight, len(supervisor.cancels))
	}
	if got := launches.Load(); got != 1 {
		t.Fatalf("worker launches = %d, want only the worker cut at the limit", got)
	}
	if task := store.Task("after-limit"); task == nil || task.Status != plandb.StatusReady {
		t.Fatalf("post-limit task = %#v, want ready and unlaunched", task)
	}
}

func TestCostLimitSumsWorkingWorkers(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "cost-limit-sum", "root", "root", "run until the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "peer", Title: "peer", ParentID: store.RootID()}}); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	bothStarted := make(chan struct{})
	var started atomic.Int32
	var ended atomic.Int32
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if started.Add(1) == 2 {
				close(bothStarted)
			}
			<-bothStarted
			bankSpend(ctx, 0.6)
			<-ctx.Done()
			ended.Add(1)
			return Report{USD: 0.6}, ctx.Err()
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 2, Limits{CostUSD: 1}, factory)
	if outcome := supervisor.Run(context.Background()); outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	if got := ended.Load(); got != 2 {
		t.Fatalf("absorbed worker returns = %d, want 2", got)
	}
	if supervisor.spent != 1.2 {
		t.Fatalf("run spend = %v, want 1.2 exactly once", supervisor.spent)
	}
	if supervisor.inFlight != 0 || len(supervisor.cancels) != 0 {
		t.Fatalf("run retained workers: inFlight=%d cancels=%d", supervisor.inFlight, len(supervisor.cancels))
	}
}

func TestSpendBankWithoutLimitLetsWorkerReturn(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "unlimited", "root", "root", "finish")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			bankSpend(ctx, 3)
			return Report{Result: "done", USD: 3}, nil
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 1, Limits{}, factory)
	if outcome := supervisor.Run(context.Background()); outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeDone)
	}
	if supervisor.spent != 3 {
		t.Fatalf("run spend = %v, want returned 3 exactly once", supervisor.spent)
	}
}

func TestCostLimitCountsReturnedWorkerBelowCeilingOnce(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "cost-limit-under", "root", "root", "finish below the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			bankSpend(ctx, 0.4)
			bankSpend(ctx, 0.9)
			return Report{Result: "done", USD: 0.9}, nil
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 1, Limits{CostUSD: 1}, factory)
	if outcome := supervisor.Run(context.Background()); outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeDone)
	}
	if supervisor.spent != 0.9 {
		t.Fatalf("run spend = %v, want cumulative 0.9 exactly once", supervisor.spent)
	}
}
