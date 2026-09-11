package tui3

// A RUN HELD AT ITS FUEL GATE STOPS CLAIMING TO BE WORKING.
//
// The ⏸ and the fold's loudest rank were already written for a fact this surface
// could not be told: nothing on a task notice said a run was standing at its
// gate, so a run that had spent its tank and was waiting to be topped up drew a
// turning spinner and filed itself under work in flight. The engine publishes it
// now (session's TaskNotice.Paused, stamped on the run's own row), and these are
// what the column, the strip and the counts do with it.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// pausedRunFixture plants one adaptive run the way the family seam publishes
// it: a root row for the run itself, and one worker hanging under it.
func pausedRunFixture(a *app) {
	a.taskUpdate(update(1, "Audit pricing", session.TaskRunning, session.TaskNotice{Run: "3"}))
	a.taskUpdate(update(2, "Read tariffs", session.TaskRunning,
		session.TaskNotice{Run: "3", Node: "n1", Parent: 1}))
	// The spinner is on the frame clock, so a test that reads a glyph has to say
	// which frame it was taken on (railtree_test.go's [railRun]).
	a.paints = 0
}

// gateRow is the run's own row as the engine republishes it when the gate goes
// up and again when it is answered: the same running row, with the one fact that
// moved on it.
func gateRow(paused bool) session.Event {
	return update(1, "Audit pricing", session.TaskRunning, session.TaskNotice{Run: "3", Paused: paused})
}

// THE GATE IS THE RUN'S AND THE WORK UNDER IT IS UNTOUCHED. Whatever was in
// flight when the tank emptied is still working, so a surface that painted the
// whole family held would be reporting a stillness that is happening on none of
// them.
func TestARunHeldAtItsGateStopsClaimingToBeWorking(t *testing.T) {
	a, _, _ := taskApp(t)
	pausedRunFixture(a)

	// Before the gate: an ordinary running family, and nobody is being asked
	// anything.
	if got := a.taskStatus(a.tasks[1]); got.Presence != session.TaskPresenceWorking || got.Attention {
		t.Fatalf("a run with fuel in the tank reads %+v", got)
	}
	if a.railGroupOf(a.tasks[1]) != railRunning {
		t.Fatalf("a working run is filed under %v", a.railGroupOf(a.tasks[1]))
	}

	a.taskUpdate(gateRow(true))

	if !a.tasks[1].Paused() {
		t.Fatal("the run's row did not take the gate the engine published")
	}
	if a.tasks[2].Paused() {
		t.Fatal("the worker under a held run was labelled held itself")
	}
	// THE READING IS THE ONE EVERY SURFACE COUNTS FROM: work that will not move
	// until a person says something.
	status := a.taskStatus(a.tasks[1])
	if status.Presence != session.TaskPresenceNeedsLook || status.On != session.TaskWaitPerson {
		t.Fatalf("the held run reads %q on %q", status.Presence, status.On)
	}
	if !status.Attention {
		t.Fatalf("a run out of money does not ask for a person: %+v", status)
	}
	if status.State != session.TaskRunning {
		t.Fatalf("the gate moved the run's lifecycle to %q", status.State)
	}
	// AND THE COUNTS AGREE WITH IT. The run leaves the running group for the one
	// that holds what is waiting on somebody; its worker does not move.
	if got := a.railGroupOf(a.tasks[1]); got != railAttention {
		t.Fatalf("the held run is filed under %v, want the group that asks for you", got)
	}
	if got := a.railGroupOf(a.tasks[2]); got != railRunning {
		t.Fatalf("the worker moved to %v when the run above it stopped", got)
	}
	if !a.taskAwaitsPerson(a.tasks[1]) {
		t.Fatal("the held run is not counted as waiting on a person")
	}

	// THE COLUMN DRAWS THE GATE ON THE RUN AND ON NOTHING ELSE.
	if got := plain(a.railTreeGlyph(a.tasks[1])); got != glyphPaused {
		t.Fatalf("the held run's cell is %q, want %q", got, glyphPaused)
	}
	if got := plain(a.railTreeGlyph(a.tasks[2])); got == glyphPaused {
		t.Fatal("the worker under a held run drew the gate's mark")
	}
	if row := mustRailRow(t, a, "Audit pricing"); !strings.Contains(row, glyphPaused) {
		t.Fatalf("the roster's row for a held run is %q", row)
	}
	if row := mustRailRow(t, a, "Read tariffs"); strings.Contains(row, glyphPaused) {
		t.Fatalf("the worker's row wears the run's gate: %q", row)
	}

	// AND SO DOES THE STRIP, which is the only door there is under the column's
	// width floor (taskstrip.go).
	a.width = 80
	a.touch()
	if !a.stripShowing() {
		t.Fatal("a run waiting on a person is not on the strip at all")
	}
	if got := stripText(a); !strings.Contains(got, glyphPaused) {
		t.Fatalf("the strip's chip for a held run is:\n%q", got)
	}
}

// THE GATE ARRIVES ON A ROW THAT DID NOT CHANGE STATE, which is exactly the
// shape the de-dup throws away: a held run goes on publishing `running`. It also
// has to come OFF the same way, because the row that says the person answered is
// another running row.
func TestTheGateSurvivesTheDeDupAndComesOffWhenItIsAnswered(t *testing.T) {
	a, _, _ := taskApp(t)
	pausedRunFixture(a)

	a.taskUpdate(gateRow(true))
	if !a.tasks[1].Paused() {
		t.Fatal("the de-dup swallowed the gate: the row stayed as it was")
	}

	// The same row again is the same news, and the de-dup is right to drop it —
	// every accepted update moves the row-space stamp an open task page re-files
	// against (app.go's railStamp).
	stamp := a.railStamp
	a.taskUpdate(gateRow(true))
	if a.railStamp != stamp {
		t.Fatal("a gate that had not moved was published to the surface again")
	}
	if !a.tasks[1].Paused() {
		t.Fatal("a repeat of the gate row took the gate off")
	}

	// The answer: the same running row with the gate down.
	a.taskUpdate(gateRow(false))
	if a.railStamp == stamp {
		t.Fatal("the row that says the gate was answered was dropped as a duplicate")
	}
	if a.tasks[1].Paused() {
		t.Fatal("the run is still asking a question the person answered")
	}
	if got := a.taskStatus(a.tasks[1]); got.Presence != session.TaskPresenceWorking || got.Attention {
		t.Fatalf("the answered run reads %+v, want it working again", got)
	}
	if got := a.railGroupOf(a.tasks[1]); got != railRunning {
		t.Fatalf("the answered run is filed under %v", got)
	}

	// AND A RUN THAT SETTLES WHILE THE GATE IS UP LEAVES IT BEHIND. The row that
	// closes the run says nothing about a gate, and a surface that kept the flag
	// would draw ⏸ over work that is over.
	a.taskUpdate(gateRow(true))
	a.taskUpdate(update(1, "Audit pricing", session.TaskFailed,
		session.TaskNotice{Run: "3", Stopped: true}))
	if a.tasks[1].Paused() {
		t.Fatal("a stopped run is still standing at its gate")
	}
	if got := a.taskStatus(a.tasks[1]); got.Presence != session.TaskPresenceStopped {
		t.Fatalf("the stopped run reads %q", got.Presence)
	}
}

// A HELD RUN IS A STILL PICTURE. The spinner is this surface's promise that
// something is happening this instant, and once the run's last worker has landed
// nothing is: the frame clock has no reason to keep repainting a column that
// cannot change until somebody answers.
func TestAHeldRunWithNothingLeftRunningStopsAskingForFrames(t *testing.T) {
	a, _, _ := taskApp(t)
	pausedRunFixture(a)
	if !a.tasksAnimating() {
		t.Fatal("a run with a worker in flight is not animating")
	}

	// The worker finishes what it was doing — which is what a run at its gate
	// waits for — and the run itself is left holding the question.
	a.taskUpdate(update(2, "Read tariffs", session.TaskDone,
		session.TaskNotice{Run: "3", Node: "n1", Parent: 1}))
	a.taskUpdate(gateRow(true))
	if a.tasksAnimating() {
		t.Fatal("a run that cannot move until a person answers is still turning a spinner")
	}
	// Answering it starts the picture moving again.
	a.taskUpdate(gateRow(false))
	if !a.tasksAnimating() {
		t.Fatal("the answered run is not drawing again")
	}
}
