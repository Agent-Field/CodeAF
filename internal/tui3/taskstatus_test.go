package tui3

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── ONE READING, EVERY SURFACE ──────────────────────────────────────────────

// statusApp is a window with a roster to put nodes in. The map is made by the
// first update in a running program, and these tests are about the reading
// rather than about the events that fill it.
func statusApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(nil)
	if a.tasks == nil {
		a.tasks = map[uint64]*taskNode{}
	}
	return a
}

// A PERSON'S STOP IS NOT A FAILURE ANYWHERE. The engine settles a stopped node
// `failed` because nothing merged, and before the reading existed each surface
// decided for itself what to make of that: the roster drew ⊘ off a live flag the
// record does not carry, so the record page, home and the mention menu drew a
// failure's cross at somebody's own stop.
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

	// AND THE LIVE ROW AGREES WITH THE RECORD, in the same cell: the roster and
	// the room header used to disagree about this node, because the header's own
	// switch knew the refusal mark and neither the stop's nor the halt's.
	node := &taskNode{id: 3, title: "Offline reading", state: session.TaskFailed, stopped: true}
	a.tasks[3] = node
	if mark := a.taskStateMark(node); mark != glyphStopped {
		t.Errorf("the roster draws %q at a stop, want %q", mark, glyphStopped)
	}
	if mark := a.roomMark(node); mark != a.taskStateMark(node) {
		t.Errorf("the room header draws %q where the roster draws %q", mark, a.taskStateMark(node))
	}
}

// WORK THE WIRE ENDED IS UNFINISHED, NOT FAILED. Nothing was found out about the
// job, so a cross would report a finding nobody made. The rail has said this
// since endings landed; the record's own word did not, and a person who read
// `failed` on the history page had no way to tell it from a fault.
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
		if mark := a.taskStateMark(node); mark != glyphHalted {
			t.Errorf("%s: the roster draws %q, want %q", ending, mark, glyphHalted)
		}
	}

	// AND A FAULT STILL WEARS THE CROSS. A working copy that could not be made
	// and an error nobody classified are the news the cross exists for, and so is
	// a row from an engine too old to name an ending.
	for _, ending := range []session.TaskEnding{session.TaskEndingError, ""} {
		entry := session.TaskIndexEntry{Status: string(session.TaskFailed), Ending: ending}
		if word := taskStateWord(entry, false); word != doneFailWord {
			t.Errorf("%q: the record says %q, want %q", ending, word, doneFailWord)
		}
		node := &taskNode{id: 9, state: session.TaskFailed, ending: ending}
		if mark := a.taskStateMark(node); mark != a.linearMark(glyphBad, glyphBadASCII) {
			t.Errorf("%q: the roster draws %q at a fault", ending, mark)
		}
	}
}

// ADMISSION IS NOT EXECUTION. A queued node in a live session was drawn and
// worded as `running` because the record's word asked only whether the session
// was alive — so a person watching a task that had not started yet was told a
// worker was at it.
func TestAQueuedRowIsNotWordedAsRunning(t *testing.T) {
	entry := session.TaskIndexEntry{Status: string(session.TaskQueued)}
	if word := taskStateWord(entry, true); word != roomQueuedWord {
		t.Errorf("a queued row in a live session says %q, want %q", word, roomQueuedWord)
	}
	running := session.TaskIndexEntry{Status: string(session.TaskRunning)}
	if word := taskStateWord(running, true); word != taskRecordRunsWord {
		t.Errorf("a running row says %q, want %q", word, taskRecordRunsWord)
	}
}

// A LIVE-LOOKING ROW NOTHING HOLDS IS UNFINISHED WORK, and it is quiet: nothing
// went wrong with work a window walked away from, so it wears the still dot
// rather than a steer mark or a spinner.
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

// THE END OF A RUN IS FINISHING AND NOT WORKING. The check reading what the
// worker left and a round closing what it found are minutes each, and they are
// read off the lifecycle fact the engine publishes rather than off whether a
// sentence happened to arrive with them.
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
	// AND A RUN IS STILL A RUN WHILE IT FINISHES: the roster's spinner is the
	// promise that something is happening, and during a check something is.
	node := &taskNode{id: 2, state: session.TaskRunning, phase: session.TaskPhaseChecking}
	a.tasks[2] = node
	if a.railGroupOf(node) != railRunning {
		t.Error("a node under its check was filed away from the running work")
	}
}

// A NODE THIS WINDOW NEVER WATCHED IS NOT DECLARED DEAD. Work outlives the
// terminal that started it, so a checkpoint row claiming to be running is taken
// at its word here rather than being drawn as unfinished on this window's say-so.
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

// EDITS NOBODY BROUGHT HOME ARE THEIR OWN AXIS. A done node whose branch
// conflicted is still done, and it still needs a person — the state and the
// branch answer different questions and the roster reads both.
func TestUnlandedEditsAskForAPersonWithoutMovingTheState(t *testing.T) {
	a := statusApp(t)
	node := &taskNode{id: 6, state: session.TaskDone, merge: mergeWordConflicted, branch: "task/fix-nil"}
	a.tasks[6] = node
	status := a.taskStatus(node)
	if status.Presence != session.TaskPresenceDone {
		t.Errorf("presence = %q, want %q", status.Presence, session.TaskPresenceDone)
	}
	if !status.ChangesUnlanded() || status.Next != session.TaskNextCollect {
		t.Errorf("a conflicted branch offers %q and unlanded=%v", status.Next, status.ChangesUnlanded())
	}
	if group := a.railGroupOf(node); group != railAttention {
		t.Errorf("the roster files unlanded edits under %q", railGroupWords[group])
	}
	if rank := a.railGlyphRank(node); rank != 0 {
		t.Errorf("unlanded edits rank %d, want the loudest", rank)
	}
}

// THE PREREQUISITE SENTENCE IS THE READING'S, so the row, the group it is filed
// under and the composer's line about it cannot disagree about whether a node is
// behind other work.
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

	// A NODE BEHIND NOTHING BUT THE MACHINE IS QUEUED AND NOT WAITING ON WORK:
	// that hold clears itself, and the row must not send anybody looking for a
	// prerequisite that does not exist.
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
