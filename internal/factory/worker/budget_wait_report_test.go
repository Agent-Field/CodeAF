package worker_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestBudgetWaitRetainsLeaseForLateUpdates(t *testing.T) {
	released := make(chan int, 10)
	var calls atomic.Int32
	g := newRigWith(t, func(_ string, held api.WorkerLease) {
		if held.ID == "" {
			released <- held.Job
		}
	}, func(o *run.Options) {
		o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
			calls.Add(1)
			return factory.StageResult{Done: true, Spent: 1}, nil
		})}
	})
	id := g.add(t, check("check"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Cap = 1
		it.Notes = []string{"initial"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.floor.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateNeedsYou)
	if err := g.mirror.Update(id, func(it *factory.Item) error { it.Notes = append(it.Notes, "late parked update"); return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case job := <-released:
		t.Fatalf("worker released job %d after a late update while waiting at its cap", job)
	case <-time.After(300 * time.Millisecond):
	}
	if err := g.floor.Answer(id, true, "continue"); err != nil {
		t.Fatal(err)
	}
	final := g.waitFor(t, id, factory.StateLanded)
	if calls.Load() != 1 {
		t.Errorf("completed step executed %d times, want 1", calls.Load())
	}
	found := false
	for _, note := range final.Notes {
		if note == "late parked update" {
			found = true
		}
	}
	if !found {
		t.Errorf("late update was lost after continuation: notes=%q reports=%s", final.Notes, g.seen.all())
	}
}
