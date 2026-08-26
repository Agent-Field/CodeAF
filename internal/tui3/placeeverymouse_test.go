package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ── THE POINTER, ASKED OF ALL SEVEN ─────────────────────────────────────────
//
// placemouse_test.go pins the three gestures on the places that had none;
// placeeveryone_test.go walks every room with the keyboard. Neither one walked
// every room with the POINTER, which is how the built binary came to have a tab
// bar that answered a press on four places and ignored it on the other three.
// This file asks the two pointer questions of all seven, over the same labs.

// A TAB WORD IS A DOOR FROM WHEREVER YOU ARE STANDING. The bar is the router's
// own row and it is drawn on every place identically, so a press on it that
// meant something different depending on which room you happened to be in would
// be the one row of the frame a person cannot trust.
func TestClickingATabWordWorksFromEveryPlace(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			// The frame is drawn first, because a press resolves against the bar
			// that was actually painted and never against a second guess at it.
			placeFrameText(a)
			target := pageSettings
			if place.id == pageSettings {
				target = pageSpend
			}
			x, ok := placeTabColumnOf(a, target)
			if !ok {
				t.Fatalf("the %s tab is not on the bar the %s place drew", target.word(), place.id.word())
			}
			if a.tabRow != placeTabRow {
				t.Fatalf("the %s place drew its tab bar on row %d", place.id.word(), a.tabRow)
			}
			drive(t, a, tea.MouseClickMsg{X: x, Y: a.tabRow, Button: tea.MouseLeft})
			if a.page != target {
				t.Fatalf("pressing the %s tab on the %s place left the router on %q",
					target.word(), place.id.word(), a.page.word())
			}
		})
	}
}

// THE WHEEL IS `↑↓` UNDER THE POINTER, on every place and by the same distance.
// A wheel that moved a different number of rows per room — or none at all in
// some of them — would be one gesture with seven meanings, which is what the
// owner met when they turned it over the built binary.
func TestTheWheelMovesEveryPlacesCursorLikeTheArrows(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			// ONE LAB WALKED WITH THE ARROWS AND ANOTHER WITH THE WHEEL, because
			// what is being compared is the two gestures' effect on the same
			// starting cursor over the same body.
			byKey, byWheel := place.open(t), place.open(t)
			// BOTH START AT THE TOP OF THE LIST. A lab that opens with its cursor
			// already on the last row proves nothing about a gesture that walks
			// down, and home's own lab opens exactly there — with its fold held
			// open, the cursor sits on the fold line it was opened from.
			placeWalkToTop(t, byKey)
			placeWalkToTop(t, byWheel)
			start := place.cursor(byKey)
			for i := 0; i < placeWheelRows; i++ {
				drive(t, byKey, key("down"))
			}
			want := place.cursor(byKey)
			if want == start {
				t.Skipf("the %s place's body is too short for `↓` to move at all", place.id.word())
			}
			drive(t, byWheel, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
			if got := place.cursor(byWheel); got != want {
				t.Fatalf("one turn of the wheel on the %s place moved the cursor to %d and %d presses of `↓` moved it to %d",
					place.id.word(), got, placeWheelRows, want)
			}
			// AND BACK THE OTHER WAY, because a wheel that only went down would
			// be a list a person can fall off the bottom of.
			drive(t, byWheel, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelUp})
			if got := place.cursor(byWheel); got != start {
				t.Fatalf("the wheel back up the %s place left the cursor on %d rather than %d",
					place.id.word(), got, start)
			}
		})
	}
}

// placeWalkToTop walks a place's cursor to the first row it will stop on, with
// the arrow a person would use. It is more presses than any of these labs has
// rows, because what is wanted is the clamp and not a count.
func placeWalkToTop(t *testing.T, a *app) {
	t.Helper()
	placeFrameText(a)
	for i := 0; i < 60; i++ {
		drive(t, a, key("up"))
	}
	placeFrameText(a)
}
