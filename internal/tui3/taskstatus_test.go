package tui3

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// One reading, every surface.

// statusApp is a window with a roster to put nodes in; a running program makes
// the map on its first update.
func statusApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(nil)
	if a.tasks == nil {
		a.tasks = map[uint64]*taskNode{}
	}
	return a
}

// A person's stop is not a failure anywhere. The engine settles a stopped node
// `failed` because nothing merged; the roster read that off a live flag the
// record does not carry, so the record page, home and the mention menu drew a
// cross at somebody's own stop.
func TestAStoppedTaskIsNeverDrawnAsAFailure(t *testing.T) {
	a := statusApp(t)
	entry := session.TaskIndexEntry{
		Label: "Offline reading", Status: string(session.TaskFailed),
		Ending: session.TaskEndingStopped,
	}
	row := session.SessionRow{Open: true}

	if word := taskStateWord(entry, false); word != taskStoppedWord {
		t.Errorf("the record says %q about work a person stopped", word)
	}
	if glyph := plain(a.homeTaskGlyph(entry, row)); glyph == plain(a.pal.badGlyph()) {
		t.Errorf("home draws a failure's cross at a stop: %q", glyph)
	}
	if glyph := taskStatusGlyph(entry, false); glyph != glyphStopped {
		t.Errorf("the mention menu draws %q at a stop, want %q", glyph, glyphStopped)
	}

	// And the live row agrees with the record, in the same cell: the room header
	// used to draw a cross where the roster drew ⊘.
	node := &taskNode{id: 3, title: "Offline reading", state: session.TaskFailed, stopped: true}
	a.tasks[3] = node
	if mark := a.taskStateMark(node); mark != glyphStopped {
		t.Errorf("the roster draws %q at a stop, want %q", mark, glyphStopped)
	}
	if mark := a.roomMark(node); mark != a.taskStateMark(node) {
		t.Errorf("the room header draws %q where the roster draws %q", mark, a.taskStateMark(node))
	}
}

// Work the wire ended is unfinished, not failed: nothing was found out about the
// job. The rail has said so since endings landed; the record's word did not.
func TestARunTheWireEndedReadsAsIncompleteEverywhere(t *testing.T) {
	a := statusApp(t)
	for _, ending := range []session.TaskEnding{
		session.TaskEndingWire,
		session.TaskEndingUpstream,
		session.TaskEndingSteps,
		session.TaskEndingRefused,
		session.TaskEndingStale,
	} {
		entry := session.TaskIndexEntry{Status: string(session.TaskFailed), Ending: ending}
		if word := taskStateWord(entry, false); word != taskRecordStoppedWord {
			t.Errorf("%s: the record says %q, want %q", ending, word, taskRecordStoppedWord)
		}
		node := &taskNode{id: 9, state: session.TaskFailed, ending: ending}
		// THE CELL IS THE TIER'S AND THE TIER HAS THREE OVER-CELLS. Work that did
		// not finish wears the cross DIM; the `!` that used to stand here was a
		// fourth answer to a question with three (tasktier.go).
		if mark := a.taskStateMark(node); mark != a.linearMark(glyphBad, glyphBadASCII) {
			t.Errorf("%s: the roster draws %q, want %q", ending, mark, glyphBad)
		}
		if ink := a.taskStateInk(node); ink("x") != a.pal.dim("x") {
			t.Errorf("%s: an unfinished row is painted like a fault", ending)
		}
	}

	// And a fault says the same word and is the only one painted like one —
	// including a row from an engine too old to name an ending.
	for _, ending := range []session.TaskEnding{session.TaskEndingError, ""} {
		entry := session.TaskIndexEntry{Status: string(session.TaskFailed), Ending: ending}
		if word := taskStateWord(entry, false); word != taskRecordStoppedWord {
			t.Errorf("%q: the record says %q, want %q", ending, word, taskRecordStoppedWord)
		}
		node := &taskNode{id: 9, state: session.TaskFailed, ending: ending}
		if mark := a.taskStateMark(node); mark != a.linearMark(glyphBad, glyphBadASCII) {
			t.Errorf("%q: the roster draws %q at a fault", ending, mark)
		}
		if ink := a.taskStateInk(node); ink("x") != a.pal.bad("x") {
			t.Errorf("%q: a fault is not painted as one", ending)
		}
	}
}

// Admission is not execution. A queued node in a live session was worded
// `running`, because the record's word asked only whether the session was
// alive.
func TestAQueuedRowIsNotWordedAsRunning(t *testing.T) {
	entry := session.TaskIndexEntry{Status: string(session.TaskQueued)}
	if word := taskStateWord(entry, true); word != roomQueuedWord {
		t.Errorf("a queued row in a live session says %q, want %q", word, roomQueuedWord)
	}
	// AND A RUNNING ROW SAYS `working`, which is the one word every surface says
	// for it since the task-states wave: the record used to spell it `running`,
	// the rail `working`, and one state with two spellings is two states to
	// whoever is reading (docs/design/task-states/DESIGN.md).
	running := session.TaskIndexEntry{Status: string(session.TaskRunning)}
	if word := taskStateWord(running, true); word != "working" {
		t.Errorf("a running row says %q, want %q", word, "working")
	}
}

// A live-looking row nothing holds is unfinished work, and it is quiet: nothing
// went wrong with work a window walked away from, so it draws the still dot.
func TestARowNothingHoldsIsQuietlyIncomplete(t *testing.T) {
	a := statusApp(t)
	entry := session.TaskIndexEntry{Status: string(session.TaskRunning)}
	if word := taskStateWord(entry, false); word != taskRecordStoppedWord {
		t.Errorf("the record says %q about a row nothing holds", word)
	}
	glyph := plain(a.homeTaskGlyph(entry, session.SessionRow{}))
	if glyph != glyphQueued {
		t.Errorf("home draws %q for a row nothing holds, want the still %q", glyph, glyphQueued)
	}
	if glyph, _ := tasksGlyph(tasksItem{entry: entry, runs: false, section: tasksRunning}, a.pal); glyph != glyphIdle {
		t.Errorf("the task place draws %q for a row nothing holds, want %q", glyph, glyphIdle)
	}
}

// The end of a run is finishing, not working — read off the lifecycle fact the
// engine publishes rather than off whether a sentence arrived with it.
func TestTheCheckAndARepairRoundReadAsFinishing(t *testing.T) {
	a := statusApp(t)
	for _, life := range []string{session.TaskPhaseChecking, session.TaskPhaseRepairing} {
		node := &taskNode{id: 2, state: session.TaskRunning, phase: life}
		if got := a.taskStatus(node).Presence; got != session.TaskPresenceFinishing {
			t.Errorf("%s reads as %q, want %q", life, got, session.TaskPresenceFinishing)
		}
	}
	working := &taskNode{id: 2, state: session.TaskRunning, phase: session.TaskPhaseWorking}
	if got := a.taskStatus(working).Presence; got != session.TaskPresenceWorking {
		t.Errorf("a node at its own work reads as %q", got)
	}
	// And a run is still a run while it finishes: something is happening.
	node := &taskNode{id: 2, state: session.TaskRunning, phase: session.TaskPhaseChecking}
	a.tasks[2] = node
	if a.railGroupOf(node) != railRunning {
		t.Error("a node under its check was filed away from the running work")
	}
}

// A node this window never watched is not declared dead: work outlives the
// terminal that started it, so a checkpoint row keeps its claim.
func TestARestoredRunningNodeKeepsItsClaim(t *testing.T) {
	a := statusApp(t)
	node := &taskNode{id: 5, state: session.TaskRunning, restored: true}
	status := a.taskStatus(node)
	if status.Presence != session.TaskPresenceWorking {
		t.Errorf("a restored running node reads as %q, want %q", status.Presence, session.TaskPresenceWorking)
	}
	if status.Liveness != session.TaskLivenessUnknown {
		t.Errorf("this window claimed %q about a node it never watched", status.Liveness)
	}
}

// Edits nobody brought home are their own axis: a done node whose branch
// conflicted is still done, and still needs a person.
func TestUnlandedEditsAskForAPersonWithoutMovingTheState(t *testing.T) {
	a := statusApp(t)
	node := &taskNode{id: 6, state: session.TaskDone, merge: mergeWordConflicted, branch: "task/fix-nil"}
	a.tasks[6] = node
	status := a.taskStatus(node)
	if status.Presence != session.TaskPresenceDone {
		t.Errorf("presence = %q, want %q", status.Presence, session.TaskPresenceDone)
	}
	if !status.ChangesUnlanded() {
		t.Error("a conflicted branch reads as landed")
	}
	if group := a.railGroupOf(node); group != railAttention {
		t.Errorf("the roster files unlanded edits under %q", railGroupWords[group])
	}
	if rank := a.railGlyphRank(node); rank != 0 {
		t.Errorf("unlanded edits rank %d, want the loudest", rank)
	}
}

// The prerequisite sentence is the reading's, so the row, the group it is filed
// under and the composer's line cannot disagree about what a node is behind.
func TestABlockedNodeNamesWhatItWaitsOn(t *testing.T) {
	a := statusApp(t)
	a.tasks[1] = &taskNode{id: 1, title: "Collect sources", state: session.TaskRunning}
	a.tasks[2] = &taskNode{id: 2, title: "Draft outline", state: session.TaskQueued, dependsOn: []uint64{1}}
	blocked := a.tasks[2]

	status := a.taskStatus(blocked)
	if status.Presence != session.TaskPresenceWaiting || status.On != session.TaskWaitWork {
		t.Fatalf("a blocked node reads as %q on %q", status.Presence, status.On)
	}
	if status.Reason != "Collect sources" {
		t.Errorf("it waits on %q", status.Reason)
	}
	if waits := a.railWaits(blocked); waits != "Collect sources" {
		t.Errorf("the row says it waits on %q", waits)
	}
	if group := a.railGroupOf(blocked); group != railParked {
		t.Errorf("a blocked node is filed under %q", railGroupWords[group])
	}

	// A node behind nothing but the machine is queued, not waiting on work: the
	// row must not send anybody looking for a prerequisite that does not exist.
	held := &taskNode{id: 3, state: session.TaskQueued, waiting: "machine busy"}
	a.tasks[3] = held
	if got := a.taskStatus(held); got.Presence != session.TaskPresenceQueued || got.Reason != "machine busy" {
		t.Errorf("a held queued node reads as %q · %q", got.Presence, got.Reason)
	}
	if a.railWaits(held) != "" {
		t.Error("a queued node behind the machine claims a prerequisite")
	}
	if group := a.railGroupOf(held); group != railIdle {
		t.Errorf("a held queued node is filed under %q", railGroupWords[group])
	}
}
