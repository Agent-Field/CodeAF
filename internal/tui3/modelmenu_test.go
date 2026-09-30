package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE MODEL MENU'S OWN ROW GRAMMAR. The current model is the chosen row of the
// /model overlay, and it used to wear the ladder's selected ground for it — a
// full-width band that read as a highlighted row wherever the cursor happened
// to be. The band is the CURSOR's language on an overlay ("where ↑/↓ has got
// to"), and a mark that outranked it made the chosen row look like a row the
// hand was on when it was not. So the menu's front mark keeps its accent and
// its weight and takes NO ground: the cursor alone lifts a row. These tests
// are the behaviour, on the rows a person actually sees.

// modelMenuLines is the drawn menu, through the app's own door.
func modelMenuLines(a *app) []string { return a.overlayRows(a.width, a.overlayHeight()) }

// modelMenuRow returns the drawn line that carries one model's row.
func modelMenuRow(t *testing.T, a *app, id string) string {
	t.Helper()
	for _, line := range modelMenuLines(a) {
		if strings.Contains(line, id) {
			return line
		}
	}
	t.Fatalf("%s is not on the drawn model menu:\n%s", id, plain(strings.Join(modelMenuLines(a), "\n")))
	return ""
}

// groundsAreDrawn guards the assertions below against a palette that paints no
// background at all: on the NoColor tier there is no ground to take off, so
// "no ground" would be true of every row and prove nothing.
func groundsAreDrawn(a *app) bool { return a.pal.profile != tokens.NoColor }

func TestTheCurrentModelKeepsBoldAccentButNoGround(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	// The cursor opens on the list's first row ([picker.cursorToFirst]), and
	// the marked row — the model in use — sits further down with no cursor on
	// it: the two facts on the frame are apart from the first keystroke.
	marked := modelMenuRow(t, a, "openai/gpt-4.1-mini")
	// BOLD BLUE, RETAINED AND NOW EXPLICIT: the label keeps the accent and the
	// weight it read at under the band.
	want := a.pal.bold(a.pal.accent("openai/gpt-4.1-mini"))
	if !strings.Contains(marked, want) {
		t.Fatalf("the current model's row lost its bold accent; want %q in:\n%s", want, plain(marked))
	}
	// AND NO GROUND. A background run on the row would be the highlighted row
	// the menu must not wear.
	if groundsAreDrawn(a) && strings.Contains(marked, "\x1b[48;") {
		t.Fatalf("the current model's row wears a ground:\n%q", marked)
	}

	// AND THE CURSOR'S HIGHLIGHT IS ON THE SCREEN WHERE IT OPENED: on the
	// list's first row, and on no other.
	if a.pick.cursor != 0 {
		t.Fatalf("the menu opened with the cursor on row %d, want the first", a.pick.cursor)
	}
	var grounded []string
	for _, line := range modelMenuLines(a) {
		if strings.Contains(line, "\x1b[48;") {
			grounded = append(grounded, line)
		}
	}
	if groundsAreDrawn(a) {
		if len(grounded) != 1 || !strings.Contains(grounded[0], "anthropic/claude-gpt-echo") {
			t.Fatalf("the opening frame carries the cursor's ground on %d rows, want the first row alone:\n%s",
				len(grounded), plain(strings.Join(modelMenuLines(a), "\n")))
		}
	}

	// AND THE CURSOR ON THE MARKED ROW STILL LIFTS IT — the mark must not have
	// made the row unhighlightable, only unhighlighted on its own. One down
	// from the first row reaches the model in use.
	drive(t, a, key("down"))
	both := modelMenuRow(t, a, "openai/gpt-4.1-mini")
	if groundsAreDrawn(a) && !strings.Contains(both, "\x1b[48;") {
		t.Fatalf("the cursor on the current model's row lifts nothing:\n%q", both)
	}
	if !strings.Contains(both, want) {
		t.Fatalf("the marked row under the cursor lost its bold accent:\n%q", plain(both))
	}
}

func TestTheModelPickerOwnsTheWheelAndClampsAtBothEnds(t *testing.T) {
	// A TRANSCRIPT THAT CAN SCROLL, so "the transcript did not move" is a real
	// assertion and not the accident of a short conversation.
	a := benchApp(20)
	a.models = func() []Model { return pickerCatalog }
	a.model = "openai/gpt-4.1-mini"
	a.frame()
	// Park the transcript away from the live edge first, with the picker shut:
	// a notch up on the bare frame is the gesture that used to leak past the
	// open menu.
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelUp})
	if a.offset == 0 {
		t.Fatal("the fixture transcript is too short to scroll")
	}
	a.openPicker()
	offset := a.offset

	last := len(a.pick.list) - 1
	// Far more notches than the list has rows: the walk must stop at the end
	// and never hand the wheel to the conversation behind the menu.
	for i := 0; i < 20; i++ {
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelDown})
	}
	if a.pick.cursor != last {
		t.Fatalf("the cursor rests on %d of %d rows, want the last", a.pick.cursor, last)
	}
	if a.offset != offset {
		t.Fatalf("the wheel moved the transcript behind the menu: %d → %d", offset, a.offset)
	}
	if a.pick.top > last {
		t.Fatalf("the list window ran past the end: top %d of %d rows", a.pick.top, last)
	}
	// The list really did move for the wheel — a clamp on a list the wheel
	// cannot move would prove nothing.
	if a.pick.cursor == 0 && a.pick.top == 0 {
		t.Fatal("the wheel never moved the list, so the clamp proves nothing")
	}

	for i := 0; i < 20; i++ {
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelUp})
	}
	if a.pick.cursor != 0 {
		t.Fatalf("the cursor rests on %d, want the first row", a.pick.cursor)
	}
	if a.pick.top != 0 {
		t.Fatalf("the list window rests at top %d, want 0", a.pick.top)
	}
	if a.offset != offset {
		t.Fatalf("the wheel moved the transcript behind the menu going up: %d → %d", offset, a.offset)
	}
}

func TestThePickerCursorStopsAtTheEndsOfTheList(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	last := len(a.pick.list) - 1

	for i := 0; i < 30; i++ {
		drive(t, a, key("down"))
	}
	if a.pick.cursor != last {
		t.Fatalf("thirty downs left the cursor on %d of %d rows, want the last", a.pick.cursor, last)
	}
	// AND THE WINDOW STAYS INSIDE THE LIST: a page key is a bigger step, and a
	// window that followed it past the last row would draw nothing at all.
	drive(t, a, key("pgdown"))
	if a.pick.cursor != last {
		t.Fatalf("a page down past the end moved the cursor to %d, want the last", a.pick.cursor)
	}
	if a.pick.top < 0 || a.pick.top > last {
		t.Fatalf("the list window left the list: top %d of %d rows", a.pick.top, last)
	}

	for i := 0; i < 30; i++ {
		drive(t, a, key("up"))
	}
	if a.pick.cursor != 0 {
		t.Fatalf("thirty ups left the cursor on %d, want the first row", a.pick.cursor)
	}
	drive(t, a, key("pgup"))
	if a.pick.cursor != 0 {
		t.Fatalf("a page up past the top moved the cursor to %d, want the first", a.pick.cursor)
	}
	// The walk is honest both ways: one down from the top reaches the second
	// row, so the clamps above are not a list that lost its motion.
	drive(t, a, key("down"))
	if a.pick.cursor != 1 {
		t.Fatalf("one down from the top rests on %d, want the second row", a.pick.cursor)
	}
}
