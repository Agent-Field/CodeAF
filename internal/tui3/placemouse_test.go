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

// searchPlaceWithHits is the search place standing over real results — the one
// promoted place a test can fill without a store on disk, and therefore the one
// this file uses to pin the laws every promoted place is held to.
func searchPlaceWithHits(t *testing.T) *app {
	t.Helper()
	hits, _ := searchFixture()
	a := searchLab(t, &searchFakeStore{hits: hits})
	a.width, a.height = 120, 30
	typeInto(t, a, "report")
	a.searchDone(searchDoneMsg{ask: a.search.ask, hits: hits})
	if len(a.search.hits) == 0 {
		t.Fatal("the search fixture put no results on the page")
	}
	return a
}

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

// THE WHEEL MOVES THE LIST UNDER THE POINTER, on every place. It is the oldest
// thing a pointer does, and a place that let the wheel fall through to the
// conversation would scroll a transcript nobody can see.
func TestTheWheelWalksThePlacesCursor(t *testing.T) {
	a := searchPlaceWithHits(t)
	was := a.search.cursor
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
	if a.search.cursor == was {
		t.Fatalf("the wheel did not move the search place's cursor from %d", was)
	}
	down := a.search.cursor
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: placeHeadRows + 1, Button: tea.MouseWheelUp})
	if a.search.cursor == down {
		t.Fatalf("the wheel back up did not move the cursor from %d", down)
	}
}

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

// Pointer motion selects the same row that the keyboard acts on.
func TestHoveringARowOfAPlaceMovesTheSelection(t *testing.T) {
	a := searchPlaceWithHits(t)
	_, hits, _, _ := a.searchFrame(a.width, a.height)
	// The second stop on the page, which is not where the cursor opened.
	stops := []int{}
	for l := 0; l < len(a.search.reading.hits)+2; l++ {
		if _, ok := a.search.reading.at(l); ok {
			stops = append(stops, l)
		}
	}
	if len(stops) < 2 {
		t.Skip("this fixture has one result, so there is no row to hover that is not the cursor")
	}
	other := stops[1]
	y := placeBodyRowOf(t, hits, other)
	before, _, _, _ := a.searchFrame(a.width, a.height)
	drive(t, a, tea.MouseMotionMsg{X: 4, Y: y})
	if a.search.cursor != other {
		t.Fatalf("the pointer selected %d, want %d", a.search.cursor, other)
	}
	after, _, _, _ := a.searchFrame(a.width, a.height)
	if before[y] == after[y] {
		t.Fatalf("the hovered row is painted exactly as it was: %q", plain(after[y]))
	}
	// A press opens the row that mouse navigation already selected.
	drive(t, a, tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if a.search.cursor != other {
		t.Fatalf("clicking body line %d left the cursor on %d", other, a.search.cursor)
	}
}

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

// A LIST WALKED PAST THE BOTTOM OF ITS ROOM SCROLLS. A place whose body was cut
// at the room and never moved would lose the cursor off the end of the screen,
// which is the one thing a list may never do.
func TestAPlaceScrollsWhenTheCursorWalksPastItsRoom(t *testing.T) {
	a := searchPlaceWithHits(t)
	// A frame short enough that the results cannot all fit at once.
	a.width, a.height = 120, 14
	lines, hits, _, _ := a.searchFrame(a.width, a.height)
	rows := 0
	for _, at := range hits {
		if at >= 0 {
			rows++
		}
	}
	if rows >= len(a.search.reading.hits) {
		t.Skip("this frame holds the whole reading, so there is nothing to scroll")
	}
	_ = lines
	for i := 0; i < len(a.search.reading.hits)+4; i++ {
		drive(t, a, key("down"))
	}
	_, hits, _, _ = a.searchFrame(a.width, a.height)
	on := false
	for _, at := range hits {
		if at == a.search.cursor {
			on = true
		}
	}
	if !on {
		t.Fatalf("the cursor walked to body line %d and the frame does not draw it", a.search.cursor)
	}
}
