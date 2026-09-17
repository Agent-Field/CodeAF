package run_test

// The supervisor's tests are scripted, not live: a fake seat answers for
// every task, the plan lives in a fresh store the way internal/plandb's own
// tests open one, and each run is judged by what the store holds and how many
// workers the supervisor launched. Every run carries a wall on its context,
// so a loop that stops moving fails the test instead of hanging it.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
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

// launched answers whether the seat ever began the named task, for a test
// that must react to a worker going out.
func (f *fakeSeat) launched(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, began := range f.order {
		if began == id {
			return true
		}
	}
	return false
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

// storePoke opens the store's database directly, for the one ending no
// exported verb writes: the root's own cancellation, which the store keeps
// out of every writer's hands and which a writer outside this process can
// still put in the file. The handle is idle between runs, so one write lands
// without contention.
func storePoke(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open the store's database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// adoptForeignWrite makes the store's handle look at the database again. A
// handle keeps what its own transactions last read, so a write another
// connection made is invisible to it until its next transaction — and one
// that changes nothing still re-reads the plan whole.
func adoptForeignWrite(t *testing.T, store *plandb.Store) {
	t.Helper()
	if _, err := store.Archive(time.Hour); err != nil {
		t.Fatalf("re-read the plan after a foreign write: %v", err)
	}
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

func TestSupervisorEndsAWorkerWhoseTaskTheStoreCancelled(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store, leafDone("l1"))
	// The leaf holds its seat until its context ends and reports the ending
	// it saw, so the test can tell a cancellation of its own from the run's
	// wall — the one ends the worker's context with cancelled, the other with
	// a deadline. It then comes home WELL, with a completion and no error: the
	// late return is the one the cancellation must refuse, and the row below is
	// what a refusal looks like. The run's counters are the half this test
	// cannot see — TestStartCountsNothingFromAWorkerWhoseTaskWasCancelled is
	// where a late return counted anyway would show.
	ended := make(chan error, 1)
	seat.actions["l1"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		<-ctx.Done()
		ended <- ctx.Err()
		return run.Report{Result: "did " + task.ID, Steps: 1, USD: 0.20}, nil
	}
	reason := "stopped by hand"
	cancelWhenLaunched(store, seat, "l1", reason)
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)

	started := time.Now()
	outcome := supervisor.Run(ctx)

	// A working cascade ends the worker within a pass of the store's cancel;
	// a broken one holds the run to its wall.
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("run took %s, want the worker ended within a pass of the cancel", elapsed)
	}
	if outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	if got := <-ended; !errors.Is(got, context.Canceled) {
		t.Fatalf("worker ended with %v, want context.Canceled", got)
	}
	// The store's cancel is the ending that stands: the claim went with it,
	// the late return wrote nothing over it, and nothing relaunched the task.
	leaf := store.Task("l1")
	if leaf.Status != plandb.StatusCancelled {
		t.Fatalf("leaf l1 status = %s, want cancelled", leaf.Status)
	}
	if leaf.ClaimedBy != "" {
		t.Fatalf("leaf l1 claim = %q, want released by the cancel write", leaf.ClaimedBy)
	}
	if leaf.Result != "" {
		t.Fatalf("leaf l1 result = %q, want nothing written over the cancel", leaf.Result)
	}
	if leaf.Error != reason {
		t.Fatalf("leaf l1 reason = %q, want the cancel's own %q", leaf.Error, reason)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusFailed {
		t.Fatalf("root status = %s, want failed with a cancelled leaf in the tree", root.Status)
	}
}

func TestRunEndsAWorkerUnderACancelledAncestorAndTakesItsLateReturn(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store, leafDone("l1"))
	// The leaf holds its seat until its own context ends, so the run has a
	// worker in flight when the cancellation lands, and comes home well
	// afterwards: its return is a late one with a completion on it.
	ended := make(chan error, 1)
	seat.actions["l1"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		<-ctx.Done()
		ended <- ctx.Err()
		return run.Report{Result: "late " + task.ID, Steps: 4, USD: 0.30}, nil
	}
	// The run's own ending is a write no exported verb makes — the root is the
	// run itself, and the store keeps it out of every writer's hands — so it
	// goes into the database the way a writer outside this process would, and
	// onto the root's row ALONE. The leaf stays open, which is what puts the
	// containment walk to work: the loop has to see the cancellation through
	// the leaf's parent rather than on the leaf's own row. The handle is opened
	// and warmed before the run so the goroutines it costs are in the count
	// the test compares against.
	poke := storePoke(t, store.Path())
	if _, err := poke.Exec(`SELECT 1`); err != nil {
		t.Fatalf("warm the store's database: %v", err)
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)
	// The count is taken before the poller goes out, so the goroutine it costs
	// is not in the baseline a blocked worker could hide behind.
	before := runtime.NumGoroutine()
	go func() {
		for i := 0; i < 5000; i++ {
			if seat.launched("l1") {
				_, _ = poke.Exec(
					`UPDATE tasks SET status = 'cancelled', claimed_by = '' WHERE id = ?`, store.RootID())
				adoptForeignWrite(t, store)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	started := time.Now()
	outcome := supervisor.Run(ctx)

	// A working sweep ends the worker within a pass of the cancellation; a
	// broken one holds the run to its wall.
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("run took %s, want the worker ended within a pass of the cancel", elapsed)
	}
	if outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	// The run's own context is still live, so the only thing that could have
	// ended this worker's context is the pass that read its cancelled ancestor.
	if got := <-ended; !errors.Is(got, context.Canceled) {
		t.Fatalf("worker ended with %v, want context.Canceled", got)
	}
	// The leaf's row was never cancelled and nothing wrote the late completion
	// onto it: the run was over before the return had a reader.
	if leaf := store.Task("l1"); leaf.Status == plandb.StatusCancelled || leaf.Result != "" {
		t.Fatalf("leaf l1 = %s with result %q, want open still and unwritten", leaf.Status, leaf.Result)
	}
	// The deposit is the half no reader sees: a worker that outlives its run
	// leaves the return on a channel wide enough to take it and ends, rather
	// than blocking on a reader that is gone. The count coming back to the
	// baseline is the proof it did, and one settling sample is enough — a run
	// of the runtime's own goroutines is not a worker left behind.
	settled := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if runtime.NumGoroutine() <= before {
			settled = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !settled {
		t.Fatalf("goroutines = %d after the run, want back to the %d it started with", runtime.NumGoroutine(), before)
	}
}

func TestRunAnswersCannotRunWhenTheRootWasCancelledBeforeAnythingRan(t *testing.T) {
	store := runOpenStore(t)
	// The root's ending is a write the store's own verbs refuse — the root is
	// the run itself — so the test puts it in the database the way a writer
	// outside this process would, and makes the handle adopt it.
	if _, err := storePoke(t, store.Path()).Exec(
		`UPDATE tasks SET status = 'cancelled', claimed_by = '' WHERE id = ?`, store.RootID()); err != nil {
		t.Fatalf("cancel the run under the store: %v", err)
	}
	adoptForeignWrite(t, store)
	seat := newFakeSeat()
	supervisor := run.NewSupervisor(store, t.TempDir(), 1, run.Limits{}, seat.workerFor)

	outcome := supervisor.Run(runContext(t))

	if outcome != run.OutcomeCannotRun {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeCannotRun)
	}
	if launches := seat.launches(); len(launches) != 0 {
		t.Fatalf("launches = %v, want none — nothing of the run started", launches)
	}
}

func TestRunAnswersIncompleteWhenTheRootWasCancelledAfterItRan(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = func(ctx context.Context, _ plandb.Task) (run.Report, error) {
		<-ctx.Done()
		return run.Report{}, ctx.Err()
	}
	supervisor := run.NewSupervisor(store, t.TempDir(), 1, run.Limits{}, seat.workerFor)
	rootID := store.RootID()
	go func() {
		for i := 0; i < 5000; i++ {
			if seat.launched(rootID) {
				_, _ = storePoke(t, store.Path()).Exec(
					`UPDATE tasks SET status = 'cancelled', claimed_by = '' WHERE id = ?`, rootID)
				adoptForeignWrite(t, store)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	started := time.Now()
	outcome := supervisor.Run(ctx)

	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("run took %s, want the cancellation seen within a pass", elapsed)
	}
	if outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	if root := store.Task(rootID); root.Status != plandb.StatusCancelled {
		t.Fatalf("root status = %s, want cancelled", root.Status)
	}
}

// startOpenStore opens the store a run's words are still owed to: the root
// task exists, its description bare, because the title is the caller's to
// put on at open and the brief is Start's to put on after.
func startOpenStore(t *testing.T, title string) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "run-test", "root", title, "")
	if err != nil {
		t.Fatalf("open the plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStartRunsABriefAndAnswersTheRunSummary(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		return run.Report{Result: "the run's answer", Steps: 3, USD: 0.42}, nil
	}

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "the whole brief the worker reads",
		Slots:     1,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if summary.Outcome != run.OutcomeDone {
		t.Fatalf("summary outcome = %q, want %q", summary.Outcome, run.OutcomeDone)
	}
	if summary.Result != "the run's answer" {
		t.Fatalf("summary result = %q, want the root worker's own report", summary.Result)
	}
	if summary.Nodes != 1 {
		t.Fatalf("summary nodes = %d, want the one worker the brief took", summary.Nodes)
	}
	if summary.Steps != 3 {
		t.Fatalf("summary steps = %d, want the worker's reported steps", summary.Steps)
	}
	if summary.USD != 0.42 {
		t.Fatalf("summary usd = %v, want what the worker reported", summary.USD)
	}
	if summary.Seconds <= 0 {
		t.Fatalf("summary seconds = %v, want the run's wall", summary.Seconds)
	}
	root := store.Task(store.RootID())
	if root.Status != plandb.StatusDone || root.Result != "the run's answer" {
		t.Fatalf("root = %s with result %q, want done with the worker's report", root.Status, root.Result)
	}
	// The title rode the store's open, and the brief went on the root task
	// through Start: the assignment the worker reads is the store's own row.
	if root.Title != "the run's own title" {
		t.Fatalf("root title = %q, want the run's own", root.Title)
	}
	if brief := strings.TrimSpace(root.Description); brief != "the whole brief the worker reads" {
		t.Fatalf("root description = %q, want the brief Start put there", root.Description)
	}
}

func TestStartRefusesADoorBuiltWithoutItsStoreOrItsFactory(t *testing.T) {
	store := startOpenStore(t, "")
	ctx := runContext(t)

	outcome, summary := run.Start(ctx, run.Spec{})
	if outcome != run.OutcomeCannotRun || summary.Outcome != run.OutcomeCannotRun {
		t.Fatalf("outcome = %q, want %q for a door with no store", outcome, run.OutcomeCannotRun)
	}

	// A door with a store but no factory refuses before anything launches and
	// leaves the store as it stood — the run's words unwritten, nothing spent.
	outcome, summary = run.Start(ctx, run.Spec{Store: store, Brief: "a brief nothing will run"})
	if outcome != run.OutcomeCannotRun || summary.Outcome != run.OutcomeCannotRun {
		t.Fatalf("outcome = %q, want %q for a door with no factory", outcome, run.OutcomeCannotRun)
	}
	if summary.Nodes != 0 || summary.USD != 0 || summary.Seconds != 0 {
		t.Fatalf("summary = %+v, want a run that did nothing", summary)
	}
	if root := store.Task(store.RootID()); root.Description != "" {
		t.Fatalf("root description = %q, want a refused door to leave the store as it stood", root.Description)
	}
}

func TestStartCountsNothingFromAWorkerWhoseTaskWasCancelled(t *testing.T) {
	store := startOpenStore(t, "the run's own title")
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = splitRoot(t, store, leafDone("l1"))
	// The leaf is cancelled mid-flight and comes home well afterwards, so its
	// report is a late one carrying a completion, steps and spend. The store
	// would refuse the write on its own law; what only the drop in absorb does
	// is keep the run's counters out of it, and the counters are what Start
	// answers with.
	seat.actions["l1"] = func(ctx context.Context, task plandb.Task) (run.Report, error) {
		<-ctx.Done()
		return run.Report{Result: "late " + task.ID, Steps: 4, USD: 0.30}, nil
	}
	cancelWhenLaunched(store, seat, "l1", "stopped by hand")

	outcome, summary := run.Start(ctx, run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "the run's own title",
		Brief:     "a brief whose leaf is cancelled mid-run",
		Slots:     2,
		Factory:   seat.workerFor,
	})

	if outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	// The root worker's own figures are the whole account: two workers went
	// out, and the cancelled one's 0.30 and four steps counted for nothing.
	if summary.Nodes != 2 {
		t.Fatalf("summary nodes = %d, want the root and the cancelled leaf", summary.Nodes)
	}
	if summary.USD != 0.10 {
		t.Fatalf("summary usd = %v, want the root's 0.1 alone", summary.USD)
	}
	if summary.Steps != 1 {
		t.Fatalf("summary steps = %d, want the root's 1 alone", summary.Steps)
	}
	if leaf := store.Task("l1"); leaf.Status != plandb.StatusCancelled || leaf.Result != "" {
		t.Fatalf("leaf l1 = %s with result %q, want cancelled with the late completion unwritten", leaf.Status, leaf.Result)
	}
}

func TestSupervisorHoldsAPausedLeafBackUntilItIsResumed(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	// The leaves are the test's own adds, so the pause is in the store before
	// the run's first pass reads it.
	if _, err := store.AddMany([]plandb.TaskSpec{leafDone("l1"), leafDone("l2")}); err != nil {
		t.Fatalf("add the leaves: %v", err)
	}
	if _, err := store.Pause("l1"); err != nil {
		t.Fatalf("pause the leaf: %v", err)
	}
	// THE HOLD IS THE STORE'S OWN LAW, not the loop's: readiness already
	// leaves a paused subtree — and a task under a paused one — off the ready
	// set, so the supervisor needs no code of its own and the proof is that
	// the leaf never launches.
	runnable := store.ReadySet().Runnable
	if len(runnable) != 1 || runnable[0].ID != "l2" {
		t.Fatalf("ready set holds %v, want l2 alone with l1 paused", runnable)
	}
	seat.actions["root"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		return run.Report{Result: "split", Steps: 1}, nil
	}
	// The first run ends on a short wall — long enough for several passes to
	// skip the paused leaf — and leaves the run open, the way a held subtree
	// leaves it.
	walled, stop := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer stop()
	first := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)
	if outcome := first.Run(walled); outcome != run.OutcomeIncomplete {
		t.Fatalf("first outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	if seat.launched("l1") {
		t.Fatalf("launches = %v, want the paused leaf never launched", seat.launches())
	}
	if leaf := store.Task("l1"); leaf.Status != plandb.StatusReady || !leaf.Paused {
		t.Fatalf("leaf l1 = %s paused=%v, want still ready and held", leaf.Status, leaf.Paused)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusRunning {
		t.Fatalf("root status = %s, want the run left open for another pass", root.Status)
	}
	// Resume returns the leaf to the frontier, and the next run's first pass
	// takes it.
	if _, err := store.Resume("l1"); err != nil {
		t.Fatalf("resume the leaf: %v", err)
	}
	second := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)
	if outcome := second.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("second outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if leaf := store.Task("l1"); leaf.Status != plandb.StatusDone {
		t.Fatalf("leaf l1 status = %s, want done once resumed", leaf.Status)
	}
	if !seat.launched("l1") {
		t.Fatalf("launches = %v, want the resumed leaf launched", seat.launches())
	}
}

func TestSupervisorHoldsAChildOfAPausedTaskBackUntilTheResume(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	// A parent with a child under it: the add makes the parent composite, so
	// the child is the only row the frontier could offer, and the hold on its
	// parent is the only thing keeping it off.
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "p1", Title: "the parent"},
		{ID: "c1", Title: "the child of p1", ParentID: "p1"},
	}); err != nil {
		t.Fatalf("add the subtree: %v", err)
	}
	if _, err := store.Pause("p1"); err != nil {
		t.Fatalf("pause the parent: %v", err)
	}
	// THE HOLD IS THE STORE'S OWN LAW AND IT IS INHERITED: readiness reads the
	// pause flag down the containment chain, so a child of a paused task is off
	// the frontier with it and the supervisor needs no code of its own. The
	// proof is that the child never launches and that the flag never moved onto
	// its row.
	if runnable := store.ReadySet().Runnable; len(runnable) != 0 {
		t.Fatalf("ready set holds %v, want nothing while the parent is paused", runnable)
	}
	seat.actions["root"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		return run.Report{Result: "split", Steps: 1}, nil
	}
	walled, stop := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer stop()
	first := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)
	if outcome := first.Run(walled); outcome != run.OutcomeIncomplete {
		t.Fatalf("first outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	if seat.launched("c1") || seat.launched("p1") {
		t.Fatalf("launches = %v, want the paused subtree never launched", seat.launches())
	}
	if child := store.Task("c1"); child.Status != plandb.StatusReady || child.Paused {
		t.Fatalf("child c1 = %s paused=%v, want still ready with the hold on its parent alone", child.Status, child.Paused)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusRunning {
		t.Fatalf("root status = %s, want the run left open for another pass", root.Status)
	}
	// Resuming the parent returns the child to the frontier, and the next run's
	// first pass takes it.
	if _, err := store.Resume("p1"); err != nil {
		t.Fatalf("resume the parent: %v", err)
	}
	second := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)
	if outcome := second.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("second outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if !seat.launched("c1") {
		t.Fatalf("launches = %v, want the child of the resumed parent launched", seat.launches())
	}
	if child := store.Task("c1"); child.Status != plandb.StatusDone {
		t.Fatalf("child c1 status = %s, want done once its parent was resumed", child.Status)
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

// cancelWhenLaunched ends a task in the store as soon as the seat shows its
// worker out, the way a person's cancellation lands while the worker runs.
// It gives up after a full wall's worth of tries so a broken loop cannot
// spin the test process forever.
func cancelWhenLaunched(store *plandb.Store, seat *fakeSeat, id, reason string) {
	go func() {
		for i := 0; i < 5000; i++ {
			if seat.launched(id) {
				_, _ = store.Cancel(id, reason)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
}

// runReopen opens a second handle on an existing plan, the way two processes
// share one store: the path is the store's own, and an open naming no project
// or root adopts whatever the file already holds.
func runReopen(t *testing.T, path string) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("reopen plan store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// leafRoot is the root action of the ownership tests: the run's own worker
// adds nothing, so the leaves are the only work the frontier offers.
func leafRoot(_ context.Context, _ plandb.Task) (run.Report, error) {
	return run.Report{Result: "no split", Steps: 1}, nil
}

// DISPATCH IS PER PROCESS, AND TWO PROCESSES NEVER RUN ONE LEAF TWICE. Two
// supervisors over one store — two handles on one path, each a stand-in for a
// process — race the same ready set. The claim is the store's single writer,
// so no leaf is handed to both: every leaf in the combined launch record ran
// exactly once and ended done. The root is the run itself and each process
// dispatches it, so the count that matters is the leaves'.
func TestSupervisorTwoProcessesNeverRunOneLeafTwice(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		leafDone("l1"), leafDone("l2"), leafDone("l3"), leafDone("l4"),
	}); err != nil {
		t.Fatalf("seed the leaves: %v", err)
	}
	second := runReopen(t, store.Path())
	seat := newFakeSeat()
	seat.actions["root"] = leafRoot
	first := run.NewSupervisor(store, t.TempDir(), 4, run.Limits{}, seat.workerFor)
	other := run.NewSupervisor(second, t.TempDir(), 4, run.Limits{}, seat.workerFor)

	var racers sync.WaitGroup
	for _, supervisor := range []*run.Supervisor{first, other} {
		racers.Add(1)
		go func(s *run.Supervisor) {
			defer racers.Done()
			s.Run(ctx)
		}(supervisor)
	}
	racers.Wait()

	counts := map[string]int{}
	for _, id := range seat.launches() {
		counts[id]++
	}
	for _, id := range []string{"l1", "l2", "l3", "l4"} {
		if counts[id] != 1 {
			t.Fatalf("leaf %s ran %d times, want once: %v", id, counts[id], seat.launches())
		}
		if task := store.Task(id); task == nil || task.Status != plandb.StatusDone {
			t.Fatalf("leaf %s = %#v, want done", id, task)
		}
	}
}

// A CLAIM ITS OWNER STOPPED TOUCHING IS TAKEN OVER. A leaf claimed by a
// process that then died — held, running, its seen-at stamp an hour old — goes
// stale after the window, and the next supervisor releases it and runs it to
// done. The claim is made by hand because a dead process writes nothing more,
// and its stamp is aged in the file because the window is wall-clock. The
// worker that takes the released leaf opens on its trajectory's resume clause,
// the path BashWorker already carries (TestBashWorkerOpensOnARecordedPredecessorWithTheResumeSentence):
// the take-over only ever hands the task to a fresh worker.
func TestSupervisorTakesOverAClaimItsOwnerStoppedTouching(t *testing.T) {
	store := runOpenStore(t)
	ctx := runContext(t)
	if _, err := store.AddMany([]plandb.TaskSpec{leafDone("l1")}); err != nil {
		t.Fatalf("seed the leaf: %v", err)
	}
	if _, err := store.Claim("l1", "l1", "proc-a"); err != nil {
		t.Fatalf("claim the leaf: %v", err)
	}
	db := storePoke(t, store.Path())
	if _, err := db.Exec(`UPDATE tasks SET seen_at = ? WHERE id = ?`,
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), "l1"); err != nil {
		t.Fatalf("age the claim: %v", err)
	}
	seat := newFakeSeat()
	seat.actions["root"] = leafRoot
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)
	supervisor.Owner = "proc-b"

	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeDone)
	}
	if !seat.launched("l1") {
		t.Fatalf("launches = %v, want the stale leaf taken over", seat.launches())
	}
	if task := store.Task("l1"); task.Status != plandb.StatusDone {
		t.Fatalf("leaf = %s, want done by the taking-over run", task.Status)
	}
}

// A LIVE CLAIM HELD BY ANOTHER PROCESS IS NEVER TOUCHED. A leaf claimed by a
// process whose stamp is inside the window stays with that process: a second
// supervisor runs several passes, releases nothing, and never launches the
// leaf.
func TestSupervisorLeavesALiveClaimAlone(t *testing.T) {
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{leafDone("l1")}); err != nil {
		t.Fatalf("seed the leaf: %v", err)
	}
	if _, err := store.Claim("l1", "l1", "proc-a"); err != nil {
		t.Fatalf("claim the leaf: %v", err)
	}
	seat := newFakeSeat()
	seat.actions["root"] = leafRoot
	supervisor := run.NewSupervisor(store, t.TempDir(), 2, run.Limits{}, seat.workerFor)
	supervisor.Owner = "proc-b"
	// A short wall: several passes run, and none of them may touch the fresh
	// claim, so the run leaves the store open on the live worker.
	walled, stop := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer stop()
	if outcome := supervisor.Run(walled); outcome != run.OutcomeIncomplete {
		t.Fatalf("outcome = %q, want %q", outcome, run.OutcomeIncomplete)
	}
	if seat.launched("l1") {
		t.Fatalf("launches = %v, want the live claim left alone", seat.launches())
	}
	if task := store.Task("l1"); task.Status != plandb.StatusRunning || task.ClaimedBy != "l1" {
		t.Fatalf("leaf = %s claimed by %q, want still running for its own worker", task.Status, task.ClaimedBy)
	}
}
