package run

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

type roadGWorker func(context.Context, plandb.Task) (Report, error)

func (f roadGWorker) Run(ctx context.Context, task plandb.Task) (Report, error) { return f(ctx, task) }

func TestPassDoesNotAcceptTerminalRootBeforeQueuedLeafReturnGetsItsReview(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "road-g", "root", "The run", "road G")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", Title: "finished leaf", ParentID: "root"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim("leaf", "leaf", "test-owner"); err != nil {
		t.Fatal(err)
	}
	leaf := *store.Task("leaf")
	const result = "leaf finished"
	if _, err := store.Done("leaf", "leaf", result, nil, nil); err != nil {
		t.Fatal(err)
	}
	factory := func(plandb.Task) Worker {
		return roadGWorker(func(context.Context, plandb.Task) (Report, error) {
			return Report{Result: "holds: reviewed"}, nil
		})
	}
	s := NewSupervisor(store, t.TempDir(), 1, Limits{ReviewRound: true}, factory)
	s.rootResult = "integrated"
	s.inFlight = 1
	s.finished <- workerReturn{task: leaf, report: Report{Result: result}}
	// Force the road-G ordering: the root return is absorbed first and tries to
	// complete the terminal tree while the leaf return is already queued.
	s.completeTree()

	if got := s.pass(context.Background(), "root"); got != "" {
		t.Fatalf("pass outcome = %q, want no ending until the queued leaf return is reviewed", got)
	}
	checks := 0
	for _, task := range store.Tasks() {
		if task.Role == plandb.RoleCheck {
			checks++
		}
	}
	if checks != 1 {
		t.Fatalf("review checks = %d, want 1 after absorbing the queued leaf return", checks)
	}
	ret := <-s.finished
	s.inFlight--
	s.absorb(ret)
	s.drain()
}
