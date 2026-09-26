package session

// A HAND-OFF'S RUN IS WORK THE PROJECT CAN SEE. A run's rows lived in this
// conversation's own graph and nowhere else: the project's index (tasks.jsonl)
// never took a row for one, and this conversation's presence never named one.
// So the `@` list, the hop's running count, the conversation list's roll-up,
// every other window and every other conversation's `tasks` tool were blind to
// a senior-dev run for its whole life and after it — the owner's project index
// held nothing of three runs that had taken an hour each.
//
// NOW EVERY ROW A RUN PUBLISHES REACHES THE INDEX, the way an adaptive run's
// does (orchestrate.go's family seam): a row saying running from the hand-off,
// and a row closing it when it settles, with its one pair and span
// (task_run_clock.go), its ending, its outcome, the branch its work was kept on
// and what it cost. The index keeps the last row per id ([lastPerNode]). A run
// left saying running by a process that went away is closed the next time this
// conversation opens ([Agent.closeInflightTaskIndexRows]), because a run's id is
// never one of the graph's own nodes. And while it runs it is named in this
// conversation's presence ([Agent.presenceBeltRuns]), which is what another
// window reads to know a running row in the index has something behind it.
//
// THIS CONVERSATION READS ITS OWN RUNS FROM THEIR STORE, not from the index
// ([Agent.withoutOwnRunRows]): the tasks tool lists them from the run's store
// by the numbers the rail shows, and a second copy of the same run from the
// index would be the same work named twice in one answer.

import (
	"strconv"
	"strings"
)

// indexRunRow appends one published run row to the project's index. A job's
// row is not work and takes none (jobrow.go's law), and neither does a row of
// an adaptive run, whose family writes its own.
func (a *Agent) indexRunRow(notice TaskNotice) {
	if notice.ID == 0 || notice.Kind == TaskKindJob || notice.Run != "" {
		return
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	entry := TaskIndexEntry{
		ID:         strconv.FormatUint(notice.ID, 10),
		Parent:     taskIndexParent(notice.Parent),
		Name:       TaskSlug(notice.Title),
		Label:      taskLabel(notice.Title),
		Title:      strings.TrimSpace(notice.Title),
		Status:     string(notice.State),
		Ending:     notice.Ending,
		Outcome:    taskOutcome(notice.Report),
		DurationMS: notice.Elapsed.Milliseconds(),
		StartedAt:  notice.StartedAt,
		EndedAt:    notice.EndedAt,
		SessionID:  session,
		// THE KEPT BRANCH ONLY, from the row's own word for how its work came
		// home ([keptBranchOf]): a run whose work was merged names none.
		Branch: keptBranchOf(notice.Branch, notice.Merge),
		// AND WHICH PROGRAM HAS IT, so every surface drawing this file can put the
		// program's badge on the row ([TaskIndexEntry.Program]).
		Program: notice.Program,
	}
	// A PERSON'S STOP IS THE ROW'S ENDING, as it is on a node's row
	// ([TaskNode.endingLocked]): the stop road publishes the flag and no word.
	if notice.Stopped && entry.Ending == "" {
		entry.Ending = TaskEndingStopped
	}
	entry.Files, entry.FilesChanged = taskFileCitations(notice.Changed)
	worktree := ""
	if where := notice.Copy; where != nil {
		worktree = where.Dir
		entry.Where, entry.Ground, entry.Mode, entry.Rung = where.Dir, where.Ground, where.Mode, where.Rung
	}
	entry.ArtifactURI = taskArtifactURI(worktree, notice.Branch, notice.Merge)
	if notice.State.settled() {
		entry.Cost = notice.CostUSD
		if entry.Cost == 0 {
			entry.Cost = a.beltRunSpent(notice.ID)
		}
	}
	a.recordTaskIndexEntry(entry)
	// Another window learns the run started, or ended, now rather than at the
	// next heartbeat.
	a.nudgePresence()
}

// beltRunSpent is what the live run whose own row id is this one came to, as
// its engine answered, and zero for every other row: a hand-off that joined
// the run has no figure of its own, and zero is drawn as no price.
func (a *Agent) beltRunSpent(id uint64) float64 {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	if run := a.beltRun; run != nil && run.row == id {
		return run.spent
	}
	return 0
}

// presenceBeltRuns is the live run this conversation has out, and each hand-off
// that joined it and has not settled, one presence row each under the id its
// index row carries — the join another window makes to know the running row in
// the index is being worked ([SessionRow.Runs]). A run no longer live, and a row
// that has settled, is the index's to report. It reads the graph without
// building one, and takes the belt's lock and the graph's one after the other,
// never together.
func (a *Agent) presenceBeltRuns() []PresenceTask {
	a.beltMu.Lock()
	run := a.beltRun
	var ids []uint64
	if run != nil {
		ids = append([]uint64{run.row}, run.joined...)
	}
	a.beltMu.Unlock()
	graph := a.tasker()
	if run == nil || graph == nil {
		return nil
	}
	var out []PresenceTask
	for _, id := range ids {
		for _, kept := range graph.runRows(id) {
			if kept.ID != id || kept.State.settled() {
				continue
			}
			row := PresenceTask{
				ID:        strconv.FormatUint(id, 10),
				Title:     strings.TrimSpace(kept.Title),
				State:     string(kept.State),
				StartedAt: kept.StartedAt,
			}
			if kept.Parent != 0 {
				row.Parent = strconv.FormatUint(kept.Parent, 10)
			}
			out = append(out, row)
		}
	}
	return out
}

// ownRunRowIDs is every hand-off run row this conversation keeps, by id: the
// rows its tasks tool reads from the run's store and never from the index. An
// adaptive run's rows and a job's are not among them.
func (a *Agent) ownRunRowIDs() map[string]bool {
	graph := a.tasker()
	if graph == nil {
		return nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	var ids map[string]bool
	for _, notice := range graph.runRowsLocked() {
		if notice.Run != "" || notice.Kind == TaskKindJob {
			continue
		}
		if ids == nil {
			ids = make(map[string]bool)
		}
		ids[strconv.FormatUint(notice.ID, 10)] = true
	}
	return ids
}

// withoutOwnRunRows is the index as this conversation's tasks tool reads it:
// every row but this conversation's own hand-off runs, which the tool reads
// from their store ([Agent.runPlanTasks]) under the numbers the rail shows. Left
// in, the same run would be listed twice in one answer, and a word for it —
// `say`, `continue` — would be answered as though it were the work of an
// earlier conversation.
func (a *Agent) withoutOwnRunRows(rows []TaskIndexEntry) []TaskIndexEntry {
	own := a.ownRunRowIDs()
	if len(own) == 0 {
		return rows
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	kept := make([]TaskIndexEntry, 0, len(rows))
	for _, row := range rows {
		if row.SessionID == session && own[strings.TrimSpace(row.ID)] {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}
