package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE ROOM'S FACTS AGE WITH THE ROOM. These pin that the top bar's trimmings
// (topbar.go) say nothing stale while a node is in flight, and that the way
// back is said where a person in a room actually looks.

// A SETTLED NODE'S CLOCK STOPS MOVING ON THE TOP BAR. The header used to go on
// saying "running · 12m04s" about work that had landed, because the clock's
// trigger was "the node HAS a start" rather than "the node is STILL running".
// What a landed node says is its final age, as the update that ended it
// reported — and a queued node carries no clock at all: nothing has begun, so
// nothing is said (the same rule the conversation's turn footer keeps,
// timestamps.go).
func TestTheClockLeavesTheBarWhenTheNodeSettles(t *testing.T) {
	a, _, advance := roomApp(t)
	a.openRoom(7, "Fix the nil-map crash")
	advance(2 * time.Minute)
	clock := a.roomClock(a.tasks[7])
	if clock == "" {
		t.Fatal("a running node carries no clock")
	}
	width := a.bodyWidth()
	if bar := plain(a.topBarWord(width)); !strings.Contains(bar, clock) {
		t.Fatalf("the bar is %q, want the running clock %q", bar, clock)
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{Merge: "merged cleanly", Branch: "task/nil-map"})})
	if got := a.roomClock(a.tasks[7]); got != "" {
		t.Fatalf("a landed node still says %q", got)
	}
	if bar := plain(a.topBarWord(width)); strings.Contains(bar, clock) {
		t.Fatalf("the bar still says %q after the node landed: %q", clock, bar)
	}
}

// THE SPEND JOINS IT, AND NOTHING GAPS. A landed node keeps what it cost — the
// one figure on the line that is still true — but the state word and the clock
// leave together, so the trimmings never draw "· ·" with a fact's share of the
// sentence missing.
func TestTheSpendStaysAndLeavesNoHole(t *testing.T) {
	a, _, advance := roomApp(t)
	a.openRoom(7, "Fix the nil-map crash")
	advance(90 * time.Second)
	// THE PRICE COMES OFF THE NODE'S OWN NOTICE (session's TaskNotice.CostUSD),
	// which is the only thing the trimming reads: a turn's usage is the
	// conversation's bill and not this node's.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{CostUSD: 0.04})})
	width := a.bodyWidth()
	before := plain(a.topBarWord(width))
	if !strings.Contains(before, "$0.04") {
		t.Fatalf("the bar is %q, want the spend", before)
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{Merge: "merged cleanly", Branch: "task/nil-map"})})
	after := plain(a.topBarWord(width))
	if !strings.Contains(after, "$0.04") {
		t.Fatalf("the spend left with the clock: %q", after)
	}
	if strings.Contains(after, "· ·") || strings.Contains(after, "·  ·") {
		t.Fatalf("the trimmings gap where the clock was: %q", after)
	}
}

// THE LEGEND SAYS HOW TO LEAVE, IN THE PLACE THE PATH STOOD. A person in a
// room is not on the path any more, so the legend's left end trades the place
// for the door — "room · esc/← back" — rather than carrying both. It goes back
// the moment the room closes.
func TestTheLegendTradesThePlaceForTheWayBack(t *testing.T) {
	a, _, _ := roomApp(t)
	left, _ := a.legendLeft(120, 60)
	if strings.Contains(left, "esc") {
		t.Fatalf("the resting legend already says how to leave a room: %q", left)
	}
	a.openRoom(7, "Fix the nil-map crash")
	left, _ = a.legendLeft(120, 60)
	if left != roomLegendWord {
		t.Fatalf("the legend in a room is %q, want %q", left, roomLegendWord)
	}
	a.closeRoom()
	left, _ = a.legendLeft(120, 60)
	if strings.Contains(left, "esc") {
		t.Fatalf("the legend kept the way out after the room closed: %q", left)
	}
}

// A NODE THIS SURFACE HAS NO RECORD OF STILL GETS A BAR: the trail, and nothing
// else. It is the room-opened-from-a-landed-card case — the card names work the
// roster no longer holds, and the trimmings must say nothing rather than guess.
func TestTheBarOverAnUnknownNodeCarriesOnlyTheTrail(t *testing.T) {
	a, _, _ := roomApp(t)
	a.room = &taskRoom{id: 9, title: "Rekey the sessions", unfolded: map[int]bool{}, live: -1, think: -1}
	if node := a.roomNode(); node != nil {
		t.Fatalf("a room on id 9 found a node: %v", node)
	}
	width := a.bodyWidth()
	bar := plain(a.topBarWord(width))
	if !strings.Contains(bar, "Rekey the sessions #9") {
		t.Fatalf("the bar is %q, want the trail with the room's handle", bar)
	}
	for _, never := range []string{stateWorking.String(), "$", "· ·"} {
		if strings.Contains(bar, never) {
			t.Fatalf("the bar over an unknown node says %q: %q", never, bar)
		}
	}
}

// ── EVERY STRETCH OF A TASK'S LIFE SAYS SOMETHING TRUE AND MOVING ───────────
//
// From the moment a task is admitted to the moment its answer comes back, the
// page a person is standing on has to say what is happening — and it has to stop
// saying the last thing the instant it stops being true. A word left standing
// from the life before is the same fault as a blank page: the person reads it,
// believes it, and goes looking for a fault that is not there.
//
// The words themselves are owned elsewhere and each has its own test — the top
// bar's ladder (room.go's [app.roomStateWord]), the phase words (taskphase.go),
// the "nothing yet" line (roomunlanded_test.go). This walks ONE node through all
// of them in one page's life, which is the thing none of those can check: that
// each stretch draws immediately, and that nothing from the stretch before
// survives into it.

// roomLifeSays is the bar and the body of the open page together — the two
// places a person looks to answer "what is happening". IT ASKS THE TOP BAR
// (ISSUE-126): the room header it used to ask is gone, and its state word, its
// clock and its spend are the bar's left cluster now.
func roomLifeSays(a *app) string {
	return plain(a.topBarWord(160)) + "\n" + roomText(a)
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
	if strings.Contains(plain(a.topBarWord(160)), stateWorking.String()) {
		t.Fatalf("the bar still calls a checked node working:\n%s", page)
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
