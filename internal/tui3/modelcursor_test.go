package tui3

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE MODEL CURSOR IS A DRAWN ROW, NOT AN INDEX. The person's report
// (2026-09-30): the cursor disappeared once the list bottomed out and they
// kept navigating past the last item, and the highlight was not visible at
// all when the menu first opened. Both had one root: the scroll window was
// counted in list rows while the drawing spends extra lines on a group's
// heading, the machines' heading and a why line — so the cursor could sit
// inside the logical window and still fall past the rendered bottom edge.
// These tests assert the RENDERED frame, because an index inside a window
// that never drew the row is exactly the bug.

// groupedCatalog is a list of two services, five models each, with the group
// headings the real list draws from connected providers. The headings are
// what pushed the cursor's row out of the rendered frame, so the fixture
// carries them even though [pickerApp]'s single-source ladder would not.
var groupedCatalog = func() []Model {
	out := make([]Model, 0, 10)
	for _, group := range []struct {
		name string
		head string
	}{
		{"alpha", "alpha · 5"},
		{"beta", "beta · 5"},
	} {
		for i := 1; i <= 5; i++ {
			out = append(out, Model{
				ID:            fmt.Sprintf("%s/model-%d", group.name, i),
				ContextLength: 200_000,
				Group:         group.name,
				GroupHead:     group.head,
				GroupOrder:    len(out),
			})
		}
	}
	return out
}()

// drawnCursorLine returns the drawn menu line that wears the cursor's ground,
// and how many of them there are: exactly one row is the cursor's, so exactly
// one drawn line may carry the ground the cursor paints.
func drawnCursorLines(a *app) []string {
	var out []string
	for _, line := range modelMenuLines(a) {
		if strings.Contains(line, "\x1b[48;") {
			out = append(out, line)
		}
	}
	return out
}

func TestTheMenuOpensWithTheCursorOnTheFirstRowAndVisible(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "beta/model-3"}, groupedCatalog)
	typeLine(t, a, "/model")

	// THE CURSOR RESTS ON THE FIRST SELECTABLE ROW — the list's first model,
	// not the group's heading above it and not the model in use.
	if a.pick.cursor != 0 {
		t.Fatalf("the menu opened with the cursor on row %d, want the first", a.pick.cursor)
	}
	if chosen, _ := a.pick.choice(); chosen.ID != "alpha/model-1" {
		t.Fatalf("the menu opened on %q, want the first row", chosen.ID)
	}
	// AND THE HIGHLIGHT IS ON THE SCREEN. One ground, on the first row.
	lines := drawnCursorLines(a)
	if len(lines) != 1 {
		t.Fatalf("the first frame drew %d grounded rows, want exactly the cursor's:\n%s",
			len(lines), plain(strings.Join(modelMenuLines(a), "\n")))
	}
	if !strings.Contains(lines[0], "alpha/model-1") {
		t.Fatalf("the cursor's ground is not on the first row:\n%q", plain(lines[0]))
	}
	// AND THE MODEL IN USE KEEPS ITS MARK — bold accent, no ground — even
	// though the cursor is not on it.
	marked := modelMenuRow(t, a, "beta/model-3")
	want := a.pal.bold(a.pal.accent("beta/model-3"))
	if !strings.Contains(marked, want) {
		t.Fatalf("the current model's row lost its bold accent; want %q in:\n%s", want, plain(marked))
	}
	if groundsAreDrawn(a) && strings.Contains(marked, "\x1b[48;") {
		t.Fatalf("the current model's row wears a ground:\n%q", marked)
	}
}

func TestTheCursorStaysVisibleAtTheBottomOfTheList(t *testing.T) {
	// A TRANSCRIPT THAT CAN SCROLL, so "the conversation did not move" is a real
	// assertion and not the accident of a short one.
	a := benchApp(20)
	a.models = func() []Model { return groupedCatalog }
	a.model = "beta/model-3"
	a.frame()
	// Park the transcript away from the live edge first, with the picker shut,
	// the way the wheel's own test does — an offset that never left 0 would
	// make the no-scroll assertion below read nothing.
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelUp})
	if a.offset == 0 {
		t.Fatal("the fixture transcript is too short to scroll")
	}
	typeLine(t, a, "/model")
	last := len(a.pick.list) - 1
	offset := a.offset

	// FAR MORE STEPS THAN THE LIST HAS ROWS, across every navigation there is:
	// repeated arrows, page keys and wheel notches all bottom out on the same
	// visible row.
	for i := 0; i < 30; i++ {
		drive(t, a, key("down"))
	}
	drive(t, a, key("pgdown"))
	for i := 0; i < 10; i++ {
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelDown})
	}
	if a.pick.cursor != last {
		t.Fatalf("the cursor rests on %d of %d rows, want the last", a.pick.cursor, last)
	}
	// AND THE CONVERSATION BEHIND THE MENU DID NOT MOVE — not for the arrows,
	// not for the page key, not for the wheel.
	if a.offset != offset {
		t.Fatalf("navigation at the bottom moved the transcript: %d → %d", offset, a.offset)
	}

	// AND THE CURSOR'S ROW IS ON THE SCREEN. The window bottoms out with the
	// group headings inside it; the cursor is the last drawn row.
	lines := drawnCursorLines(a)
	if len(lines) != 1 {
		t.Fatalf("at the bottom the frame drew %d grounded rows, want exactly the cursor's:\n%s",
			len(lines), plain(strings.Join(modelMenuLines(a), "\n")))
	}
	lastID := a.pick.all[a.pick.hits[a.pick.list[last].hit]].ID
	if !strings.Contains(lines[0], lastID) {
		t.Fatalf("the cursor's ground is not on the last row (%s):\n%q", lastID, plain(lines[0]))
	}
	// AND THE WINDOW SHOWS THE END OF THE LIST, not a page that stops short:
	// the door rides the list's end, so it is on the screen too.
	shown := plain(strings.Join(modelMenuLines(a), "\n"))
	if !strings.Contains(shown, addProviderRowWord) {
		t.Fatalf("the list bottomed out above its own end:\n%s", shown)
	}
}
