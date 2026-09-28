package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// THE POINTER ON A PLACE, AS A PERSON MEETS IT.
//
// Three gestures, one law each, and every one of them was reported broken by
// the owner against the real binary: a tab word is a door, the wheel moves the
// list under the pointer, and mouse and keyboard share the selected row.

// standPlaceLab is the standing place standing over three orders.
func standPlaceLab(t *testing.T) *app {
	t.Helper()
	a, _ := standingPlaceApp(t, []standing.Item{
		standOrder("one", "watch the filings", standing.AltitudeMachine),
		standOrder("two", "sweep the inbox", standing.AltitudeProject),
		standOrder("three", "price the crew", standing.AltitudeConversation),
	}, nil)
	a.openStanding()
	if !a.at(pageStanding) {
		t.Fatal("the standing place did not open over three orders")
	}
	return a
}

// placeBodyRowOf finds the screen row one place's frame drew a given body line
// on. It asks the frame rather than counting, exactly as a press does.
func placeBodyRowOf(t *testing.T, hits []int, line int) int {
	t.Helper()
	for y, at := range hits {
		if at == line {
			return y
		}
	}
	t.Fatalf("body line %d is not on the frame", line)
	return -1
}

// ── the tab bar ─────────────────────────────────────────────────────────────

// A TAB WORD IS A DOOR. The bar names four rooms — and the one you are in, when
// it is one of the others — and draws a band on that one; a bar that answered a
// press with nothing would be four labels.
func TestClickingATabWordGoesToThatPlace(t *testing.T) {
	a := placeApp(t)
	// The frame is drawn first, because a press resolves against the bar that
	// was actually painted — the width ladder can drop words.
	placeFrameText(a)
	x, ok := placeTabColumnOf(a, pageSpend)
	if !ok {
		t.Fatal("the spend tab is not on this bar")
	}
	drive(t, a, tea.MouseClickMsg{X: x, Y: navRow, Button: tea.MouseLeft})
	if a.page != pageSpend {
		t.Fatalf("clicking the spend tab left the router on %q", a.page.word())
	}
	// AND THE PLACE YOU ARE ALREADY IN IS NOT REOPENED BY A PRESS ON ITS OWN
	// WORD — pressing where you are standing is not a gesture.
	placeFrameText(a)
	x, ok = placeTabColumnOf(a, pageSpend)
	if !ok {
		t.Fatal("the spend tab left the bar it is banded on")
	}
	// The spend place has no box, so what a reopen would reset is its cursor.
	a.moveSpend(1)
	was := a.spend.cursor
	drive(t, a, tea.MouseClickMsg{X: x, Y: navRow, Button: tea.MouseLeft})
	if a.spend.cursor != was {
		t.Fatalf("pressing the tab you are on reopened the place: the cursor moved from %d to %d", was, a.spend.cursor)
	}
}

// AND A PRESS IN THE NAV'S AIR DOES NOTHING. Two words' buttons touch, each
// owning its own pad cells, so the air on the row is the cells before the
// first button and after the last; they belong to no room, and a row that
// rounded a miss to its nearest neighbour would open the wrong place for a
// one-cell slip.
func TestClickingBetweenTwoTabsDoesNothing(t *testing.T) {
	a := placeApp(t)
	placeFrameText(a)
	gap, ok := placeTabGapColumn(a)
	if !ok {
		t.Fatal("this nav has no air before its first button")
	}
	drive(t, a, tea.MouseClickMsg{X: gap, Y: navRow, Button: tea.MouseLeft})
	if a.page != pageHome {
		t.Fatalf("a press in the gap moved to %q", a.page.word())
	}
}

// AND A FRAME WITH NO TAB BAR ON IT HAS NO TAB TO PRESS. Home's phone inbox
// draws a rule in the cells the bar rides at every wider tier; a press resolved
// against the last bar this window painted would open a place for a click on
// that rule.
func TestAFrameWithNoTabBarHasNoTabToPress(t *testing.T) {
	a := placeApp(t)
	placeFrameText(a)
	if a.tabRow < 0 {
		t.Fatal("the wide frame recorded no tab bar to begin with")
	}
	a.width, a.height = 44, 24
	placeFrameText(a)
	if !a.home.phone {
		t.Skip("forty-four columns is not the phone tier on this build")
	}
	if a.tabRow >= 0 {
		t.Fatalf("the phone frame claims a tab bar on row %d", a.tabRow)
	}
	drive(t, a, tea.MouseClickMsg{X: 4, Y: navRow, Button: tea.MouseLeft})
	if a.page != pageHome {
		t.Fatalf("a press on the phone frame's second row went to %q", a.page.word())
	}
}

// placeTabColumnOf is a cell inside one place's chip on the bar as it stands.
func placeTabColumnOf(a *app, id page) (int, bool) {
	for _, span := range a.tabs {
		if span.id == id {
			return span.from, true
		}
	}
	return 0, false
}

// placeTabGapColumn is a cell of the nav's air: the last column before the
// first button, between it and the wordmark, which no button claims.
func placeTabGapColumn(a *app) (int, bool) {
	if len(a.tabs) < 1 || a.tabs[0].from < 1 {
		return 0, false
	}
	for _, span := range a.tabs {
		if a.tabs[0].from-1 >= span.from && a.tabs[0].from-1 < span.to {
			return 0, false
		}
	}
	return a.tabs[0].from - 1, true
}

// ── the wheel ───────────────────────────────────────────────────────────────

// AND THE STANDING PLACE ANSWERS IT TOO, which is the same law asked of the
// other promoted list.
func TestTheWheelWalksTheStandingPlacesCursor(t *testing.T) {
	a := standPlaceLab(t)
	was := a.orders.cursor
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
	if a.orders.cursor == was {
		t.Fatalf("the wheel did not move the standing place's cursor from %d", was)
	}
}

// ── the hover ───────────────────────────────────────────────────────────────

// AND THE STANDING PLACE PREVIEWS TOO. Its hover map was left answering -1
// when the list was promoted out of the chrome that used to draw it, so a
// pointer crossing it lit nothing at all.
func TestHoveringARowOfTheStandingPlaceLightsIt(t *testing.T) {
	a := standPlaceLab(t)
	_, hits, _, _ := a.standingPlaceFrame(a.width, a.height)
	other := -1
	for _, at := range hits {
		if at >= 0 && at != a.orders.cursor {
			other = at
			break
		}
	}
	if other < 0 {
		t.Fatal("the standing place drew no row that is not the cursor")
	}
	y := placeBodyRowOf(t, hits, other)
	before, _, _, _ := a.standingPlaceFrame(a.width, a.height)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	if a.orders.cursor != other {
		t.Fatalf("the pointer selected standing row %d, want %d", a.orders.cursor, other)
	}
	after, _, _, _ := a.standingPlaceFrame(a.width, a.height)
	if before[y] == after[y] {
		t.Fatalf("the hovered standing row is painted exactly as it was: %q", plain(after[y]))
	}
	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if a.orders.cursor != other {
		t.Fatalf("clicking standing row %d left the cursor on %d", other, a.orders.cursor)
	}
}

// ── the window ──────────────────────────────────────────────────────────────
