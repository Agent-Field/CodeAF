package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// continueRemoteRun adopts the original plan and copy. A terminal plan may
// still need its received change landed after a crash, so no local supervisor
// is asked to resume it and no start can be issued by this road.
func (a *Agent) continueRemoteRun(ctx context.Context, kept TaskNotice) (string, error) {
	if a.config.TaskComputers == nil {
		return "", errors.New(taskComputersUnavailable)
	}
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	a.beltMu.Lock()
	live := a.beltRun
	a.beltMu.Unlock()
	if live != nil {
		return "", fmt.Errorf("%s is already running", taskStopName(live.row, live.title))
	}
	g := a.graph()
	store, err := plandb.Open(g.planPath(), "", "", "", "")
	if err != nil {
		return "", err
	}
	record := readRemoteRecord(store, kept.Remote)
	if record == nil || record.Abandoned || record.StartFailed || record.Settled {
		store.Close()
		return "", errors.New("this task has no remote work to follow")
	}
	if store.RootID() != strconv.FormatUint(kept.ID, 10) {
		store.Close()
		return "", errors.New("this task's plan is no longer the current run")
	}
	engine, err := a.taskExecutorEngine(ctx, record.Executor)
	if err != nil {
		store.Close()
		return "", err
	}
	tree, err := reopenWorkspaceSet(kept.Copy, kept.WorkspaceSet, a.config.Place)
	if err != nil {
		store.Close()
		return "", err
	}
	runCtx, cut := context.WithCancel(a.rootContext())
	run := &beltRun{store: store, root: store.RootID(), row: kept.ID, title: kept.Title, workspace: tree.dir, ground: tree.ground, tree: tree, stand: taskStand{dir: tree.ground, mode: tree.mode}, cut: cut, born: kept.StartedAt, over: make(chan struct{}), remote: record, remoteEngine: engine, remoteResume: true}
	// The plan state's handle is the one every page already reads.
	run.plan = &planState{path: g.planPath(), chat: g.planChat()}
	if root := store.Task(store.RootID()); root != nil && root.Status == plandb.StatusCancelled {
		run.stopped = true
		run.stopReason = root.Error
	}
	a.installBeltRun(g, run)
	if run.stopped {
		a.stopRemoteRun(run)
	}
	kept.State = TaskRunning
	kept.Remote = cloneTaskRemote(record)
	kept.Unreachable = record.Unreachable
	a.publishRunRow(g, kept)
	go a.driveBeltRun(runCtx, engine, run, a.beltRunSpec(run, ""))
	return "following " + taskStopName(kept.ID, kept.Title) + " on " + record.Executor.Name, nil
}

// RunTaskHere fences the old attempt before touching its network or starting
// local work. The copy comparison and result restore share remoteMu, so a late
// result cannot pass between the comparison and the abandonment write.
func (a *Agent) RunTaskHere(ctx context.Context, row uint64) (string, error) {
	epoch, err := a.beltAdmissionEpoch()
	if err != nil {
		return "", err
	}
	if a.config.TaskComputers == nil || chatRunEngine == nil {
		return "", errRunRoadUnavailable
	}
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	if err := a.checkBeltAdmission(epoch); err != nil {
		return "", err
	}
	g := a.graph()
	replacement, err := a.prepareTaskHere(ctx, g, row)
	if err != nil {
		return "", err
	}
	// Cleanup is best effort; the durable abandonment already excludes every late result.
	go func(record TaskRemoteRecord, engine TaskRunEngine) {
		stopCtx, cancel := context.WithTimeout(context.Background(), TaskComputerControlWithin)
		defer cancel()
		_ = engine.Abandon(stopCtx, record)
	}(*replacement.record, replacement.engine)
	if err := replacement.join(ctx); err != nil {
		return "", err
	}
	a.beltMu.Lock()
	busy := a.beltRun != nil
	a.beltMu.Unlock()
	if busy {
		return "", errors.New("another task started before this task could run here")
	}
	return a.startTaskHere(g, replacement)
}

type taskHereReplacement struct {
	kept   TaskNotice
	live   *beltRun
	store  *plandb.Store
	record *TaskRemoteRecord
	engine TaskRunEngine
	tree   taskTree
	brief  string
}

// Preparation checks the untouched copy and durably fences the old attempt under its copy lock.
func (a *Agent) prepareTaskHere(ctx context.Context, g *TaskGraph, row uint64) (*taskHereReplacement, error) {
	kept, found := runRowOf(g, row)
	if !found || kept.Remote == nil || kept.Remote.Abandoned {
		return nil, errors.New("this task is not waiting on another computer")
	}
	a.beltMu.Lock()
	live := a.beltRun
	a.beltMu.Unlock()
	if live != nil && live.row != row {
		return nil, errors.New("another task is already running in this conversation")
	}
	r := &taskHereReplacement{kept: kept, live: live}
	if err := r.openRecord(ctx, a, g); err != nil {
		r.releaseRecord()
		return nil, err
	}
	defer r.releaseRecord()
	if r.store.RootID() != strconv.FormatUint(row, 10) {
		return nil, errors.New("this task's saved plan does not match its number")
	}
	if !r.mayStartHere() {
		return nil, errors.New("the other computer is still answering; stop this task before running it here")
	}
	var err error
	r.tree, err = reopenWorkspaceSet(kept.Copy, kept.WorkspaceSet, a.config.Place)
	if err != nil {
		return nil, err
	}
	if err := r.engine.CheckCopy(ctx, r.tree.dir, r.record.Fingerprint); err != nil {
		return nil, err
	}
	r.record.Abandoned, r.record.AckPending = true, true
	if err := writeRemoteRecord(r.store, *r.record); err != nil {
		return nil, err
	}
	if live != nil {
		live.remote = cloneTaskRemote(r.record)
	}
	r.kept.Remote = cloneTaskRemote(r.record)
	r.kept.Computer = ""
	r.kept.Unreachable = false
	a.publishRunRow(g, r.kept)
	if root := r.store.Task(r.store.RootID()); root != nil {
		r.brief = root.Description
	}
	return r, nil
}

// A live record shares the watcher's fence; an archived record owns a separate store handle.
func (r *taskHereReplacement) openRecord(ctx context.Context, a *Agent, g *TaskGraph) error {
	if r.live != nil {
		r.live.remoteMu.Lock()
		r.record = cloneTaskRemote(r.live.remote)
		r.engine = r.live.remoteEngine
		r.store = r.live.store
		return nil
	}
	var err error
	r.store, err = openRemoteTaskPlan(g, r.kept.ID)
	if err != nil {
		return err
	}
	r.record = readRemoteRecord(r.store, r.kept.Remote)
	if r.record == nil {
		return errors.New("this task is not waiting on another computer")
	}
	r.engine, err = a.taskExecutorEngine(ctx, r.record.Executor)
	return err
}
func (r *taskHereReplacement) releaseRecord() {
	if r.live != nil {
		r.live.remoteMu.Unlock()
	} else if r.store != nil {
		_ = r.store.Close()
	}
}
func (r *taskHereReplacement) mayStartHere() bool {
	return r.record != nil && r.engine != nil && (r.record.StartFailed || (r.record.Unreachable && !r.record.Finished) || r.kept.State == TaskInterrupted)
}

// The start lock remains held while joining, so another task cannot retire this copy first.
func (r *taskHereReplacement) join(ctx context.Context) error {
	if r.live == nil {
		return nil
	}
	if r.live.cut != nil {
		r.live.cut()
	}
	select {
	case <-r.live.over:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// The replacement adopts the ready copy and begins with a fresh local trajectory.
func (a *Agent) startTaskHere(g *TaskGraph, r *taskHereReplacement) (string, error) {
	row := r.kept.ID
	if err := archiveRemoteTrajectory(g.planPath(), row); err != nil {
		return "", err
	}
	plan, store, err := a.openBeltRunStore(g, g.planPath(), strconv.FormatUint(row, 10), r.kept.Title, r.brief, false)
	if err != nil {
		return "", err
	}
	runCtx, cut := context.WithCancel(a.rootContext())
	run := &beltRun{plan: plan, store: store, root: store.RootID(), row: row, title: r.kept.Title, worker: r.record.NamedModel, workspace: r.tree.dir, ground: r.tree.ground, tree: r.tree, stand: taskStand{dir: r.tree.ground, mode: r.tree.mode}, cut: cut, born: a.taskClockNow(), over: make(chan struct{})}
	a.installBeltRun(g, run)
	kept := r.kept
	kept.State, kept.Report, kept.Ending, kept.Stopped = TaskRunning, "", "", false
	kept.StartedAt, kept.EndedAt = run.born, time.Time{}
	kept.Computer, kept.Unreachable, kept.RanElsewhere = "", false, false
	a.publishRunRow(g, kept)
	go a.driveBeltRun(runCtx, chatRunEngine, run, a.beltRunSpec(run, r.brief))
	return "running " + taskStopName(row, kept.Title) + " here", nil
}

func (a *Agent) stopRemoteRun(run *beltRun) {
	// A pending task still owns only local preparation; no remote receipt is owed yet.
	a.beltMu.Lock()
	pending, cut := run.pending, run.cut
	a.beltMu.Unlock()
	if pending && cut != nil {
		cut()
		return
	}
	run.remoteMu.Lock()
	record := cloneTaskRemote(run.remote)
	run.remoteMu.Unlock()
	if record == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), TaskComputerControlWithin)
		defer cancel()
		_ = run.remoteEngine.Stop(ctx, *record)
	}()
}

// A previous refused run can have its plan archived by later work. Read the
// requested task's plan rather than borrowing the newest task's instruction.
func openRemoteTaskPlan(g *TaskGraph, row uint64) (*plandb.Store, error) {
	paths := append([]string{g.planPath()}, planArchivePaths(g.planPath())...)
	for _, path := range paths {
		store, err := plandb.Open(path, "", "", "", "")
		if err != nil {
			continue
		}
		if store.RootID() == strconv.FormatUint(row, 10) {
			return store, nil
		}
		_ = store.Close()
	}
	return nil, errors.New("this task's saved plan is no longer available")
}

// A local replacement must not resume effects that happened only in the abandoned copy.
func archiveRemoteTrajectory(plan string, row uint64) error {
	path := filepath.Join(plandb.TaskDir(filepath.Dir(plan), strconv.FormatUint(row, 10)), "trajectory.jsonl")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	archive, err := os.CreateTemp(filepath.Dir(path), "trajectory-abandoned-*.jsonl")
	if err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := os.Rename(path, archive.Name()); err != nil {
		_ = os.Remove(archive.Name())
		return err
	}
	return nil
}
