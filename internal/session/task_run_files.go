package session

// WHAT A RUN ON THE WORKER HARNESS TOUCHED, WRITTEN WHERE THE OLDER ENGINE'S
// TASKS ALREADY WRITE IT.
//
// A task on the older engine leaves a row in the project's record
// (task_index.go) naming the files it wrote, and that row is what `<elsewhere>`,
// the `tasks` tool and the sessions rows on home read. A run on the worker
// harness left no row at all: its workers edit through a shell and fill no
// write ledger, so nothing ever wrote the list down (0 of 138 measured on
// 2026-09-24), and every overlap with one was invisible.
//
// THE LIST IS THE WORKING COPY'S OWN ACCOUNT, NOT THE MODEL'S. It is the diff
// between the commit the run's copy was cut from and the copy as the run left
// it: a worker that committed its own work is counted as surely as one whose
// edits the landing committed for it. No model is asked, and nothing about a
// language or a tool is consulted — a path is in the list because git says the
// run changed it.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// runTouchedFiles is every path a run's work changed against the commit its copy
// was cut from, and "" — or the reason the list could not be read.
//
// The copy is read while it is still on disk, which covers what the landing
// committed and anything it left uncommitted. A copy that has already been
// given back is read off its branch instead, from the repository that holds it.
//
// untracked adds the files the workers made and nothing has committed yet. It
// is asked for by the roads that read a run CUT SHORT — a stop, a closed chat, a
// process that went away — because there nothing has committed the work and a
// new file is as much the run's as an edited one. The landing's road does not
// ask: by then the run's work is committed, and what is left untracked is the
// harness's own, which the landing deliberately leaves out.
func runTouchedFiles(tree taskTree, untracked bool) ([]string, string) {
	base, ok := checkBaseFor(tree)
	if !ok {
		return nil, "the commit the run's copy was cut from is not on record"
	}
	dir := strings.TrimSpace(tree.dir)
	args := []string{"diff", "--name-only", "--no-renames", "-z", base.sha}
	onDisk := true
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		branch := strings.TrimSpace(tree.branch)
		if branch == "" {
			return nil, "the run's copy is gone and it named no branch"
		}
		dir, onDisk = base.repository, false
		args = append(args, branch)
	}
	files, unread := gitPathList(dir, args...)
	if unread != "" || !untracked || !onDisk {
		return files, unread
	}
	made, unread := gitPathList(dir, "ls-files", "--others", "--exclude-standard", "-z")
	for _, path := range made {
		// WHAT IS MACHINERY IS ANSWERED IN ONE PLACE ([harnessWrote]), the same
		// place the landing asks, so a list read here never names a file the
		// landing would have left out.
		if !harnessWrote(path) {
			files = mergePaths(files, []string{path})
		}
	}
	return files, unread
}

// gitPathList is one git reading that answers NUL-separated paths, spelled
// with forward slashes, or the reason git would not answer.
func gitPathList(dir string, args ...string) ([]string, string) {
	out, err := git(dir, args...)
	if err != nil {
		reason := strings.TrimSpace(firstLine(out))
		if reason == "" {
			reason = err.Error()
		}
		return nil, "git could not read the run's changes: " + reason
	}
	var files []string
	for _, path := range strings.Split(out, "\x00") {
		if path = strings.TrimSpace(path); path != "" {
			files = append(files, filepath.ToSlash(path))
		}
	}
	return files, ""
}

// ── THE RUN'S ROWS IN THE PROJECT'S RECORD ──────────────────────────────────
//
// A run takes the rows an adaptive run takes (orchestrate.go), for the same
// reason: a `running` row from its first breath, so every other window and home
// can see work is out, and a second row that closes it. The file is append-only
// and every reader takes the last row per (session, id), so the closing row
// supersedes the running one, and a run carried on and finished supersedes the
// `interrupted` row its first life left.
//
// A RUNNING ROW IS A CLAIM ABOUT A PROCESS, and it is believed only while a
// fresh presence file from this window names the run ([Agent.presenceBeltRun]
// is that half). A process that goes away leaves the claim unbacked; the next
// open of the conversation closes it ([Agent.closeInflightTaskIndexRows]).

// beltRunEntry is the part of a run's row every one of its rows shares.
func (a *Agent) beltRunEntry(run *beltRun) TaskIndexEntry {
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	return TaskIndexEntry{
		ID:        strconv.FormatUint(run.row, 10),
		Name:      TaskSlug(run.title),
		Label:     taskLabel(run.title),
		Title:     strings.TrimSpace(run.title),
		Ground:    run.ground,
		Mode:      run.tree.mode,
		Rung:      run.tree.rung,
		StartedAt: a.beltRunStarted(run),
		SessionID: session,
	}
}

// beltRunStarted is when the run's work began: the instant its row was first
// published with, which a run carried on keeps, and the run's own birth for a
// row that never said.
func (a *Agent) beltRunStarted(run *beltRun) time.Time {
	if g := a.graph(); g != nil {
		for _, kept := range g.runRows(run.row) {
			if kept.ID == run.row && !kept.StartedAt.IsZero() {
				return kept.StartedAt
			}
		}
	}
	return run.born
}

// recordBeltRunStart writes the run's `running` row, the moment it starts or is
// carried on.
func (a *Agent) recordBeltRunStart(run *beltRun) {
	if run == nil {
		return
	}
	entry := a.beltRunEntry(run)
	entry.Status = string(TaskRunning)
	a.recordTaskIndexEntry(entry)
}

// recordBeltRunIndex writes the row that closes the run: how it ended, what it
// cost, and the files it touched.
func (a *Agent) recordBeltRunIndex(run *beltRun, notice TaskNotice, cost float64, touched []string, unread string) {
	if run == nil {
		return
	}
	files, count := taskFileCitations(touched)
	entry := a.beltRunEntry(run)
	entry.Status = string(notice.State)
	entry.Ending = notice.Ending
	entry.Outcome = taskOutcome(notice.Report)
	entry.FilesChanged, entry.Files = count, files
	entry.FilesUnread = strings.TrimSpace(unread)
	entry.Cost = cost
	entry.EndedAt = notice.EndedAt
	entry.Branch = keptBranchOf(notice.Branch, notice.Merge)
	if notice.Stopped {
		entry.Ending = TaskEndingStopped
	}
	if !entry.StartedAt.IsZero() && notice.EndedAt.After(entry.StartedAt) {
		entry.DurationMS = notice.EndedAt.Sub(entry.StartedAt).Milliseconds()
	}
	a.recordTaskIndexEntry(entry)
}

// recordBeltRunInterrupted writes the row a run leaves when its conversation
// closes under it: `interrupted`, the word its kept row already wears, with the
// files its workers had touched by then. It is not a finding about the work —
// nobody was there to make one — and a later life of the run that finishes
// writes the row that supersedes it.
func (a *Agent) recordBeltRunInterrupted(run *beltRun, cost float64) {
	touched, unread := runTouchedFiles(run.tree, true)
	a.recordBeltRunIndex(run, TaskNotice{
		State: TaskInterrupted, Report: taskInterruptedOutcome, EndedAt: a.taskClockNow(),
	}, cost, touched, unread)
}

// interruptedRunRow is the closing row for one of this conversation's runs whose
// process went away mid-run, or false for a row that is not a run's. It carries
// the files the run's copy holds so far when the copy it was written down with
// is still there to read.
func (a *Agent) interruptedRunRow(row TaskIndexEntry, now time.Time) (TaskIndexEntry, bool) {
	g := a.tasker()
	if g == nil {
		return TaskIndexEntry{}, false
	}
	kept, found := runRowOf(g, taskIDNumber(row.ID))
	if !found || kept.PlanTask == "" {
		return TaskIndexEntry{}, false
	}
	closed := row
	closed.Status = string(TaskInterrupted)
	closed.Outcome = taskInterruptedOutcome
	closed.EndedAt = now
	closed.FilesUnread = "the run's copy was not written down"
	if tree, err := runCopyTree(kept.Copy, a.config.Place); err == nil {
		touched, unread := runTouchedFiles(tree, true)
		closed.Files, closed.FilesChanged = taskFileCitations(touched)
		closed.FilesUnread = unread
	} else if kept.Copy != nil {
		closed.FilesUnread = err.Error()
	}
	return closed, true
}

// presenceBeltRun is the run this window has out, and the hand-offs that
// joined it, as presence rows: the run's own row under the id its record row
// carries, so [SessionPresence.Holds] backs that row's claim of running, and
// each joined hand-off under its own id with the run as its parent.
//
// A RUN THAT IS ONLY LANDING IS STILL OUT until it has settled, and one whose
// conversation is closing is not: nothing will finish it here.
func (a *Agent) presenceBeltRun() []PresenceTask {
	a.beltMu.Lock()
	run := a.beltRun
	var joined []uint64
	closing := run == nil || run.closing
	if run != nil {
		joined = append(joined, run.joined...)
	}
	a.beltMu.Unlock()
	if closing {
		return nil
	}
	parent := strconv.FormatUint(run.row, 10)
	out := []PresenceTask{{
		ID: parent, Title: strings.TrimSpace(run.title), State: string(TaskRunning), StartedAt: run.born,
	}}
	g := a.tasker()
	for _, id := range joined {
		part := PresenceTask{ID: strconv.FormatUint(id, 10), State: string(TaskRunning), Parent: parent}
		if g != nil {
			if kept, found := runRowOf(g, id); found {
				if kept.State.settled() {
					continue
				}
				part.Title, part.StartedAt = kept.Title, kept.StartedAt
			}
		}
		out = append(out, part)
	}
	return out
}

// withoutListedRuns is the project's rows less this conversation's own rows for
// runs whose plan the `tasks` answer already lists. A run's plan rows are the
// fuller account — every part, its state, its result — and the record's row is
// the same run said again, so it is the one left out. The join is the run's
// store id ([planStoreID]), which is what the plan's root row is called.
func (a *Agent) withoutListedRuns(rows []TaskIndexEntry) []TaskIndexEntry {
	listed := map[string]bool{}
	for _, plan := range a.runPlanTasks() {
		listed[plan.ID] = true
	}
	if len(listed) == 0 {
		return rows
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	kept := make([]TaskIndexEntry, 0, len(rows))
	for _, row := range rows {
		if row.SessionID == session && listed[planStoreID(strings.TrimSpace(row.ID))] {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}
