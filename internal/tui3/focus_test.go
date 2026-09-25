package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// headRow is the room's pinned focus header, which is the row under the tab
// strip wherever there is one (chattabs.go's [app.roomHeadRow]).
func headRow(a *app) string {
	rows := strings.Split(frame(a), "\n")
	at := a.roomHeadRow()
	if at >= len(rows) {
		return ""
	}
	return rows[at]
}

// headFactsRow is the row under it: what the work is doing, and the `Stop`
// (room.go's [app.roomFactsLine]).
func headFactsRow(a *app) string {
	rows := strings.Split(frame(a), "\n")
	at := a.roomFactsRow()
	if at >= len(rows) {
		return ""
	}
	return rows[at]
}

// headPanel is both of a room's own pinned rows, which is what a test asking
// whether "the header" says something has to read: the header is two rows now,
// ancestry on one and telemetry on the other.
func headPanel(a *app) string { return headRow(a) + "\n" + headFactsRow(a) }

// tabsRowOf reads the tab labels inside the header's optional vertical padding.
func tabsRowOf(a *app) string {
	rows := strings.Split(frame(a), "\n")
	at := tabStripRow
	if at >= len(rows) {
		return ""
	}
	return rows[at]
}

// THE HEADER SAYS WHERE YOU ARE AND WHAT IS HAPPENING THERE. It is pinned above
// the page, it wears the accent, and it names the way out.
func TestARoomPinsAFocusHeader(t *testing.T) {
	a, _, advance := roomApp(t)
	a.openingPrompt = "work on the parser"
	clickRail(t, a, 0)
	advance(2*time.Minute + 12*time.Second)
	a.touch()

	head := plain(headPanel(a))
	// The clock is the RAIL's spelling of an age ("2m 12s"), because the rail is
	// where a person already reads this node's clock.
	for _, want := range []string{a.chatCrumbWord() + " ▸ Fix the nil-map", "working", "2m 12s", roomBackWord} {
		if !strings.Contains(head, want) {
			t.Fatalf("the focus header is missing %q:\n%s", want, head)
		}
	}
	if !strings.Contains(headRow(a), sgr256(hueInk)) {
		t.Fatalf("the current task title has no primary ink:\n%q", headRow(a))
	}
	// PINNED: the page scrolls under it and its header row stays in place.
	a.roomScroll(-3)
	if got := plain(headRow(a)); !strings.Contains(got, "Fix the nil-map") {
		t.Fatalf("the header scrolled away with the page:\n%s", got)
	}
	// AND IT COSTS THE PAGE ITS ROW, in the one number every geometric question
	// resolves through — a header the scrolling did not know about would push
	// the room's last row under the input box. The tab strip and the room's
	// trail and facts are pinned here, under the whole head, with their
	// breathing room. The task strip stands down wherever this header is drawn.
	wantHead := a.roomHeadRow() + a.roomHeadHeight(a.width)
	if a.headHeight() != wantHead || a.stripHeight() != 0 || a.bodyTop() != wantHead {
		t.Fatalf("the pinned rows are drawn but not budgeted: head=%d strip=%d top=%d",
			a.headHeight(), a.stripHeight(), a.bodyTop())
	}
	// AND LEAVING TAKES THE HEADER ROW AWAY ENTIRELY, leaving the tab strip that
	// was over it: a conversation has no trail and no way out of itself, and the
	// row that said so is not drawn (chattabs.go). What must NOT survive is the
	// room's — the task's name, its state and its exit.
	drive(t, a, key("esc"))
	head = plain(tabsRowOf(a))
	// What is left over a conversation is the places' head — the pulse, the
	// strip, the rule and the blank — which is the seam between the head and the
	// transcript (head.go).
	if a.headHeight() != chatHeadRows || !strings.Contains(head, a.chatDisplayName()) {
		t.Fatalf("the conversation's strip is %d rows and reads:\n%q", a.headHeight(), head)
	}
	for _, gone := range []string{"Fix the nil-map", roomBackWord, roomCrumbSep} {
		if strings.Contains(head, gone) {
			t.Fatalf("the room's header outlived the room — it still says %q:\n%s", gone, head)
		}
	}
}

// The header carries the node's own spend, and nothing at all when nobody has
// published one: a room is not a place to invent a figure.
func TestTheFocusHeaderCarriesTheNodesSpend(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	if strings.Contains(plain(headPanel(a)), "$") {
		t.Fatalf("an unpriced node drew a cost:\n%s", plain(headPanel(a)))
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{CostUSD: 0.04})})
	if got := plain(headFactsRow(a)); !strings.Contains(got, "$0.04") {
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
	_ = frame(a)
	x := a.roomBackSpan.from + 1
	if !a.roomBackSpan.pressable() {
		t.Fatal("the focus header drew no Back target")
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: a.roomHeadRow(), Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: a.roomHeadRow(), Button: tea.MouseLeft})
	if a.roomOpen() {
		t.Fatal("a press on the focus header's Back target did not return to the conversation")
	}
}

// The rail is still the door between rooms, and it is still under the pointer
// where it is drawn — one row lower, because the header took the first one.
func TestTheRailIsStillTheDoorUnderTheHeader(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Mix the audio",
		session.TaskRunning, session.TaskNotice{})})
	// TWO RUNNING TASKS, so the column is newest first under one heading:
	// node 9, then node 7. Which is which is not what this test owns (it owns
	// the OFFSET), but naming them in the roster's own order is what keeps it
	// about that.
	clickRail(t, a, 0)
	if a.room == nil || a.room.id != 9 {
		t.Fatalf("the first rail row did not open node 9: %+v", a.room)
	}
	// Node 7 is drawn under node 9, and the header is above both: a click on the
	// rail's second row has to land a row further down the screen than it did
	// before the room opened.
	clickRail(t, a, 1)
	if a.room == nil || a.room.id != 7 {
		t.Fatalf("the second rail row did not open node 7 through the header: %+v", a.room)
	}
}

// PRESSING THE MODEL'S NAME OPENS THE PICKER, and the sentence being written
// survives the whole round trip: opening it, and switching with it.
//
// THE NAME IS ON THE SEAM from 2026-09-09 — the rule above the box — and so is
// its door (foot.go's [app.legendModelPress]). It was the left of the status
// row until then, where a long title pushed the numbers off the frame.
func TestPressingTheModelNameOpensThePickerAndKeepsTheDraft(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := newTestApp(agent)
	a.width, a.height = 120, 24
	a.input.setText("half a sentence")
	a.touch()

	// The name's own columns, as the line that drew it recorded them.
	_ = frame(a)
	if !a.seamModelSpan.pressable() {
		t.Fatal("the seam recorded no columns for the model")
	}
	x, y := a.seamModelSpan.from+1, markedRowY(a, chromeLegend, 0)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})

	if !a.pick.open {
		t.Fatalf("pressing the model at x=%d did not open the picker", x)
	}
	if a.input.String() != "half a sentence" {
		t.Fatalf("opening the picker took the draft: %q", a.input.String())
	}

	// And switching keeps it too: the box is suspended, never emptied. Enter
	// chooses and leaves the list up, so esc is what puts the draft back in
	// front of the keyboard ([app.pickerKey]).
	drive(t, a, key("enter"), key("esc"))
	if a.pick.open {
		t.Fatal("esc did not close the picker")
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
