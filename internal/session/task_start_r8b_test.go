package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// R8B and contracts 1–2 require the named task's receipt before repository-sized work.
func TestANamedTaskAnswersWhileItsCopyIsStillPreparing(t *testing.T) {
	a, _, entered, release := gatedRemoteStart(t)
	id := answerGatedRemoteStart(t, a, entered, release)
	row, found := runRowOf(a.graph(), id)
	if !found || row.State != TaskQueued || row.PendingRun == nil || row.Remote == nil || row.Remote.Executor.Name != "work computer" || row.Waiting != "" {
		t.Fatalf("preparing task lost its queued placement: %+v", row)
	}
	release()
	waitRemoteRun(t, a)
	row, _ = runRowOf(a.graph(), id)
	if row.State != TaskFailed || !strings.Contains(row.Report, "the fixture copy could not be prepared") {
		t.Fatalf("preparation failure did not reach the task row: %+v", row)
	}
}

// R8B and contract 11 keep stop independent of preparation after the receipt.
func TestANamedTaskCanBeStoppedWhileItsCopyIsPreparing(t *testing.T) {
	a, called, entered, release := gatedRemoteStart(t)
	id := answerGatedRemoteStart(t, a, entered, release)
	if _, err := a.Cancel(CancelTask + ":" + strconv.FormatUint(id, 10)); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	row, _ := runRowOf(a.graph(), id)
	if !row.Stopped || row.State != TaskFailed || called.Load() != 0 {
		t.Fatalf("stopped preparation reached a worker or lost its ending: row=%+v starts=%d", row, called.Load())
	}
}

func gatedRemoteStart(t *testing.T) (*Agent, *atomic.Int32, <-chan struct{}, func()) {
	t.Helper()
	remoteStartHome(t)
	entered, freed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(freed) }) }
	previous := beltRunPrepare
	beltRunPrepare = func(ctx context.Context, _ Place, _, _ string, _ uint64, _ string, _ taskStand) (taskTree, error) {
		close(entered)
		select {
		case <-freed:
			return taskTree{}, errors.New("the fixture copy could not be prepared")
		case <-ctx.Done():
			return taskTree{}, ctx.Err()
		}
	}
	t.Cleanup(func() { beltRunPrepare = previous })
	called := &atomic.Int32{}
	remote := &scriptedTaskEngine{start: func(context.Context, RunSpec) RunSummary {
		called.Add(1)
		return RunSummary{Outcome: "incomplete", KeepCopy: true}
	}}
	a, _ := remoteTaskAgent(t, remote, func(c *Config) {
		c.ProfileDir = filepath.Join(os.Getenv("CODEAF_HOME"), "profile")
		c.TaskMaxLoad, c.TaskMinFreeMB = 0, 0
	})
	t.Cleanup(func() { release(); joinRemoteStartFixture(t, a) })
	return a, called, entered, release
}

func answerGatedRemoteStart(t *testing.T, a *Agent, entered <-chan struct{}, release func()) uint64 {
	t.Helper()
	type answer struct {
		id  uint64
		err error
	}
	reply := make(chan answer, 1)
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		id, _, _, err := a.StartTaskOn(t.Context(), "repair the export", true, "work computer")
		reply <- answer{id, err}
	}()
	t.Cleanup(func() { release(); <-returned })
	select {
	case <-entered:
	case result := <-reply:
		if result.err != nil || result.id == 0 {
			t.Fatalf("named task was not accepted: %+v", result)
		}
		select {
		case <-entered:
		case <-time.After(30 * time.Second):
			t.Fatal("accepted task never reached preparation")
		}
		return result.id
	case <-time.After(30 * time.Second):
		t.Fatal("named task never reached preparation")
	}
	select {
	case result := <-reply:
		if result.err != nil || result.id == 0 {
			t.Fatalf("named task was not accepted: %+v", result)
		}
		return result.id
	case <-time.After(5 * time.Second):
		t.Fatal("the start receipt waited for repository preparation")
		return 0
	}
}

func remoteStartHome(t *testing.T) {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "rn-l1-r8b-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", home)
}

type alreadySelectedTaskComputers struct {
	testTaskComputers
	factories, recoveries atomic.Int32
}

func (c *alreadySelectedTaskComputers) Engine(context.Context, string) (TaskRunEngine, error) {
	c.factories.Add(1)
	return nil, errors.New("the directory was queried again for a selected computer")
}
func (c *alreadySelectedTaskComputers) RecoverEngine(TaskExecutor) TaskRunEngine {
	c.recoveries.Add(1)
	return c.engine
}

// R8B and contract 6 resolve once; each eventual dial still checks permission.
func TestASelectedTaskComputerDoesNotNeedAnotherRosterReadForItsEngine(t *testing.T) {
	remoteStartHome(t)
	remote := &scriptedTaskEngine{start: func(context.Context, RunSpec) RunSummary {
		return RunSummary{Outcome: "incomplete", KeepCopy: true}
	}}
	computers := &alreadySelectedTaskComputers{testTaskComputers: testTaskComputers{engine: remote}}
	a, _ := remoteTaskAgent(t, remote, func(c *Config) {
		c.TaskComputers = computers
		c.ProfileDir = filepath.Join(os.Getenv("CODEAF_HOME"), "profile")
	})
	if _, _, _, err := a.StartTaskOn(t.Context(), "repair the export", true, "work computer"); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	if computers.factories.Load() != 0 || computers.recoveries.Load() != 1 {
		t.Fatalf("selected engine read the roster again: factories=%d selected=%d", computers.factories.Load(), computers.recoveries.Load())
	}
}

// R8B and contract 10 retain a not-yet-dispatched task across closing its chat.
func TestReopeningANamedTaskResumesItsPendingCopyBeforeDispatch(t *testing.T) {
	first, started, entered, release := gatedRemoteStart(t)
	id := answerGatedRemoteStart(t, first, entered, release)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, first)
	store, err := plandb.Open(first.graph().planPath(), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := store.Task(store.RootID())
	_ = store.Close()
	if root == nil || terminalStoreStatus(root.Status) {
		t.Fatalf("closing preparation ended the pending task: %+v", root)
	}
	// The old preparer has joined, so recovery can exercise the real copy road.
	beltRunPrepare = prepareTaskTreeOn
	second, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place, c.SessionFile = first.config.Workspace, first.config.Place, first.config.SessionFile
		c.ProfileDir, c.TaskComputers = first.config.ProfileDir, first.config.TaskComputers
		c.AskConsent = false
	})
	waitRemoteRun(t, second)
	row, found := runRowOf(second.graph(), id)
	if !found || row.PendingRun != nil || row.Remote == nil || row.Remote.Executor.Name != "work computer" || started.Load() != 1 {
		t.Fatalf("reopen did not dispatch the original placement once: row=%+v starts=%d", row, started.Load())
	}
	second.recoverBeltRun()
	if started.Load() != 1 {
		t.Fatal("repeated recovery dispatched another task")
	}
}

// Contracts 10–11 require an unavailable executor to remain remote until an explicit replacement.
func TestReopeningAPendingRemoteTaskDoesNotChooseTheLocalEngine(t *testing.T) {
	first, _, entered, release := gatedRemoteStart(t)
	id := answerGatedRemoteStart(t, first, entered, release)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, first)
	first.SettleWrites()
	// A mistaken local dispatch must fail the assertion rather than reuse the first gate.
	beltRunPrepare = func(context.Context, Place, string, string, uint64, string, taskStand) (taskTree, error) {
		return taskTree{}, errors.New("fixture caught an unexpected local dispatch")
	}
	second := reopenRemoteStart(t, first, nil)
	second.beltMu.Lock()
	dispatched := second.beltRun != nil
	second.beltMu.Unlock()
	row, found := runRowOf(second.graph(), id)
	if dispatched || !found || row.Remote == nil || row.PendingRun == nil || !strings.Contains(row.Report, "paired task computers are unavailable here") {
		t.Fatalf("unavailable remote intent was lost or dispatched locally: live=%v row=%+v", dispatched, row)
	}
}

// Contract 10 preserves the actual owned copy when Close wins after preparation finishes.
func TestReopeningARemoteTaskAdoptsTheCopyPreparedAsTheChatClosed(t *testing.T) {
	remoteStartHome(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	prepared := make(chan taskTree, 1)
	previous := beltRunPrepare
	var preparations atomic.Int32
	beltRunPrepare = func(ctx context.Context, p Place, workspace, chat string, id uint64, title string, stand taskStand) (taskTree, error) {
		preparations.Add(1)
		tree, err := previous(ctx, p, workspace, chat, id, title, stand)
		if err != nil {
			return tree, err
		}
		if err := os.WriteFile(filepath.Join(tree.dir, "kept-before-close"), []byte("prepared copy"), 0600); err != nil {
			return tree, err
		}
		prepared <- tree
		<-ctx.Done()
		return tree, nil
	}
	t.Cleanup(func() { beltRunPrepare = previous })
	var starts, resumes atomic.Int32
	remote := &scriptedTaskEngine{
		start: func(context.Context, RunSpec) RunSummary {
			starts.Add(1)
			return RunSummary{Outcome: "incomplete", KeepCopy: true}
		},
		resume: func(context.Context, RunSpec) RunSummary {
			resumes.Add(1)
			return RunSummary{Outcome: "incomplete", KeepCopy: true}
		},
	}
	first, _ := remoteTaskAgent(t, remote, nil)
	t.Cleanup(func() { joinRemoteStartFixture(t, first) })
	id, _, _, err := first.StartTaskOn(t.Context(), "repair export", true, "work computer")
	if err != nil {
		t.Fatal(err)
	}
	var copy taskTree
	select {
	case copy = <-prepared:
	case <-time.After(30 * time.Second):
		t.Fatal("owned copy never prepared")
	}
	if copy.workspaceSet == nil || copy.uri == "" {
		t.Fatal("fixture did not prepare an owned workspace")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, first)
	first.SettleWrites()
	beltRunPrepare = func(ctx context.Context, p Place, workspace, chat string, id uint64, title string, stand taskStand) (taskTree, error) {
		preparations.Add(1)
		return previous(ctx, p, workspace, chat, id, title, stand)
	}
	second := reopenRemoteStart(t, first, first.config.TaskComputers)
	waitRemoteRun(t, second)
	row, _ := runRowOf(second.graph(), id)
	if row.Copy == nil || row.Copy.URI != copy.uri || preparations.Load() != 1 || starts.Load() != 1 || resumes.Load() != 0 {
		t.Fatalf("reopen recarved or lost its owned copy: row=%+v preparations=%d starts=%d resumes=%d", row, preparations.Load(), starts.Load(), resumes.Load())
	}
	if raw, err := os.ReadFile(filepath.Join(copy.dir, "kept-before-close")); err != nil || string(raw) != "prepared copy" {
		t.Fatalf("prepared bytes lost: %q %v", raw, err)
	}
}

// Contract 3 keeps the ownership handles of a real copy returned with a preparation error.
func TestARemotePreparationFailureKeepsItsRetainedCopyOnTheTaskRow(t *testing.T) {
	remoteStartHome(t)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	previous := beltRunPrepare
	beltRunPrepare = func(ctx context.Context, p Place, workspace, chat string, id uint64, title string, stand taskStand) (taskTree, error) {
		tree, err := previous(ctx, p, workspace, chat, id, title, stand)
		if err != nil {
			return tree, err
		}
		return tree, errors.New("fixture failure after retaining the private copy")
	}
	t.Cleanup(func() { beltRunPrepare = previous })
	remote := &scriptedTaskEngine{start: func(context.Context, RunSpec) RunSummary {
		t.Error("failed preparation dispatched a worker")
		return RunSummary{}
	}}
	a, _ := remoteTaskAgent(t, remote, nil)
	t.Cleanup(func() { joinRemoteStartFixture(t, a) })
	id, _, _, err := a.StartTaskOn(t.Context(), "repair export", true, "work computer")
	if err != nil {
		t.Fatal(err)
	}
	waitRemoteRun(t, a)
	row, _ := runRowOf(a.graph(), id)
	if row.State != TaskFailed || row.Copy == nil || row.Copy.URI == "" || row.WorkspaceSet == nil || !strings.Contains(row.Report, "failure after retaining") {
		t.Fatalf("failed row dropped its retained private copy: %+v", row)
	}
	if _, err := os.Stat(row.Copy.Dir); err != nil {
		t.Fatal(err)
	}
}

func reopenRemoteStart(t *testing.T, first *Agent, computers TaskComputers) *Agent {
	t.Helper()
	second, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place, c.SessionFile = first.config.Workspace, first.config.Place, first.config.SessionFile
		c.ProfileDir, c.TaskComputers = first.config.ProfileDir, computers
		c.AskConsent = false
	})
	t.Cleanup(func() { joinRemoteStartFixture(t, second) })
	return second
}

func joinRemoteStartFixture(t *testing.T, a *Agent) {
	t.Helper()
	a.beltMu.Lock()
	run := a.beltRun
	a.beltMu.Unlock()
	if run != nil && run.cut != nil {
		run.cut()
	}
	_ = a.Close()
	if run != nil {
		select {
		case <-run.over:
		case <-time.After(30 * time.Second):
			t.Error("fixture driver did not join before cleanup")
		}
	}
}
