package run

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

type parkedRootWorker func(context.Context, plandb.Task) (Report, error)

func (f parkedRootWorker) Run(ctx context.Context, task plandb.Task) (Report, error) {
	return f(ctx, task)
}

// A PARKED ROOT'S LATE FINISH DOES NOT CLOSE THE RUN OVER UNREVIEWED WORK. This
// is the ordering five loaded runs in four hundred produced and none produced
// quiet: the root adds a child and parks on it; a round the root's worker had
// already started still ends its tool, and that tool is the root's own finish,
// arriving once the child has written its own done. The store used to admit it
// through the root worker's exception, the run then answered done, and the child
// was never reviewed. The late finish is refused now, so the run goes the
// ordinary way: the child's return seats its review, the review lands, the root
// is woken, and the woken worker's word is the run's answer.
//
// Forced with channels, no sleeps, in the order the loaded runs had it: the
// child's done lands in the store, THEN the root's late finish arrives, and only
// then does the child's worker come home. The child's return is what seats its
// review, so while it is held back the store shows a root whose every child is
// finished and nothing yet stands in the late finish's way but this law.
func TestAParkedRootsLateFinishDoesNotCloseTheRunOverAnUnreviewedChild(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "parked-root", "root", "The run", "park on one child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID := store.RootID()
	const late, woken = "the late round's word", "integrated after the wake"

	childDone := make(chan struct{})
	lateTried := make(chan struct{})
	var rootLaunches atomic.Int32
	var lateErr atomic.Value
	factory := func(task plandb.Task) Worker {
		switch {
		case task.ID == rootID:
			return parkedRootWorker(func(ctx context.Context, task plandb.Task) (Report, error) {
				if rootLaunches.Add(1) > 1 {
					// THE WOKEN WORKER: the flag was cleared before it ran, so
					// its finish is an ordinary one.
					if _, err := store.Done(task.ID, task.ID, woken, nil, nil); err != nil {
						return Report{}, err
					}
					return Report{Result: woken, Steps: 1}, nil
				}
				if _, err := store.AddMany([]plandb.TaskSpec{{ID: "c1", Title: "the child", ParentID: task.ID}}); err != nil {
					return Report{}, err
				}
				if _, err := store.Wait(task.ID, task.ID); err != nil {
					return Report{}, err
				}
				select {
				case <-childDone:
				case <-ctx.Done():
					return Report{}, ctx.Err()
				}
				// THE LATE ROUND: the worker has parked and its tool still ends.
				if _, err := store.Done(task.ID, task.ID, late, nil, nil); err != nil {
					lateErr.Store(err.Error())
				}
				close(lateTried)
				return Report{Waiting: true, Steps: 2}, nil
			})
		case task.Role == plandb.RoleCheck:
			return parkedRootWorker(func(context.Context, plandb.Task) (Report, error) {
				return Report{Result: "holds: reviewed", Steps: 1}, nil
			})
		default:
			return parkedRootWorker(func(ctx context.Context, task plandb.Task) (Report, error) {
				if _, err := store.Done(task.ID, task.ID, "the child is done", nil, nil); err != nil {
					return Report{}, err
				}
				close(childDone)
				// THE CHILD'S RETURN IS HELD BACK until the late finish has been
				// tried, the way a loaded box held it.
				select {
				case <-lateTried:
				case <-ctx.Done():
					return Report{}, ctx.Err()
				}
				return Report{Result: "the child is done", Steps: 1}, nil
			})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	supervisor := NewSupervisor(store, t.TempDir(), 2, Limits{ReviewRound: true}, factory)
	if outcome := supervisor.Run(ctx); outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeDone)
	}
	if said, _ := lateErr.Load().(string); !strings.Contains(said, "waiting") {
		t.Fatalf("the parked root's late finish answered %q, want it refused because the task is waiting", said)
	}
	checks := 0
	for _, task := range store.Tasks() {
		if task.Role == plandb.RoleCheck {
			checks++
			if task.Status != plandb.StatusDone {
				t.Fatalf("the child's review = %s, want landed before the run answered", task.Status)
			}
		}
	}
	if checks != 1 {
		t.Fatalf("review checks = %d, want exactly one, for the child", checks)
	}
	root := store.Task(rootID)
	if root.Status != plandb.StatusDone || root.Waiting {
		t.Fatalf("root = %s waiting=%v, want done and not waiting", root.Status, root.Waiting)
	}
	if root.Result != woken {
		t.Fatalf("root result = %q, want the woken worker's %q", root.Result, woken)
	}
	if got := rootLaunches.Load(); got != 2 {
		t.Fatalf("root launches = %d, want the first and one wake", got)
	}
}
