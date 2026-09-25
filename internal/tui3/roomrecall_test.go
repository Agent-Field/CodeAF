package tui3

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/history"
)

// THE ROOM'S BOX IS THE CONVERSATION'S BOX (room.go's [app.roomKey]).
//
// A room used to be the one place on this surface where a sentence somebody had
// already typed could not be brought back: ↑ over an empty box scrolled the page
// by one row and there was no way to reach the history at all. These tests pin
// the grammar that replaced it — ↑ walks what you typed, ↓ walks back to your own
// draft, a steered line joins that history, and the page still scrolls when there
// is nothing to walk.

// roomRecallApp is [roomApp] with a history store under it, standing in the room
// of the node that helper leaves running.
func roomRecallApp(t *testing.T, entries ...history.Entry) (*app, *roomFake) {
	t.Helper()
	a, agent, _ := roomApp(t)
	store := history.New(filepath.Join(t.TempDir(), "history.jsonl"))
	t.Cleanup(func() { _ = store.Close() })
	for _, entry := range entries {
		store.Append(entry.Text, entry.Cwd)
	}
	a.history, a.workspace = store, "/tmp/lab"
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatal("the rail click did not open the node's room")
	}
	return a, agent
}

// ↑ INSIDE A ROOM WALKS YOUR OWN HISTORY, which is what it does in the
// conversation — and ↓ walks back out of the walk to the draft it was holding.
func TestUpInsideARoomWalksTheHistoryAndDownComesBack(t *testing.T) {
	a, _ := roomRecallApp(t,
		history.Entry{Text: "older here", Cwd: "/tmp/lab"},
		history.Entry{Text: "newer here", Cwd: "/tmp/lab"},
	)
	typeInto(t, a, "half a sentence")

	for _, want := range []string{"newer here", "older here"} {
		drive(t, a, key("up"))
		if a.input.String() != want {
			t.Fatalf("↑ in a room shows %q, want %q", a.input.String(), want)
		}
	}
	if !a.roomOpen() {
		t.Fatal("walking the history left the room")
	}
	drive(t, a, key("down"))
	if a.input.String() != "newer here" {
		t.Fatalf("↓ shows %q", a.input.String())
	}
	drive(t, a, key("down"))
	if a.recalling() || a.input.String() != "half a sentence" {
		t.Fatalf("↓ did not put the draft back: %q (recalling=%v)",
			a.input.String(), a.recalling())
	}
}

// AND esc DURING THAT WALK IS THE WALK'S, NOT THE ROOM'S: the draft comes back
// and the page is still on screen. The room is one keystroke behind it.
func TestEscDuringARoomsRecallKeepsTheRoomAndPutsTheDraftBack(t *testing.T) {
	a, _ := roomRecallApp(t, history.Entry{Text: "an old prompt", Cwd: "/tmp/lab"})
	typeInto(t, a, "mine")

	drive(t, a, key("up"))
	if a.input.String() != "an old prompt" {
		t.Fatalf("↑ recalled %q", a.input.String())
	}
	// The legend promises what the NEXT esc does, and for these few keystrokes
	// that is not "main" (render.go's [app.legendLeft]).
	if legend := plain(a.legend(a.width)); !strings.Contains(legend, roomLegendRecallWord) {
		t.Fatalf("the legend still promises the room's own esc:\n%s", legend)
	}
	drive(t, a, key("esc"))
	if !a.roomOpen() {
		t.Fatal("esc during a recall walk closed the room")
	}
	if a.recalling() || a.input.String() != "mine" {
		t.Fatalf("esc left %q (recalling=%v)", a.input.String(), a.recalling())
	}
	// And the next esc is the room's again, exactly as the legend now says.
	if legend := plain(a.legend(a.width)); !strings.Contains(legend, roomLegendWord) {
		t.Fatalf("the legend did not go back to the way out:\n%s", legend)
	}
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("the second esc did not leave the room")
	}
}

// A LINE STEERED AT A NODE IS A LINE YOU TYPED, so ↑ brings it back
// (room.go's [app.steer]).
func TestASteeredLineIsRememberedAndComesBackWithUp(t *testing.T) {
	a, agent := roomRecallApp(t)
	typeInto(t, a, "look at the loader instead")
	drive(t, a, key("enter"))

	if len(agent.steered) != 1 || agent.steered[0].text != "look at the loader instead" {
		t.Fatalf("the node was steered with %+v", agent.steered)
	}
	if a.input.String() != "" {
		t.Fatalf("the box kept %q after a steer that landed", a.input.String())
	}
	drive(t, a, key("up"))
	if a.input.String() != "look at the loader instead" {
		t.Fatalf("↑ after a steer recalled %q", a.input.String())
	}
}

// AND A LINE THE ENGINE REFUSED IS NOT REMEMBERED: the guard keeps the words in
// the box, and a recall list holding sentences that went nowhere would be a
// history of things that did not happen.
func TestARefusedSteerIsNotRemembered(t *testing.T) {
	a, agent := roomRecallApp(t)
	agent.steerErr = errString("task 7 is done, not running")
	typeInto(t, a, "one more thing")
	drive(t, a, key("enter"))

	if !a.guarding() {
		t.Fatal("a refused steer did not raise the guard")
	}
	if a.input.String() != "one more thing" {
		t.Fatalf("the guard took the words out of the box: %q", a.input.String())
	}
	if got := a.history.RecentFor("/tmp/lab", 5); len(got) != 0 {
		t.Fatalf("a refused steer was remembered: %+v", got)
	}
}

// THE PAGE STILL SCROLLS WHERE THERE IS NOTHING TO WALK. ↑ is the history's
// first and the page's second, which is input.go's own order — so a session with
// an empty history keeps the one-row scroll the room has always had.
func TestUpScrollsTheRoomWhenThereIsNoHistory(t *testing.T) {
	a, _ := roomRecallApp(t)
	// A page long enough to have somewhere to scroll to.
	for i := range 40 {
		a.roomSaid(entry{kind: entryAssistant, text: "line " + itoa(i)})
	}
	a.room.stick = true
	before := a.roomOffsetFor(len(a.roomRows(a.bodyWidth())), a.viewHeight())

	drive(t, a, key("up"))
	if a.recalling() {
		t.Fatal("an empty history started a walk")
	}
	after := a.roomOffsetFor(len(a.roomRows(a.bodyWidth())), a.viewHeight())
	if after != before-1 {
		t.Fatalf("↑ moved the page from %d to %d", before, after)
	}
}

// THE HINT SLOT MUST NOT SAY "esc interrupt" IN A ROOM, because esc in here
// leaves the page and interrupts nothing (room.go's [app.roomHint]).
func TestTheHintSlotInARoomNeverPromisesAnInterrupt(t *testing.T) {
	a, _ := roomRecallApp(t)
	a.state = stateWorking

	if hint := a.hintWord(); hint == "esc interrupt" {
		t.Fatal("the hint slot promised an interrupt from inside a room")
	}
	// What it says instead is the key that actually ends the work in here.
	if hint := a.hintWord(); hint != roomStopHint {
		t.Fatalf("the room's hint reads %q, want %q", hint, roomStopHint)
	}
	drive(t, a, key("esc"))
	if hint := a.hintWord(); hint != "esc interrupt" {
		t.Fatalf("back in the conversation the hint reads %q", hint)
	}
}

// errString is a test error whose text is the engine's own sentence.
type errString string

func (e errString) Error() string { return string(e) }
