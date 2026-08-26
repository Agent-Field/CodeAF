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

// ── the pointer previews ────────────────────────────────────────────────────

// THE POINTER PREVIEWS AND THE CURSOR SELECTS, AND THE MOTION MESSAGE IS WHAT
// CARRIES IT. homehover_test.go proves home's card follows the pointer by
// calling [app.homeHover] itself, which is the right test of the law and no
// test at all of the road: the owner drove the built binary and reported that
// nothing previewed under the pointer, and a motion message swallowed on its
// way through [app.Update] would look exactly like that with every hover test
// in this package still green.
func TestAMotionMessageMovesHomesCardWithoutMovingTheCursor(t *testing.T) {
	a, _, _ := hoverLab(t)
	if a.width < homeCardMin {
		t.Fatalf("the hover lab opens on %d columns and there is no card under %d", a.width, homeCardMin)
	}
	cursor := a.home.cursor
	before := homeCardTitle(t, a)
	at := homeLineOfKind(t, a, homeSession, "alpha")
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: homeLineY(t, a, at)})
	if title := homeCardTitle(t, a); title == before {
		t.Fatalf("a motion message over another row left the card on %q", title)
	}
	if a.home.cursor != cursor {
		t.Fatalf("the pointer moved the cursor from %d to %d", cursor, a.home.cursor)
	}
}

// AND THE TASKS PLACE LIGHTS THE ROW UNDER THE POINTER TOO. It is the one
// promoted place placemouse_test.go's preview law does not reach — its rows are
// its own hit kinds rather than body lines — and it is the longest list on the
// surface, which is exactly where a pointer is worth having.
func TestHoveringARowOfTheTasksPlaceLightsIt(t *testing.T) {
	a := historyApp(t, 200)
	a.width, a.height = 120, 30
	_, hits, _, _ := a.taskSheetFrame(a.width, a.height)
	lit := false
	for y, hit := range hits {
		if hit.kind != taskSheetHitRow || hit.index == a.taskSheet.cursor {
			continue
		}
		cursor := a.taskSheet.cursor
		before := placeFrameText(a)
		drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
		if a.taskSheet.cursor != cursor {
			t.Fatalf("the pointer over row %d moved the cursor from %d to %d", y, cursor, a.taskSheet.cursor)
		}
		after := placeFrameText(a)
		beforeLines, afterLines := splitLines(before), splitLines(after)
		if y < len(beforeLines) && y < len(afterLines) && beforeLines[y] != afterLines[y] {
			lit = true
			break
		}
		if before != after {
			lit = true
			break
		}
	}
	if !lit {
		t.Fatal("no row of the tasks place lights under the pointer")
	}
}

// ── the window moves, and not just the cursor ───────────────────────────────

// `↓` PAST THE BOTTOM OF THE WINDOW SCROLLS THE LIST. placeeveryone_test.go's
// walk proves the cursor is still drawn afterwards, which a place could satisfy
// by refusing to move it at all past the last visible row — a list that stops
// dead at the window's edge and a list that scrolls look identical to that
// question. This asks the sharper one: the FIRST body line on the frame has to
// have moved, so the rows under the window are genuinely reachable.
//
// The tasks place is the one this matters most on: its tail fades with depth
// (depthfade.go), which is a claim that there is more below — a claim a page
// that could not scroll would be making falsely.
func TestWalkingPastTheWindowScrollsEveryPlacesList(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			// A SHORTER FRAME THAN THE LABS OPEN ON, because the law is only owed
			// by a place whose body does not fit — and three of these labs hold a
			// list their own window can draw whole. Cutting the room is how the
			// same seven labs are made to state it, rather than growing three
			// fixtures other tests read.
			a.height = 14
			placeWalkToTop(t, a)
			_, last, drawn := placeWindowOf(place.hits(a))
			if drawn == 0 {
				t.Fatalf("the %s place drew no body rows at all", place.id.word())
			}
			for i := 0; i < drawn+12; i++ {
				drive(t, a, key("down"))
			}
			_, lastAfter, _ := placeWindowOf(place.hits(a))
			cursor := place.cursor(a)
			onFrame := false
			for _, at := range place.hits(a) {
				if at == cursor {
					onFrame = true
					break
				}
			}
			if !onFrame {
				t.Fatalf("the cursor walked to body line %d and the %s place does not draw it",
					cursor, place.id.word())
			}
			// A BODY THAT FITS WHOLE HAS NO WINDOW TO MOVE, which is the common
			// case and the right one — the law is only owed by a place whose
			// cursor has walked past what its first frame could hold.
			if cursor <= last {
				t.Skipf("the %s place's whole body fits in its window, so there is nothing to scroll", place.id.word())
			}
			// THE FAR EDGE OF THE WINDOW IS WHAT HAS TO MOVE, and not its first
			// line: a place may pin a heading at the top of its region, and one
			// that did would have a window that scrolls perfectly while its first
			// drawn line never changes.
			if lastAfter <= last {
				t.Fatalf("the cursor walked to body line %d and the %s place still draws no further than line %d",
					cursor, place.id.word(), lastAfter)
			}
		})
	}
}

// placeWindowOf is the window a frame's hit map describes: the first and last
// body lines it put on screen, and how many rows it drew.
//
// THE THREE ARE NOT DERIVABLE FROM EACH OTHER. A place whose rows are two lines
// tall, or whose headings are lines no cursor stops on, draws a window with gaps
// in it — so the count is not `last - first` and the last line is not
// `first + count`.
func placeWindowOf(hits []int) (first, last, drawn int) {
	first, last = -1, -1
	for _, at := range hits {
		if at < 0 {
			continue
		}
		if first < 0 {
			first = at
		}
		if at > last {
			last = at
		}
		drawn++
	}
	return first, last, drawn
}
