package run

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

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

// A WORKER NEVER WAITS ON THE RUN TO SAY WHAT IT SPENT. The road this pins is
// the one on which nobody is listening: the caller ended the run while one
// worker was still out, so the run drains, and drain waits for that worker and
// reads nothing. A worker that had to be heard before it could go on would hold
// the run open for good, which is an engine that never answers. The worker here
// reports its spend far more often than any buffer is deep, after its context
// has ended, and the run must still come home.
//
// THE ROAD USED TO BE A TREE THAT FINISHED OVER A WORKER STILL OUT. That state is
// gone: a worker that has written its own done has not landed until it returns,
// and the run waits for it ([landed]). The caller's own ending is the road that
// still leaves a worker behind.
func TestAWorkerReportingSpendWhileTheRunDrainsDoesNotHoldTheRunOpen(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "cost-limit-drain", "root", "root", "end the run while a worker is still out")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	started := make(chan struct{})
	var reports atomic.Int32
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			close(started)
			<-ctx.Done()
			for i := 1; i <= 64; i++ {
				bankSpend(ctx, float64(i)/1000)
				reports.Add(1)
			}
			return Report{USD: 0.064}, ctx.Err()
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 2, Limits{CostUSD: 100}, factory)
	ctx, end := context.WithCancel(context.Background())
	defer end()
	answered := make(chan Outcome, 1)
	go func() { answered <- supervisor.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(20 * time.Second):
		t.Fatal("the run never started its worker")
	}
	end()
	select {
	case <-answered:
	case <-time.After(20 * time.Second):
		t.Fatalf("the run never answered: a worker reporting its spend is holding the drain open (%d of 64 reports made)", reports.Load())
	}
	if got := reports.Load(); got != 64 {
		t.Fatalf("spend reports made = %d, want all 64", got)
	}
	// AND THE WORKER THE RUN OUTLIVED WAS STILL PAID FOR. Its words are dropped
	// with its return; its dollars are in the run's count, once.
	if supervisor.spent != 0.064 {
		t.Fatalf("run spend = %v, want the outlived worker's 0.064 counted exactly once", supervisor.spent)
	}
	if supervisor.inFlight != 0 || len(supervisor.finished) != 0 {
		t.Fatalf("the run left work behind it: inFlight=%d, returns unread=%d", supervisor.inFlight, len(supervisor.finished))
	}
}

// WHAT A RUN COUNTS AS SPENT NEVER GOES DOWN, and it includes every paid call
// whatever way its task ended. A person's limit is about dollars, not about how
// the work came out: a task the store cancelled under its worker hands back no
// work the run counts, and its dollars were still paid. With a limit the run
// has already counted them on the way and must not take them back at the
// return; without one the return is where they are counted.
func TestSpendOfAWorkerWhoseTaskTheStoreCancelledStaysInTheRunsCount(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit float64
	}{
		{name: "with a dollar limit", limit: 100},
		{name: "with no dollar limit", limit: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := plandb.Open(t.TempDir()+"/plan.json", "cancelled-spend", "root", "root", "cancel one task part way")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer store.Close()
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "peer", Title: "peer", ParentID: store.RootID()}}); err != nil {
				t.Fatalf("add peer: %v", err)
			}
			peerPaid := make(chan struct{})
			var paidOnce atomic.Bool
			factory := func(task plandb.Task) Worker {
				if task.ID == "peer" {
					return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
						bankSpend(ctx, 0.30)
						if paidOnce.CompareAndSwap(false, true) {
							close(peerPaid)
						}
						<-ctx.Done()
						return Report{Result: "late", Steps: 4, USD: 0.30}, ctx.Err()
					})
				}
				return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
					select {
					case <-peerPaid:
					case <-ctx.Done():
						return Report{}, ctx.Err()
					}
					// Already cancelled on a wake of the root: the first cancellation stands.
					_, _ = store.Cancel("peer", "stopped by hand")
					return Report{Result: "the root's word"}, nil
				})
			}
			supervisor := NewSupervisor(store, t.TempDir(), 2, Limits{CostUSD: test.limit}, factory)
			supervisor.Run(context.Background())
			if peer := store.Task("peer"); peer == nil || peer.Status != plandb.StatusCancelled {
				t.Fatalf("peer = %#v, want cancelled by the store", peer)
			}
			if supervisor.spent != 0.30 {
				t.Fatalf("run spend = %v, want the cancelled task's 0.30 counted exactly once", supervisor.spent)
			}
			if supervisor.steps != 0 {
				t.Fatalf("run steps = %d, want none of the cancelled task's steps", supervisor.steps)
			}
		})
	}
}

// A RUN TELLS ITS OWNER WHAT IT HAS SPENT WHILE IT IS STILL WORKING, AND WITH NO
// DOLLAR LIMIT OF ITS OWN. The conversation that started a run counts the run's
// dollars in its own total, so its limits hold against a run still going; that
// needs the figure while the work is live, whether or not the run was handed a
// limit. What the owner hears only ever rises, and its last word is the run's
// own total, so an owner that adds up the differences counts every dollar once.
func TestARunWithNoDollarLimitStillTellsItsOwnerWhatItHasSpent(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "owner-spend", "root", "root", "spend with nobody limiting it")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	heardLive := make(chan struct{})
	release := make(chan struct{})
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			bankSpend(ctx, 0.25)
			// THE WORKER IS HELD OUT until the owner has heard the live figure,
			// so the first word cannot be the return's.
			select {
			case <-release:
			case <-ctx.Done():
				return Report{}, ctx.Err()
			}
			bankSpend(ctx, 0.75)
			return Report{Result: "done", USD: 0.75}, nil
		})
	}
	var heard []float64
	supervisor := NewSupervisor(store, t.TempDir(), 1, Limits{}, factory)
	supervisor.onSpend = func(total float64) {
		// Called on the run's own goroutine, like everything else that reads
		// its account.
		if len(heard) == 0 || total != heard[len(heard)-1] {
			heard = append(heard, total)
		}
		if len(heard) == 1 {
			close(heardLive)
		}
	}
	answered := make(chan Outcome, 1)
	go func() { answered <- supervisor.Run(context.Background()) }()
	select {
	case <-heardLive:
	case <-time.After(20 * time.Second):
		t.Fatal("the owner heard nothing while the worker was still out")
	}
	close(release)
	select {
	case outcome := <-answered:
		if outcome != OutcomeDone {
			t.Fatalf("outcome = %q, want %q", outcome, OutcomeDone)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the run never answered")
	}
	if len(heard) == 0 || heard[0] != 0.25 {
		t.Fatalf("the owner heard %v, want the live 0.25 first", heard)
	}
	for i := 1; i < len(heard); i++ {
		if heard[i] < heard[i-1] {
			t.Fatalf("the owner heard %v: the figure went down", heard)
		}
	}
	if last := heard[len(heard)-1]; last != 0.75 || supervisor.spent != 0.75 {
		t.Fatalf("the owner's last word = %v and the run's total = %v, want 0.75 for both", last, supervisor.spent)
	}
}
