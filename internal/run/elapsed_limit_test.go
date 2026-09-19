package run

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// TestElapsedLimitUsesTheLimitEndingRoad states the run-limit law at the
// boundary where it matters: reaching a time limit while work is in flight
// ends and drains that work, starts nothing afterwards, and reports the same
// outcome as every other run limit. Its timer is driven by the test, never by
// elapsed machine time.
func TestElapsedLimitUsesTheLimitEndingRoad(t *testing.T) {
	store, err := plandb.Open(t.TempDir()+"/plan.json", "elapsed-limit", "root", "root", "run until the limit")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	fired := make(chan time.Time, 1)
	started := make(chan struct{})
	ended := make(chan struct{})
	var launches atomic.Int32
	factory := func(task plandb.Task) Worker {
		launches.Add(1)
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			close(started)
			<-ctx.Done()
			if _, err := store.AddMany([]plandb.TaskSpec{{ID: "after-limit", Title: "must not launch", ParentID: task.ID}}); err != nil {
				t.Errorf("add post-limit task: %v", err)
			}
			close(ended)
			return Report{}, ctx.Err()
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 2, Limits{Elapsed: time.Hour}, factory)
	supervisor.after = func(time.Duration) <-chan time.Time { return fired }

	result := make(chan Outcome, 1)
	go func() { result <- supervisor.Run(context.Background()) }()
	<-started
	fired <- time.Time{}

	if outcome := <-result; outcome != OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeLimit)
	}
	select {
	case <-ended:
	default:
		t.Fatal("Run returned before its in-flight worker ended and drained")
	}
	if got := launches.Load(); got != 1 {
		t.Fatalf("worker launches = %d, want only the in-flight root", got)
	}
	if task := store.Task("after-limit"); task == nil || task.Status != plandb.StatusReady {
		t.Fatalf("post-limit task = %#v, want ready and unlaunched", task)
	}
}

type workerFunc func(context.Context, plandb.Task) (Report, error)

func (f workerFunc) Run(ctx context.Context, task plandb.Task) (Report, error) { return f(ctx, task) }
