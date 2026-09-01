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
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone,
		Task: &session.TaskNotice{ID: 7}, Usage: session.Usage{CostUSD: 0.04}}})
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
