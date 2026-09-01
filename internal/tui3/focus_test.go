package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// headRow is the frame's first row: the top bar, when there is one.
func headRow(a *app) string {
	rows := strings.Split(frame(a), "\n")
	if len(rows) == 0 {
		return ""
	}
	return rows[0]
}

// THE TOP BAR SAYS WHERE YOU ARE AND WHAT IS HAPPENING THERE. It is pinned above
// the page, the room's cluster wears the accent, and it names the way out.
func TestARoomPinsAFocusHeader(t *testing.T) {
	a, _, advance := roomApp(t)
	clickRail(t, a, 0)
	advance(2*time.Minute + 12*time.Second)
	a.touch()

	head := plain(headRow(a))
	// The clock is the RAIL's spelling of an age ("2m 12s"), because the rail is
	// where a person already reads this node's clock.
	for _, want := range []string{"Fix the nil-map", "working", "2m 12s", roomBackWord} {
		if !strings.Contains(head, want) {
			t.Fatalf("the top bar is missing %q:\n%s", want, head)
		}
	}
	if !strings.Contains(headRow(a), sgr256(hueAccent)) {
		t.Fatalf("the room's bar is not in the accent:\n%q", headRow(a))
	}
	// PINNED: the page scrolls under it and it stays on the first row.
	a.roomScroll(-3)
	if got := plain(headRow(a)); !strings.Contains(got, "Fix the nil-map") {
		t.Fatalf("the top bar scrolled away with the page:\n%s", got)
	}
	// AND IT COSTS THE PAGE ITS ROWS, in the one number every geometric question
	// resolves through — a bar the scrolling did not know about would push the
	// room's last row under the input box. The bar is two rows — itself and its
	// rule — and it is the ONLY pinned thing here: the roster is standing on a
	// frame this wide (view.go's [app.topHeight]).
	if a.headHeight() != 2 || a.bodyTop() != 2 {
		t.Fatalf("the pinned rows are drawn but not budgeted: head=%d top=%d",
			a.headHeight(), a.bodyTop())
	}
	// ESC CLIMBS ONE CRUMB LEVEL, and with no parent above this room that is the
	// conversation — but the bar is the CONVERSATION'S head now, not the room's,
	// so it stays: what leaves the frame is the room's facts on it.
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc left the room open")
	}
	if a.headHeight() != 2 {
		t.Fatal("the top bar went with the room")
	}
	if got := plain(headRow(a)); strings.Contains(got, "Fix the nil-map") {
		t.Fatalf("the closed room's title is still on the bar:\n%s", got)
	}
}

// The header carries the node's own spend, and nothing at all when nobody has
// published one: a room is not a place to invent a figure.
func TestTheFocusHeaderCarriesTheNodesSpend(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	if strings.Contains(plain(headRow(a)), "$") {
		t.Fatalf("an unpriced node drew a cost:\n%s", plain(headRow(a)))
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{CostUSD: 0.04})})
	if got := plain(headRow(a)); !strings.Contains(got, "$0.04") {
		t.Fatalf("the node's spend is not on the header:\n%s", got)
	}
}

// ← LEAVES A ROOM AND DOES NOTHING AT ALL while there is a sentence in the box,
// where it is the caret's.
//
// The room briefly counted two ← of its own to leave. The arrow grammar it met
// says the same thing with one more level in it — one ← steps back a level, two
// inside [navDoubleTap] go home — so what a single ← does is covered by
// [TestTheArrowsMoveBetweenTheConversationAndTheWork] and what two do by
// [TestATwoTapLeftGoesHome]. What is left here, and is this test's own, is the
// guard: no number of ← may take a person out of a page they are typing on.
func TestLeftDoesNothingToARoomAPersonIsTypingIn(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)
	a.input.setText("keep going")
	drive(t, a, key("left"), key("left"), key("left"))
	if !a.roomOpen() {
		t.Fatal("← threw a person out of a room while they were typing")
	}
	if a.input.String() != "keep going" {
		t.Fatalf("the box lost the sentence: %q", a.input.String())
	}
}

// A PRESS ON THE HEADER IS A PRESS ON THE WAY OUT. The row names esc and ←; a
// row that named the exits and did nothing when pressed would be the one dead
// cell on the page.
func TestPressingTheFocusHeaderLeavesTheRoom(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	drive(t, a, tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	if a.roomOpen() {
		t.Fatal("a press on the focus header did not return to the conversation")
	}
}

// The rail is still the door between rooms, and it is still under the pointer
// where it is drawn — one row lower, because the header took the first one.
func TestTheRailIsStillTheDoorUnderTheHeader(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Mix the audio",
		session.TaskRunning, session.TaskNotice{})})
	// TWO FAMILIES OF ONE, EQUALLY URGENT, so the column is in the order the
	// session admitted them (task.go): node 7 first, node 9 under it. Which is
	// which is not what this test owns — it owns the OFFSET — but naming them in
	// the roster's own order is what keeps it about that.
	clickRail(t, a, 0)
	if a.room == nil || a.room.id != 7 {
		t.Fatalf("the first rail row did not open node 7: %+v", a.room)
	}
	// Node 9 is drawn under node 7, and the header is above both: a click on the
	// rail's second row has to land a row further down the screen than it did
	// before the room opened.
	clickRail(t, a, 1)
	if a.room == nil || a.room.id != 9 {
		t.Fatalf("the second rail row did not open node 9 through the header: %+v", a.room)
	}
}

// PRESSING THE MODEL'S NAME OPENS THE PICKER, and the sentence being written
// survives the whole round trip: opening it, and switching with it.
func TestPressingTheModelNameOpensThePickerAndKeepsTheDraft(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := newTestApp(agent)
	a.width, a.height = 120, 24
	a.input.setText("half a sentence")
	a.touch()

	// The name's own columns, as the row that drew it recorded them.
	_ = frame(a)
	if !a.modelSpan.pressable() {
		t.Fatal("the status row recorded no columns for the model")
	}
	x, y := a.modelSpan.from+1, a.height-1
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})

	if !a.pick.open {
		t.Fatalf("pressing the model at x=%d did not open the picker", x)
	}
	if a.input.String() != "half a sentence" {
		t.Fatalf("opening the picker took the draft: %q", a.input.String())
	}

	// And switching keeps it too: the box is suspended, never emptied.
	drive(t, a, key("enter"))
	if a.pick.open {
		t.Fatal("enter did not close the picker")
	}
	if a.input.String() != "half a sentence" {
		t.Fatalf("switching the model took the draft: %q", a.input.String())
	}
}

// A press on the telemetry half of the same row is NOT the model's: those are
// figures, not controls.
func TestPressingTheTelemetryDoesNotOpenThePicker(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := newTestApp(agent)
	a.width, a.height = 120, 24
	a.touch()
	_ = frame(a)

	drive(t, a, tea.MouseClickMsg{X: a.width - 2, Y: a.height - 1, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.width - 2, Y: a.height - 1, Button: tea.MouseLeft})
	if a.pick.open {
		t.Fatal("a press on the telemetry opened the model picker")
	}
}
