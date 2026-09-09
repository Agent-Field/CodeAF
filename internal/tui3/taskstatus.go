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
		Paused:  node.paused,
		Stopped: node.stopped,
		Merge:   node.merge,
		Branch:  node.branch,
		Report:  node.report,
		// THE QUESTION IS THE ENGINE'S TO CHOOSE AND NOT THIS WINDOW'S. Which of
		// the six a your-call row is asking comes out of these three facts — the
		// landing that turned the work back, the files that would not fasten, and
		// who is holding the answer right now — and a surface that guessed at one
		// of them would be a card asking a different question from its own row.
		Held:      node.producedHeld,
		Conflicts: node.conflicts,
		Decider:   node.decider,
	}
	if !node.restored {
		facts.Liveness = session.TaskLivenessHeld
	}
	// AND A NODE THIS WINDOW HANDED TO THE MODEL IS THE MODEL'S TO DECIDE, which
	// is the same fact the engine publishes as [session.TaskNotice.Decider] and
	// the only one this surface knows first: the settle policy is read here, on
	// the card, before any of it reaches the wire (tasksettle.go). It is a fact
	// about WHO IS HOLDING THE QUESTION and it does not move the row out of its
	// tier — the row still asks, still wears the `?`, and still reads its reason.
	// It used to be published as a state of its own, `awaiting review`, which was
	// a fourth word for one reading and hid the reason behind it.
	if a.taskReviewPending(node) {
		facts.Decider = session.TaskAskOwnerModel
	}
	status := session.ProjectTask(facts)
	// AND A QUESTION SOMEBODY ELSE IS HOLDING IS NOT A DEMAND ON THIS PERSON.
	// [session.TaskStatus.Attention] is what files a row in the column's `needs
	// you` group, and a card handed to the model is exactly the row that must not
	// stand there — it is being answered, and it comes back by itself if it is
	// not (session's agent.go hands it back at the end of the turn). The tier,
	// the word and the question are untouched: the row still wears the `?` and
	// still reads its reason, because a person watching it is owed both.
	if status.Ask.Owner == session.TaskAskOwnerModel {
		status.Attention = false
	}
	return status
}

// taskEntryStatus reads one row of the project's record. `runs` is
// [session.SessionRow.Runs]' judgement — the one place this program decides
// whether a live-looking row is work that is HAPPENING — and the record's own
// gaps travel with it (session's [session.TaskIndexEntry.StatusFacts] lists
// them).
func taskEntryStatus(entry session.TaskIndexEntry, runs bool) session.TaskStatus {
	return session.ProjectTask(entry.StatusFacts(runs))
}

// taskPresenceWord is the reading in the person's words, and IT SPELLS NOTHING
// OF ITS OWN: the word is [session.TaskStatus.Word], written down once in
// internal/session and read out here.
//
// It used to be this surface's own table over the presence, and that table is
// where three of the deleted words lived — `needs your look` for a landing
// nobody could check, `awaiting review` for one handed to the model, `failed`
// for a run the wire ended. Each was a private name for a reading the engine had
// already made, and the roster, the record and home each held a slightly
// different copy of it. A reading with no word draws NOTHING, which is the
// emptiness law and not a fall-through to `done`.
func taskPresenceWord(status session.TaskStatus) string { return status.Word }

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
