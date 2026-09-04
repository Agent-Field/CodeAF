package tui3

import "github.com/Agent-Field/aforge-v2/internal/session"

// ── THE ONE READING EVERY TASK SURFACE DRAWS FROM ───────────────────────────
//
// [session.ProjectTask] is where "what is this task doing, and is anything mine
// to do about it" is answered. This file is the two doors onto it — one for the
// nodes this window is watching, one for the rows the project's record holds —
// and the one table that turns a reading into this surface's own words.
//
// THE VOCABULARY STAYS HERE. The engine answers with identifiers and this file
// spells them, which is the same split the record page has always had
// (taskview.go): a screen that says `needs your look` where the code says
// TaskUnverified is a surface decision, and the engine has no business holding a
// copy of it.

// taskStatus reads one node this window is watching.
//
// LIVENESS IS ONLY CLAIMED WHERE IT IS KNOWN. A node this session met while it
// ran is held by this session; a node replayed out of a checkpoint was never
// watched here ([taskNode.restored]), and this window has no standing to say
// whether anything is behind it — so it says nothing, and the reading follows
// the state as recorded rather than calling the work dead.
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

// taskPresenceWord is the reading in this surface's words, and it is the only
// table that spells one. A presence this build has no word for draws nothing at
// all rather than falling through to `done`, which is what the record page's own
// switch used to do with a status it did not recognise.
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
		// THE FAULT IS THE ONLY THING THAT EARNS `failed`. A dropped connection,
		// a threshold, a check that named gaps and a brief whose world had moved
		// are all work that did not finish, and calling any of them a failure
		// reports a finding nobody made (session's TaskStatus.Fault).
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
