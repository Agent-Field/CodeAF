package tui3

// ── THE HEADER IS THE INSTRUMENT ────────────────────────────────────────────
//
// A person comes to a task to steer and to check, and the check is one glance
// at a fixed number of cells at the top of the frame. So the header carries the
// judgment and accountability acts whole — the verb, the elapsed, the spend, the
// call count and the live line — and it degrades by what a person is reading it
// for rather than by cutting from the right (room.go's [app.roomHeadWord]).
//
// These pin the three things that can quietly stop being true: that every
// figure is there while it is known, that no figure is there while it is not
// (the emptiness law, per segment), and that a narrow frame loses the tail
// rather than the name.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// headRoom is a room open on a running node that has made calls, spent money
// and been going for a while — the state a person actually glances at.
func headRoom(t *testing.T) *app {
	t.Helper()
	a, _, _ := roomApp(t)
	a.openRoom(7, "Fix the nil-map crash")
	node := a.roomNode()
	if node == nil {
		t.Fatal("the roster has no row for the open room")
	}
	node.cost = 0.42
	base := a.now().Add(-3 * time.Minute)
	node.began = base
	a.room.entries = []entry{
		{kind: entryUser, text: "Fix the nil-map crash", turn: 1, brief: true},
		{kind: entryTool, tool: "read", turn: 1, status: toolOK, began: base, ended: base.Add(time.Second)},
		{kind: entryTool, tool: "edit", turn: 1, status: toolOK, began: base, ended: base.Add(2 * time.Second)},
		{kind: entryTool, tool: "bash", turn: 1, status: toolRunning, began: a.now().Add(-time.Second)},
	}
	a.room.dirty = true
	return a
}

// EVERY ACT THE HEADER OWES IS ON IT. The verb, the clock, the spend, the size
// of what happened, and the one thing on the row that is moving.
func TestTheRoomHeaderCarriesTheVerbTheClockTheSpendTheCountAndTheLiveLine(t *testing.T) {
	head := plain(headRoom(t).roomHeadWord(160))
	for _, want := range []string{"Fix the nil-map crash", "working", "3m", "$0.42", "2 tool calls", "bash"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the header does not say %q: %q", want, head)
		}
	}
}

// A NODE THAT HAS SPENT NOTHING SHOWS NO SPEND, AND ZERO CALLS SHOW NO COUNT.
// The emptiness law, asked per segment: a figure that is zero is a figure
// nobody measured, and `$0.00 · 0 tool calls` is the row spending its scarce
// cells to say nothing twice.
func TestTheHeaderDropsEverySegmentNobodyPublished(t *testing.T) {
	a, _, _ := roomApp(t)
	a.openRoom(7, "Fix the nil-map crash")
	head := plain(a.roomHeadWord(160))
	for _, never := range []string{"$", "0 tool calls", "0 calls"} {
		if strings.Contains(head, never) {
			t.Fatalf("the header drew %q about a node nobody has measured: %q", never, head)
		}
	}
	if !strings.Contains(head, "Fix the nil-map crash") {
		t.Fatalf("the header lost the name it is for: %q", head)
	}
}

// AND A FINISHED NODE HAS NO LIVE LINE. What work that is over is doing right
// now is nothing, and the surface does not spend a segment saying so.
func TestAFinishedNodeHasNoLiveLine(t *testing.T) {
	a := headRoom(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskDone, session.TaskNotice{})})
	if head := plain(a.roomHeadWord(160)); strings.Contains(head, "still working") {
		t.Fatalf("a landed node's header claims it is still going: %q", head)
	}
}

// THE NAME IS WHOLE OR THE ROW IS POINTLESS, AND THE TAIL IS A PREFIX OF
// ITSELF. Narrowing the frame takes facts off the END in rank order; it never
// takes cells from the name and never promotes a later fact into the gap a
// higher-ranked one could not use (rowfit.go's laws 1 and 3).
func TestTheHeaderDegradesByRankAndNeverClipsTheName(t *testing.T) {
	a := headRoom(t)
	wide := plain(a.roomHeadWord(160))
	if !strings.Contains(wide, "$0.42") {
		t.Fatalf("the wide header is missing the spend: %q", wide)
	}
	narrow := plain(a.roomHeadWord(46))
	if !strings.Contains(narrow, "Fix the nil-map crash") {
		t.Fatalf("a narrow frame cut the name: %q", narrow)
	}
	if strings.Contains(narrow, "tool calls") && !strings.Contains(narrow, "$0.42") {
		t.Fatalf("a lower-ranked fact was promoted past the spend: %q", narrow)
	}
	if len(narrow) >= len(wide) {
		t.Fatalf("the header did not degrade at all: %q then %q", wide, narrow)
	}
}
