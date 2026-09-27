package session

import (
	"context"
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
	if _, err := os.Stat(g.planPath()); err != nil {
		return
	}
	store, err := plandb.Open(g.planPath(), "", "", "", "")
	if err != nil {
		return
	}
	root := store.Task(store.RootID())
	id, parseErr := strconv.ParseUint(store.RootID(), 10, 64)
	_ = store.Close()
	if root == nil || parseErr != nil {
		return
	}
	kept, found := runRowOf(g, id)
	if !found || kept.State != TaskInterrupted {
		return
	}
	if terminalStoreStatus(root.Status) {
		if planTaskInterrupted(root) {
			return
		}
		if planStopReason(root.Error) {
			kept.State, kept.Stopped, kept.Report = TaskFailed, true, root.Error
			kept.EndedAt = root.CompletedAt
			kept.PendingRun = nil
			a.publishRunRow(g, kept)
		} else if root.Status == plandb.StatusFailed || root.Status == plandb.StatusCancelled {
			kept.State, kept.Report, kept.PendingRun = TaskFailed, root.Error, nil
			kept.EndedAt = root.CompletedAt
			a.publishRunRow(g, kept)
		} else {
			// The plan finished, but no durable landing receipt says its copy
			// reached the person's branch. Keep that distinction visible.
			kept.Report = "the plan finished before the restart; inspect the saved working copy before landing its changes"
			kept.PendingRun = nil
			a.publishRunRow(g, kept)
		}
		return
	}
	if kept.PendingRun == nil {
		if kept.Program != "" {
			return
		}
		if kept.Copy == nil {
			a.interruptUnrecoverableRun(g, kept, "the saved task has no working folder; ask for the task again with its folder")
			return
		}
		if _, err := a.ContinueRun(context.Background(), id); err != nil {
			log.Printf("session: could not resume task %d: %v", id, err)
			a.interruptUnrecoverableRun(g, kept, err.Error())
		}
		return
	}
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	a.beltMu.Lock()
	live := a.beltRun != nil
	a.beltMu.Unlock()
	if live {
		return
	}
	pending := kept.PendingRun
	if !filepath.IsAbs(pending.Ground) || strings.TrimSpace(pending.Brief) == "" {
		a.interruptUnrecoverableRun(g, kept, "the saved task is missing its exact folder or brief; ask for it again")
		return
	}
	info, err := os.Stat(pending.Ground)
	if err != nil || !info.IsDir() {
		a.interruptUnrecoverableRun(g, kept, "the saved working folder is unavailable; restore it and request the task again")
		return
	}
	switch pending.Mode {
	case TaskModeWorktree, TaskModeReference, TaskModeMirror, TaskModeInPlace, TaskModeFolder:
	default:
		a.interruptUnrecoverableRun(g, kept, "the saved task has no valid folder mode; ask for it again with its folder")
		return
	}
	var via *delegate.Delegate
	if kept.Program != "" {
		for i := range a.config.Delegates {
			if a.config.Delegates[i].Name == kept.Program {
				via = &a.config.Delegates[i]
				break
			}
		}
		if via == nil {
			return
		}
	}
	plan, reopened, err := a.openBeltRunStore(g, g.planPath(), strconv.FormatUint(id, 10), kept.Title, pending.Brief, true)
	if err != nil {
		return
	}
	ctx, cut := context.WithCancel(context.Background())
	run := &beltRun{plan: plan, store: reopened, root: reopened.RootID(), row: id,
		title: kept.Title, brief: pending.Brief, ground: canonicalPath(pending.Ground),
		stand: taskStand{dir: pending.Ground, mode: pending.Mode}, pending: true,
		delegate: via, asked: pending.Asked, cut: cut, born: a.taskClockNow(), over: make(chan struct{}),
		joined:    recoveredJoinedRows(g, id),
		admission: NewRunAdmission(a.config.TaskMaxLoad, a.config.TaskMinFreeMB, g.lanes),
	}
	a.installBeltRun(g, run)
	kept.State, kept.Waiting = TaskQueued, waitingMachineBusy
	a.publishRunRow(g, kept)
	go a.driveBeltRun(ctx, chatRunEngine, run, RunSpec{})
}

// interruptUnrecoverableRun makes an older or incomplete record visibly inactive.
// Missing ground must never be guessed and unfinished work must never become done.
func (a *Agent) interruptUnrecoverableRun(g *TaskGraph, kept TaskNotice, reason string) {
	store, err := plandb.Open(g.planPath(), "", "", "", "")
	if err != nil {
		return
	}
	defer store.Close()
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
