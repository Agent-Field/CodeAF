package session

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// recoverBeltRun reconnects the one live plan to its ordinary driver. The session
// owner has already acquired its file lock before constructing this agent, and
// the start lock serializes recovery with new requests. Settled plans never run.
func (a *Agent) recoverBeltRun() {
	if a.config.InTask || chatRunEngine == nil {
		return
	}
	g := a.tasker()
	if g == nil || g.planPath() == "" {
		return
	}
	defer a.retrySavedRemoteAcks(g)
	if _, err := os.Stat(g.planPath()); err != nil {
		return
	}
	store, err := plandb.Open(g.planPath(), "", "", "", "")
	if err != nil {
		return
	}
	root := store.Task(store.RootID())
	recordedSpend := storeSpent(store)
	id, parseErr := strconv.ParseUint(store.RootID(), 10, 64)
	_ = store.Close()
	if root == nil || parseErr != nil {
		return
	}
	kept, found := runRowOf(g, id)
	if a.recoverRemoteBeltRun(g, store, &kept, found) {
		return
	}
	if !found || kept.State != TaskInterrupted {
		return
	}
	if admittedProgramRecord(g.planPath(), kept) {
		// Delegated runs settle through their recorded program receipt below
		// startup recovery, including old checkpoints without Program filled in.
		return
	}
	if terminalStoreStatus(root.Status) {
		// The last running checkpoint can precede the worker's final receipt.
		// Restore its price label from the ledger without charging it again.
		kept.CostUSD = max(kept.CostUSD, recordedSpend)
		a.reconcileSettledRun(g, kept, root)
		return
	}
	if kept.PendingRun == nil {
		a.resumeAdmittedRun(g, kept)
		return
	}
	a.resumePendingRun(g, kept)
}

// admittedProgramRecord reads the durable worker kind before choosing a
// recovery owner. A queued program has not launched and still needs admission.
func admittedProgramRecord(path string, kept TaskNotice) bool {
	if kept.PendingRun != nil {
		return false
	}
	if kept.Program != "" {
		return true
	}
	_, found := delegate.ReadProgram(plandb.TaskDir(filepath.Dir(path), strconv.FormatUint(kept.ID, 10)))
	return found
}

// reconcileSettledRun publishes the durable ending without executing work again.
// A completed plan alone is not a receipt that its changes were landed.
func (a *Agent) reconcileSettledRun(g *TaskGraph, kept TaskNotice, root *plandb.Task) {
	if planTaskInterrupted(root) {
		return
	}
	kept.PendingRun = nil
	switch {
	case root.Status == plandb.StatusCancelled || planStopReason(root.Error):
		kept.State, kept.Stopped, kept.Report = TaskFailed, true, root.Error
		kept.Ending = TaskEndingStopped
		kept.EndedAt = root.CompletedAt
	case root.Status == plandb.StatusFailed:
		kept.State, kept.Report = TaskFailed, root.Error
		kept.EndedAt = root.CompletedAt
	default:
		kept.Report = "the plan finished before the restart; " + errNoRunCopy.Error()
		kept.RecoveredCompletion = true
		if kept.Copy != nil {
			kept.State, kept.Report = TaskUnverified, withReport(completedRunReview, root.Result)
			kept.EndedAt = root.CompletedAt
			kept.Elapsed = runSpan(kept.StartedAt, root.CompletedAt)
		}
	}
	a.publishRunRow(g, kept)
}

// resumeAdmittedRun adopts an ordinary task's existing copy. Programs have
// their own restart ending and must not be replayed as ordinary workers.
func (a *Agent) resumeAdmittedRun(g *TaskGraph, kept TaskNotice) {
	if kept.Program != "" {
		return
	}
	if kept.Copy == nil {
		a.interruptUnrecoverableRun(g, kept, "the saved task has no working folder; ask for the task again with its folder")
		return
	}
	if _, err := a.ContinueRun(a.rootContext(), kept.ID); err != nil {
		log.Printf("session: could not resume task %d: %v", kept.ID, err)
		a.interruptUnrecoverableRun(g, kept, err.Error())
	}
}

// resumePendingRun reinstalls admission before any folder is taken or worker
// launched. The start lock ensures repeated recovery installs only one driver.
func (a *Agent) resumePendingRun(g *TaskGraph, kept TaskNotice) {
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	a.beltMu.Lock()
	live := a.beltRun != nil
	a.beltMu.Unlock()
	if live {
		return
	}
	pending := kept.PendingRun
	if pending.WorkspaceSet != nil {
		copied := *pending
		request := *pending.WorkspaceSet
		if err := validateWorkspaceSetRequest(&request); err != nil {
			a.interruptUnrecoverableRun(g, kept, err.Error())
			return
		}
		copied.WorkspaceSet = &request
		for _, root := range request.Roots {
			if root.Slot == request.Primary {
				copied.Ground = root.Source
			}
		}
		pending = &copied
		kept.PendingRun = pending
	}
	if reason := invalidPendingRun(pending); reason != "" {
		a.interruptUnrecoverableRun(g, kept, reason)
		return
	}
	crew, err := a.restoreTaskCrew(kept.ID, kept.CrewState)
	if err != nil {
		a.interruptUnrecoverableRun(g, kept, err.Error())
		return
	}
	via := a.pendingRunProgram(kept.Program)
	if kept.Program != "" && via == nil {
		return
	}
	id := kept.ID
	plan, reopened, err := a.openBeltRunStore(g, g.planPath(), strconv.FormatUint(id, 10), kept.Title, pending.Brief, true)
	if err != nil {
		return
	}
	ctx, cut := context.WithCancel(a.rootContext())
	run := &beltRun{plan: plan, store: reopened, root: reopened.RootID(), row: id,
		title: kept.Title, brief: pending.Brief, crew: crew, recoveredCrew: kept.CrewState, ground: canonicalPath(pending.Ground),
		stand: taskStand{dir: pending.Ground, mode: pending.Mode, workspaceSet: pending.WorkspaceSet}, pending: true,
		delegate: via, asked: pending.Asked, cut: cut, born: a.taskClockNow(), over: make(chan struct{}),
		joined:      recoveredJoinedRows(g, id),
		admission:   newRunAdmission(a.config.TaskMaxLoad, a.config.TaskMinFreeMB, a.config.ProfileDir, g.lanes),
		machineHeld: map[string]bool{planStoreID(reopened.RootID()): true},
	}
	engine, err := a.pendingRunEngine(ctx, run, kept)
	if err != nil {
		cut()
		reopened.Close()
		a.interruptUnrecoverableRun(g, kept, err.Error())
		return
	}
	a.installBeltRun(g, run)
	kept.State, kept.Waiting = TaskQueued, waitingMachineBusy
	a.publishRunRow(g, kept)
	go a.driveBeltRun(ctx, engine, run, RunSpec{})
}

// invalidPendingRun refuses incomplete or obsolete records instead of guessing
// a working folder from the process's current directory.
func invalidPendingRun(pending *PendingRunRecord) string {
	if !filepath.IsAbs(pending.Ground) || strings.TrimSpace(pending.Brief) == "" {
		return "the saved task is missing its exact folder or brief; ask for it again"
	}
	info, err := os.Stat(pending.Ground)
	if err != nil || !info.IsDir() {
		return "the saved working folder is unavailable; restore it and request the task again"
	}
	switch pending.Mode {
	case TaskModeWorktree, TaskModeReference, TaskModeMirror, TaskModeInPlace, TaskModeFolder:
		return ""
	default:
		return "the saved task has no valid folder mode; ask for it again with its folder"
	}
}

// pendingRunProgram resolves the same program the queued request named.
func (a *Agent) pendingRunProgram(name string) *delegate.Delegate {
	if name == "" {
		return nil
	}
	for i := range a.config.Delegates {
		if a.config.Delegates[i].Name == name {
			return &a.config.Delegates[i]
		}
	}
	return nil
}

// interruptUnrecoverableRun makes an older or incomplete record visibly inactive.
// Missing ground must never be guessed and unfinished work must never become done.
func (a *Agent) interruptUnrecoverableRun(g *TaskGraph, kept TaskNotice, reason string) {
	store, err := plandb.Open(g.planPath(), "", "", "", "")
	if err != nil {
		return
	}
	defer store.Close()
	kept.EndedAt = interruptedRunEnd(store, TaskIndexEntry{ID: strconv.FormatUint(kept.ID, 10), StartedAt: kept.StartedAt})
	kept.Elapsed = 0
	if err := store.FailRoot(taskWordInterrupted); err != nil {
		return
	}
	_, _ = store.AddNote(store.RootID(), store.RootID(), reason)
	kept.State, kept.Report, kept.PendingRun = TaskInterrupted, reason, nil
	a.publishRunRow(g, kept)
}

// recoveredJoinedRows restores the completion obligations attached to a run.
// A stopped child is already settled and must keep its own ending.
func recoveredJoinedRows(g *TaskGraph, parent uint64) []uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	var ids []uint64
	for _, row := range g.runRowsLocked() {
		if row.Parent == parent && row.State == TaskInterrupted {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

// Remote recovery follows the authoritative sidecar and never starts another worker.
func (a *Agent) recoverRemoteBeltRun(g *TaskGraph, store *plandb.Store, kept *TaskNotice, found bool) bool {
	if !found {
		return false
	}
	remote := readRemoteRecord(store, kept.Remote)
	if remote == nil {
		return false
	}
	kept.Remote = remote
	if remote.Settled && !remote.Abandoned {
		return true
	}
	if remote.Abandoned {
		return false
	}
	if kept.PendingRun != nil && remote.Attempt == "" {
		a.resumeRemotePreparation(g, *kept)
		return true
	}
	if remote.StartFailed {
		kept.State = TaskFailed
		a.publishRunRow(g, *kept)
		return true
	}
	if kept.State.settled() && kept.State != TaskInterrupted && !kept.EndedAt.IsZero() {
		// A persisted final row already completed landing; only its acknowledgement can remain.
		if remote.Finished && remote.Restored {
			remote.Settled = true
			remote.AckPending = true
			_ = writeRemoteRecord(store, *remote)
			kept.Remote = remote
		}
		return true
	}
	_, _ = a.continueRemoteRun(a.rootContext(), *kept)
	return true
}

// Missing capability leaves the remote intent readable and recoverable without local execution.
func (a *Agent) resumeRemotePreparation(g *TaskGraph, kept TaskNotice) {
	if a.config.TaskComputers == nil {
		kept.State, kept.Report = TaskInterrupted, taskComputersUnavailable
		a.publishRunRow(g, kept)
		return
	}
	a.resumePendingRun(g, kept)
}

// Recovery adopts any recorded copy; preparing it again would overwrite retained work.
func (a *Agent) pendingRunEngine(ctx context.Context, run *beltRun, kept TaskNotice) (RunEngine, error) {
	if kept.Remote == nil || kept.Remote.Abandoned {
		return chatRunEngine, nil
	}
	if a.config.TaskComputers == nil {
		return nil, errors.New(taskComputersUnavailable)
	}
	remote, err := a.taskExecutorEngine(ctx, kept.Remote.Executor)
	if err != nil {
		return nil, err
	}
	run.remoteEngine, run.remote = remote, cloneTaskRemote(kept.Remote)
	run.placement.computer = &TaskComputer{Device: kept.Remote.Executor.Device, Name: kept.Remote.Executor.Name, CanTakeTasks: true}
	if kept.Copy != nil || kept.WorkspaceSet != nil {
		tree, err := reopenWorkspaceSet(kept.Copy, kept.WorkspaceSet, a.config.Place)
		if err != nil {
			return nil, err
		}
		run.tree, run.workspace, run.ground = tree, tree.dir, tree.ground
	}
	return remote, nil
}
