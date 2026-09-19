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

// NO RUN ANSWERS DONE WHILE A FINISHED PIECE OF WORK HAS HAD NO REVIEW ROUND,
// WHATEVER THE ORDER OF RETURNS. The two tests here go through the run's own
// loop with scripted workers and force, with channels and no sleeps, the two
// orders a loaded box produced and a quiet one never did. In both the child
// writes its own done to the store and its worker is then held back, so for a
// while the store shows a finished child the run has not yet taken in. A
// LANDING IS A RETURN THE RUN HAS ABSORBED, NOT A STORE ROW: the return is what
// seats the review, so until it is home the child has not landed, the root is
// not complete, and an ending the root wrote meanwhile is not the run's answer.
//
// THE PARKED ROOT. The root parks on its child; a round its worker had already
// started still ends its tool, and that tool is the root's own finish. The store
// refuses it because the root is parked ([plandb.Store.Done]), and what is asked
// here is the rest: the child's review is seated and lands, the root is woken
// once, and the woken worker's word is the run's answer.
func TestAParkedRootsRunReviewsItsChildBeforeItAnswers(t *testing.T) {
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

// THE RUNNING ROOT. The root never parks: it adds a child, and once the child's
// row reads done it writes its own done, which the store admits because every
// child it has is finished. Only then does the child's worker come home. Its
// review has to be seated beneath a root that already reads done, so the store
// reopens that root for the one check. The root finished over a landing it was
// never given, so once the check has landed it is owed the wake any parent is
// owed, and the run ends done with the root's word only after that. It used to
// refuse the check, drop the refusal, and answer done over work nobody had
// reviewed.
func TestARunningRootsFinishDoesNotCloseTheRunOverAnUnreviewedChild(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "running-root", "root", "The run", "finish over one child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID := store.RootID()
	const word = "the root's own word"

	childDone := make(chan struct{})
	rootDone := make(chan struct{})
	var rootLaunches atomic.Int32
	factory := func(task plandb.Task) Worker {
		switch {
		case task.ID == rootID:
			return parkedRootWorker(func(ctx context.Context, task plandb.Task) (Report, error) {
				if rootLaunches.Add(1) > 1 {
					// THE WAKE: the child's landing is handed over, and the root
					// says again what it said.
					return Report{Result: word, Steps: 1}, nil
				}
				if _, err := store.AddMany([]plandb.TaskSpec{{ID: "c1", Title: "the child", ParentID: task.ID}}); err != nil {
					return Report{}, err
				}
				select {
				case <-childDone:
				case <-ctx.Done():
					return Report{}, ctx.Err()
				}
				if _, err := store.Done(task.ID, task.ID, word, nil, nil); err != nil {
					return Report{}, err
				}
				close(rootDone)
				return Report{Result: word, Steps: 2}, nil
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
				// THE CHILD'S RETURN IS HELD BACK until the root has written its
				// own done over it.
				select {
				case <-rootDone:
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
	reviewed := false
	for _, task := range store.Tasks() {
		if task.Role != plandb.RoleCheck {
			continue
		}
		if task.Status != plandb.StatusDone {
			t.Fatalf("review %q = %s, want landed before the run answered", task.ID, task.Status)
		}
		if supervisor.checkOf[task.ID] == "c1" {
			reviewed = true
		}
	}
	if !reviewed {
		t.Fatal("the run answered done and the child was never reviewed")
	}
	if root := store.Task(rootID); root.Status != plandb.StatusDone || root.Result != word {
		t.Fatalf("root = %s %q, want done with its own word %q", root.Status, root.Result, word)
	}
}
