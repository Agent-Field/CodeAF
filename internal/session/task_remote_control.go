package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// continueRemoteRun adopts the original plan and copy. A terminal plan may
// still need its received change landed after a crash, so no local supervisor
// is asked to resume it and no start can be issued by this road.
func (a *Agent) continueRemoteRun(ctx context.Context, kept TaskNotice) (string, error) {
	if a.config.TaskComputers == nil {
		return "", errors.New("paired task computers are unavailable here")
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
	epoch, admissionErr := a.beltAdmissionEpoch()
	if admissionErr != nil {
		return "", admissionErr
	}
	if a.config.TaskComputers == nil || chatRunEngine == nil {
		return "", errRunRoadUnavailable
	}
	a.lockBeltStart()
	if err := a.checkBeltAdmission(epoch); err != nil {
		a.beltStartMu.Unlock()
		return "", err
	}
	g := a.graph()
	kept, found := runRowOf(g, row)
	if !found || kept.Remote == nil || kept.Remote.Abandoned {
		a.beltStartMu.Unlock()
		return "", errors.New("this task is not waiting on another computer")
	}
	a.beltMu.Lock()
	live := a.beltRun
	a.beltMu.Unlock()
	if live != nil && live.row != row {
		a.beltStartMu.Unlock()
		return "", errors.New("another task is already running in this conversation")
	}
	var store *plandb.Store
	var record *TaskRemoteRecord
	var engine TaskRunEngine
	var err error
	if live != nil {
		live.remoteMu.Lock()
		record = cloneTaskRemote(live.remote)
		engine = live.remoteEngine
		store = live.store
	} else {
		store, err = openRemoteTaskPlan(g, row)
		if err == nil {
			record = readRemoteRecord(store, kept.Remote)
			engine, err = a.taskExecutorEngine(ctx, record.Executor)
		}
	}
	unlock := func() {
		if live != nil {
			live.remoteMu.Unlock()
		} else if store != nil {
			_ = store.Close()
		}
		a.beltStartMu.Unlock()
	}
	if err == nil && store.RootID() != strconv.FormatUint(row, 10) {
		err = errors.New("this task's saved plan does not match its number")
	}
	if err != nil {
		unlock()
		return "", err
	}
	if record == nil || engine == nil || (!record.StartFailed && !(record.Unreachable && !record.Finished) && kept.State != TaskInterrupted) {
		unlock()
		return "", errors.New("the other computer is still answering; stop this task before running it here")
	}
	tree, err := reopenWorkspaceSet(kept.Copy, kept.WorkspaceSet, a.config.Place)
	if err != nil {
		unlock()
		return "", err
	}
	if err = engine.CheckCopy(ctx, tree.dir, record.Fingerprint); err != nil {
		unlock()
		return "", err
	}
	record.Abandoned, record.AckPending = true, true
	if err = writeRemoteRecord(store, *record); err != nil {
		unlock()
		return "", err
	}
	if live != nil {
		live.remote = cloneTaskRemote(record)
	}
	kept.Remote = cloneTaskRemote(record)
	kept.Computer = ""
	kept.Unreachable = false
	a.publishRunRow(g, kept)
	brief := ""
	if root := store.Task(store.RootID()); root != nil {
		brief = root.Description
	}
	if live != nil {
		live.remoteMu.Unlock()
	} else if store != nil {
		_ = store.Close()
	}
	// Keep start admission while joining the abandoned watcher. No new task may
	// archive this plan or retire its copy between the fence and its replacement.
	defer a.beltStartMu.Unlock()
	// Network cleanup is best effort and cannot delay the person's local work.
	go func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), TaskComputerControlWithin)
		defer cancel()
		_ = engine.Abandon(stopCtx, *record)
	}()
	if live != nil {
		if live.cut != nil {
			live.cut()
		}
		select {
		case <-live.over:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	a.beltMu.Lock()
	busy := a.beltRun != nil
	a.beltMu.Unlock()
	if busy {
		return "", errors.New("another task started before this task could run here")
	}
	plan, newStore, err := a.openBeltRunStore(g, g.planPath(), strconv.FormatUint(row, 10), kept.Title, brief, false)
	if err != nil {
		return "", err
	}
	runCtx, cut := context.WithCancel(a.rootContext())
	run := &beltRun{plan: plan, store: newStore, root: newStore.RootID(), row: row, title: kept.Title, worker: record.NamedModel, workspace: tree.dir, ground: tree.ground, tree: tree, stand: taskStand{dir: tree.ground, mode: tree.mode}, cut: cut, born: a.taskClockNow(), over: make(chan struct{})}
	a.installBeltRun(g, run)
	kept.State, kept.Report, kept.Ending, kept.Stopped = TaskRunning, "", "", false
	kept.StartedAt, kept.EndedAt = run.born, time.Time{}
	kept.Computer, kept.Unreachable, kept.RanElsewhere = "", false, false
	a.publishRunRow(g, kept)
	go a.driveBeltRun(runCtx, chatRunEngine, run, a.beltRunSpec(run, brief))
	return "running " + taskStopName(row, kept.Title) + " here", nil
}

func (a *Agent) stopRemoteRun(run *beltRun) {
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
