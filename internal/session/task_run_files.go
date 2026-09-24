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
)

// runTouchedFiles is every path a run's work changed against the commit its copy
// was cut from, and "" — or the reason the list could not be read.
//
// The copy is read while it is still on disk, which covers what the landing
// committed and anything it left uncommitted. A copy that has already been
// given back is read off its branch instead, from the repository that holds it.
func runTouchedFiles(tree taskTree) ([]string, string) {
	base, ok := checkBaseFor(tree)
	if !ok {
		return nil, "the commit the run's copy was cut from is not on record"
	}
	dir := strings.TrimSpace(tree.dir)
	args := []string{"diff", "--name-only", "--no-renames", "-z", base.sha}
	if info, err := os.Stat(dir); dir == "" || err != nil || !info.IsDir() {
		branch := strings.TrimSpace(tree.branch)
		if branch == "" {
			return nil, "the run's copy is gone and it named no branch"
		}
		dir = base.repository
		args = append(args, branch)
	}
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

// recordBeltRunIndex writes the run's row into the project's record, once, when
// the run has ended: the row every reader of that record already reads for a
// task on the older engine, with the files the run touched on it.
//
// IT IS ONE ROW, WRITTEN AT THE END. A run that is still going is presence's
// and its own store's to report, and a row claiming `running` would be a claim
// about a process that nothing closes when the process goes. A run whose
// conversation closed under it writes nothing here: it is not over, and the
// road that carries it on writes the row when it is.
func (a *Agent) recordBeltRunIndex(run *beltRun, notice TaskNotice, cost float64, touched []string, unread string) {
	if run == nil {
		return
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	files, count := taskFileCitations(touched)
	entry := TaskIndexEntry{
		ID:           strconv.FormatUint(run.row, 10),
		Name:         TaskSlug(run.title),
		Label:        taskLabel(run.title),
		Title:        strings.TrimSpace(run.title),
		Ground:       run.ground,
		Mode:         run.tree.mode,
		Rung:         run.tree.rung,
		Status:       string(notice.State),
		Ending:       notice.Ending,
		Outcome:      taskOutcome(notice.Report),
		FilesChanged: count,
		Files:        files,
		FilesUnread:  strings.TrimSpace(unread),
		Cost:         cost,
		StartedAt:    run.born,
		EndedAt:      notice.EndedAt,
		SessionID:    session,
		Branch:       keptBranchOf(notice.Branch, notice.Merge),
	}
	if notice.Stopped {
		entry.Ending = TaskEndingStopped
	}
	if !run.born.IsZero() && notice.EndedAt.After(run.born) {
		entry.DurationMS = notice.EndedAt.Sub(run.born).Milliseconds()
	}
	a.recordTaskIndexEntry(entry)
}
