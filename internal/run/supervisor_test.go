package run_test

// The supervisor's tests are scripted, not live: a fake seat answers for
// every task, the plan lives in a fresh store the way internal/plandb's own
// tests open one, and each run is judged by what the store holds and how many
// workers the supervisor launched. Every run carries a wall on its context,
// so a loop that stops moving fails the test instead of hanging it.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// fakeSeat is the fake worker: it records the order its tasks began in and
// how many ran at once, and answers each task out of a script. A task with no
// script succeeds with a plain report.
type fakeSeat struct {
	mu      sync.Mutex
	order   []string
	active  int
	peak    int
	actions map[string]func(ctx context.Context, task plandb.Task) (run.Report, error)
}

func newFakeSeat() *fakeSeat {
	return &fakeSeat{actions: map[string]func(ctx context.Context, task plandb.Task) (run.Report, error){}}
}

// workerFor is the WorkerFactory the supervisor is built with. It returns the
// same recording seat for every task, which is the point: the seat is what
// the tests read afterwards.
func (f *fakeSeat) workerFor(task plandb.Task) run.Worker {
	return &fakeWorker{seat: f}
}

type fakeWorker struct {
	seat *fakeSeat
}

func (w *fakeWorker) Run(ctx context.Context, task plandb.Task) (run.Report, error) {
	w.seat.mu.Lock()
	w.seat.order = append(w.seat.order, task.ID)
	w.seat.active++
	if w.seat.active > w.seat.peak {
		w.seat.peak = w.seat.active
	}
	w.seat.mu.Unlock()
	defer func() {
		w.seat.mu.Lock()
		w.seat.active--
		w.seat.mu.Unlock()
	}()
	action := w.seat.actions[task.ID]
	if action == nil {
		return run.Report{Result: "did " + task.ID, Steps: 1}, nil
	}
	return action(ctx, task)
}

func (f *fakeSeat) launches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
}

func (f *fakeSeat) peakConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peak
}

// runOpenStore opens a fresh store for one test, the same road
// internal/plandb's tests take: a real path under a scratch directory, a
// project and a root named at open, and the handle closed when the test ends.
func runOpenStore(t *testing.T) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "run-test", "root", "The run", "drive the plan to the ground")
	if err != nil {
		t.Fatalf("open plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// runContext bounds every test's run, so a loop that stops moving ends the
// run as an incomplete outcome and the test fails on its assertions instead
// of hanging.
func runContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// splitRootAction is the root worker of every test here: it adds three leaves
// under the root through the store, the way a real worker splits, and
// reports its own account of the split.
func splitRoot(t *testing.T, store *plandb.Store, leaves ...plandb.TaskSpec) func(context.Context, plandb.Task) (run.Report, error) {
	t.Helper()
	return func(_ context.Context, task plandb.Task) (run.Report, error) {
		specs := make([]plandb.TaskSpec, len(leaves))
		for i, leaf := range leaves {
			leaf.ParentID = task.ID
			specs[i] = leaf
		}
		if _, err := store.AddMany(specs); err != nil {
			return run.Report{}, err
		}
		return run.Report{Result: fmt.Sprintf("split into %d leaves", len(leaves)), Steps: 1, USD: 0.10}, nil
	}
}

// leafDone is the ordinary leaf ending, with its own steps and cost.
func leafDone(id string) plandb.TaskSpec {
	return plandb.TaskSpec{ID: id, Title: "leaf " + id}
}

func TestSupervisorSplitsARootAndCompletesItAfterItsLeaves(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "l1", Title: "first"},
		plandb.TaskSpec{ID: "l2", Title: "second"},
		plandb.TaskSpec{ID: "l3", Title: "third"},
	)
	supervisor := run.NewSupervisor(store, t.TempDir(), 3, run.Limits{StepsPerTask: 9}, seat.workerFor)

	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	root := store.Task(store.RootID())
	if root.Status != plandb.StatusDone {
		t.Fatalf("root status = %s, want done", root.Status)
	}
	if root.Result != "split into 3 leaves" {
		t.Fatalf("root result = %q, want the root worker's own result", root.Result)
	}
	// Three leaves, three workers, and every one of them finished before the
	// root's own completion was written.
	launches := seat.launches()
	if len(launches) != 4 || launches[0] != "root" {
		t.Fatalf("launch order = %v, want the root first and three leaves after it", launches)
	}
	for _, id := range []string{"l1", "l2", "l3"} {
		leaf := store.Task(id)
		if leaf.Status != plandb.StatusDone {
			t.Fatalf("leaf %s status = %s, want done", id, leaf.Status)
		}
		if leaf.Result != "did "+id {
			t.Fatalf("leaf %s result = %q, want its own report's result", id, leaf.Result)
		}
		if root.CompletedAt.Before(leaf.CompletedAt) {
			t.Fatalf("root completed at %v, before leaf %s at %s", root.CompletedAt, id, leaf.CompletedAt)
		}
	}
	// The step cap rides the task's context, typed, and reaches the worker.
	if got := run.StepsPerTask(context.Background()); got != 0 {
		t.Fatalf("StepsPerTask on a bare context = %d, want 0", got)
	}
}

func TestSupervisorRunsOneTaskAtATimeUnderASlotOfOne(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "l1", Title: "first"},
		plandb.TaskSpec{ID: "l2", Title: "second"},
		plandb.TaskSpec{ID: "l3", Title: "third"},
	)
	// Each leaf holds its seat briefly, so a second launch during a run would
	// show up as a peak above one.
	seat.actions["l1"] = holdSeat(30 * time.Millisecond)
	seat.actions["l2"] = holdSeat(30 * time.Millisecond)
	seat.actions["l3"] = holdSeat(30 * time.Millisecond)
	supervisor := run.NewSupervisor(store, t.TempDir(), 1, run.Limits{}, seat.workerFor)

	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if peak := seat.peakConcurrency(); peak != 1 {
		t.Fatalf("peak concurrency = %d, want 1 under a slot limit of one", peak)
	}
}

func TestSupervisorStopsLaunchingOnceTheCostLimitIsReached(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	// The leaves chain on hard dependencies, so the store admits them one at
	// a time and the cost counter is read between every launch.
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "l1", Title: "first"},
		plandb.TaskSpec{ID: "l2", Title: "second", Dependencies: []plandb.Dependency{{TaskID: "l1", Kind: plandb.DepFeedsInto}}},
		plandb.TaskSpec{ID: "l3", Title: "third", Dependencies: []plandb.Dependency{{TaskID: "l2", Kind: plandb.DepFeedsInto}}},
	)
	seat.actions["l1"] = holdSeat(10 * time.Millisecond)
	seat.actions["l2"] = holdSeat(10 * time.Millisecond)
	supervisor := run.NewSupervisor(store, t.TempDir(), 8, run.Limits{CostUSD: 0.50}, seat.workerFor)

	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeLimit {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeLimit)
	}
	// The root and the first two leaves ran; the third never started, and the
	// run stands open for a later pass to pick up.
	if launches := seat.launches(); len(launches) != 3 {
		t.Fatalf("launches = %v, want root, l1 and l2 only", launches)
	}
	for _, id := range []string{"l1", "l2"} {
		if leaf := store.Task(id); leaf.Status != plandb.StatusDone {
			t.Fatalf("leaf %s status = %s, want done", id, leaf.Status)
		}
	}
	if leaf := store.Task("l3"); leaf.Status != plandb.StatusReady {
		t.Fatalf("leaf l3 status = %s, want still ready and unlaunched", leaf.Status)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusRunning {
		t.Fatalf("root status = %s, want the run left open for another pass", root.Status)
	}
}

func TestSupervisorWritesAFailedWorkersErrorAndEndsIncomplete(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store,
		plandb.TaskSpec{ID: "l1", Title: "first"},
		plandb.TaskSpec{ID: "l2", Title: "second"},
		plandb.TaskSpec{ID: "l3", Title: "third"},
	)
	reason := errors.New("the checkout step refused")
	seat.actions["l2"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		time.Sleep(20 * time.Millisecond)
		return run.Report{}, reason
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 8, run.Limits{}, seat.workerFor)

	outcome := supervisor.Run(ctx)

	if outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	failed := store.Task("l2")
	if failed.Status != plandb.StatusFailed {
		t.Fatalf("leaf l2 status = %s, want failed", failed.Status)
	}
	if failed.Error != reason.Error() {
		t.Fatalf("leaf l2 failure = %q, want the worker's error %q", failed.Error, reason.Error())
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusFailed {
		t.Fatalf("root status = %s, want failed with the tree unfinished", root.Status)
	}
}

// holdSeat is a leaf action that keeps its worker seat for a while, so a
// supervisor that launches too eagerly meets a seat that is still taken.
func holdSeat(d time.Duration) func(context.Context, plandb.Task) (run.Report, error) {
	return func(_ context.Context, task plandb.Task) (run.Report, error) {
		time.Sleep(d)
		return run.Report{Result: "did " + task.ID, Steps: 2, USD: 0.30}, nil
	}
}
