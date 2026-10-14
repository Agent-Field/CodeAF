package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE POINTER ON A PLACE, AS A PERSON MEETS IT.
//
// Three gestures, one law each, and every one of them was reported broken by
// the owner against the real binary: a tab word is a door, the wheel moves the
// list under the pointer, and mouse and keyboard share the selected row.

// autoPlaceLab is the automations place open over three automations.
func autoPlaceLab(t *testing.T) *app {
	t.Helper()
	a, _ := automationLab(t, labWatch("watch the filings"), labWork("sweep the inbox", "1h"), labReminder("price the crew"))
	a.width, a.height = 100, 24
	runCmd(a.openAutomations())
	if !a.at(pageAutomations) {
		t.Fatal("the automations place did not open over three automations")
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

// AND THE AUTOMATIONS PLACE ANSWERS IT TOO, which is the same law asked of the
// other promoted list.
func TestTheWheelWalksTheAutomationsPlacesCursor(t *testing.T) {
	a := autoPlaceLab(t)
	was := a.autoPlace.cursor
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
	if a.autoPlace.cursor == was {
		t.Fatalf("the wheel did not move the automations place's cursor from %d", was)
	}
}

// ── the hover ───────────────────────────────────────────────────────────────

// AND THE AUTOMATIONS PLACE PREVIEWS TOO: a pointer crossing a row lights it
// and puts the cursor on it, and a press is enter on that row.
func TestHoveringARowOfTheAutomationsPlaceLightsIt(t *testing.T) {
	a := autoPlaceLab(t)
	_, hits := automationsPlaceFrame(a)
	other := -1
	for _, at := range hits {
		if at >= 0 && at != a.autoPlace.cursor {
			other = at
			break
		}
	}
	if other < 0 {
		t.Fatal("the automations place drew no row that is not the cursor")
	}
	y := placeBodyRowOf(t, hits, other)
	before, _ := automationsPlaceFrame(a)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	if a.autoPlace.cursor != other {
		t.Fatalf("the pointer selected automation row %d, want %d", a.autoPlace.cursor, other)
	}
	after, _ := automationsPlaceFrame(a)
	if before[y] == after[y] {
		t.Fatalf("the hovered automation row is painted exactly as it was: %q", plain(after[y]))
	}
	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if a.autoPlace.history == "" {
		t.Fatalf("clicking automation row %d did not open its history", other)
	}
}

// ── the window ──────────────────────────────────────────────────────────────
