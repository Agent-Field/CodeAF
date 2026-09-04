package tui3

import "github.com/Agent-Field/aforge-v2/internal/session"

// The two doors onto [session.ProjectTask], and the one table that turns a
// reading into this surface's own words. The engine answers with identifiers and
// the vocabulary stays here, which is the split the record page has always had
// (taskview.go).

// taskStatus reads one node this window is watching. Liveness is claimed only
// where it is known: a node this session met while it ran is held here, and one
// replayed out of a checkpoint ([taskNode.restored]) was never watched, so this
// window says nothing about it rather than calling the work dead.
func (a *app) taskStatus(node *taskNode) session.TaskStatus {
	if node == nil {
		return session.TaskStatus{}
	}
	facts := session.TaskFacts{
		State:   node.state,
		Ending:  node.ending,
		Life:    node.phase,
		Kind:    node.kind,
		Phase:   node.doing,
		Gap:     node.mending,
		Hold:    node.waiting,
		Waits:   a.taskWaitTitles(node),
		Stopped: node.stopped,
		Merge:   node.merge,
		Branch:  node.branch,
	}
	if !node.restored {
		facts.Liveness = session.TaskLivenessHeld
	}
	return session.ProjectTask(facts)
}

// taskEntryStatus reads one row of the project's record. `runs` is
// [session.SessionRow.Runs]' judgement — the one place this program decides
// whether a live-looking row is work that is HAPPENING — and the record's own
// gaps travel with it (session's [session.TaskIndexEntry.StatusFacts] lists
// them).
func taskEntryStatus(entry session.TaskIndexEntry, runs bool) session.TaskStatus {
	return session.ProjectTask(entry.StatusFacts(runs))
}

// taskPresenceWord is the reading in this surface's words, and the only table
// that spells one. A presence with no word draws nothing rather than falling
// through to `done`, which is what the record page's switch did with a status it
// did not recognise.
func taskPresenceWord(status session.TaskStatus) string {
	switch status.Presence {
	case session.TaskPresenceQueued:
		return roomQueuedWord
	case session.TaskPresenceWorking:
		return taskRecordRunsWord
	case session.TaskPresenceWaiting:
		return taskHeldWord
	case session.TaskPresenceFinishing:
		return taskFinishingWord
	case session.TaskPresenceDone:
		return doneWord
	case session.TaskPresenceNeedsLook:
		return taskUnverifiedWord
	case session.TaskPresenceStopped:
		return taskStoppedWord
	case session.TaskPresenceIncomplete:
		// The fault is the only thing that earns `failed`: a dropped connection, a
		// threshold, a check that named gaps and a brief whose world had moved are
		// all work that did not finish, and calling any of them a failure reports a
		// finding nobody made.
		if status.Fault {
			return doneFailWord
		}
		return taskRecordStoppedWord
	}
	return ""
}

// taskWaitTitles names the prerequisites a node is still blocked on, oldest
// first. A dependency this surface has never seen an update for is skipped
// rather than named as an id: a row that says "waits: 7" has told nobody
// anything.
func (a *app) taskWaitTitles(node *taskNode) []string {
	var names []string
	for _, id := range node.dependsOn {
		dep := a.tasks[id]
		if dep == nil || dep.state == session.TaskDone {
			continue
		}
		names = append(names, dep.title)
	}
	return names
}
