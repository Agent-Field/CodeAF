package tui3

// ── EVERY STRETCH OF A TASK'S LIFE SAYS SOMETHING TRUE AND MOVING ───────────
//
// From the moment a task is admitted to the moment its answer comes back, the
// page a person is standing on has to say what is happening — and it has to stop
// saying the last thing the instant it stops being true. A word left standing
// from the life before is the same fault as a blank page: the person reads it,
// believes it, and goes looking for a fault that is not there.
//
// The words themselves are owned elsewhere and each has its own test — the room
// header's ladder (room.go's [app.roomStateWord]), the phase words
// (taskphase.go), the "nothing yet" line (roomunlanded_test.go). This walks ONE
// node through all of them in one page's life, which is the thing none of those
// can check: that each stretch draws immediately, and that nothing from the
// stretch before survives into it.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// roomLifeSays is the header and the body of the open page together — the two
// places a person looks to answer "what is happening".
func roomLifeSays(a *app) string {
	return plain(roomHeadAll(a, 160)) + "\n" + roomText(a)
}

func TestATaskRoomWalksItsWholeLifeWithoutAStaleWord(t *testing.T) {
	a, agent, _ := roomApp(t)
	// No journal: the page opens with nothing replayed, which is the state every
	// task is in for its first seconds and the one a blank page is reached from.
	agent.journal = ""

	// ── QUEUED. Work that has not begun has journaled nothing because there was
	// nothing to journal, and the page says exactly that.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskQueued, session.TaskNotice{})})
	clickRail(t, a, 0)
	page := roomLifeSays(a)
	for _, want := range []string{roomQueuedWord, roomYetWord} {
		if !strings.Contains(page, want) {
			t.Fatalf("a queued page does not say %q:\n%s", want, page)
		}
	}
	for _, banned := range []string{roomFinishedRefusal.what, roomGoneWord, taskCheckingWord} {
		if strings.Contains(page, banned) {
			t.Fatalf("a queued page says %q:\n%s", banned, page)
		}
	}

	// ── THE WORKER TALKING. The deck takes the page over the moment the first
	// thing arrives on the lane, and the line about an empty page comes off.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{})})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTextDelta, Text: "Reading parseRow now.",
	}})
	page = roomLifeSays(a)
	if !strings.Contains(page, "Reading parseRow now.") {
		t.Fatalf("the worker's line is not on its page:\n%s", page)
	}
	if !strings.Contains(page, stateWorking.String()) {
		t.Fatalf("a working page does not say so:\n%s", page)
	}
	for _, banned := range []string{roomYetWord, roomQueuedWord} {
		if strings.Contains(page, banned) {
			t.Fatalf("a page with the worker talking still says %q:\n%s", banned, page)
		}
	}

	// ── THE CHECK. The node is still `running` and the worker has stopped
	// talking; without a word here the page is a clock over silence for minutes.
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseChecking, 0, 0, "")})
	page = roomLifeSays(a)
	if !strings.Contains(page, taskCheckingWord) {
		t.Fatalf("a checked page does not say what is happening:\n%s", page)
	}
	if strings.Contains(plain(roomHeadAll(a, 160)), stateWorking.String()) {
		t.Fatalf("the header still calls a checked node working:\n%s", page)
	}

	// ── A REPAIR ROUND, with how far through it is.
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseRepairing, 1, 2,
		"not done — go test reports no test files")})
	page = roomLifeSays(a)
	if !strings.Contains(page, taskClosingWord+railSep+"round 1 of 2") {
		t.Fatalf("a repairing page does not say which round it is on:\n%s", page)
	}
	if strings.Contains(page, taskCheckingWord) {
		t.Fatalf("a repairing page still says it is being checked:\n%s", page)
	}

	// ── AND BACK AT ITS OWN WORK, which is the transition a stale word survives:
	// the engine says `working` on the way out of a round, and everything the
	// round put on the row goes with it.
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseWorking, 0, 0, "")})
	page = roomLifeSays(a)
	for _, banned := range []string{taskCheckingWord, taskClosingWord} {
		if strings.Contains(page, banned) {
			t.Fatalf("a node back at work still says %q:\n%s", banned, page)
		}
	}
	if !strings.Contains(page, stateWorking.String()) {
		t.Fatalf("a node back at work does not say so:\n%s", page)
	}

	// ── LANDED. The lane closes, the foot goes on, and no word from any earlier
	// stretch is left anywhere on the page.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{})})
	drive(t, a, roomClosedMsg{gen: a.room.gen})
	page = roomLifeSays(a)
	if !strings.Contains(page, roomFinishedRefusal.what) {
		t.Fatalf("a landed page never took its foot:\n%s", page)
	}
	for _, banned := range []string{roomQueuedWord, roomYetWord, taskCheckingWord, taskClosingWord} {
		if strings.Contains(page, banned) {
			t.Fatalf("a landed page still says %q:\n%s", banned, page)
		}
	}
}
