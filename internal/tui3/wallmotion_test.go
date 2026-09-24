package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// wallPinned is a wall over the three-conversation fixture with its clock held
// still, so every motion is asserted at a moment the test names.
func wallPinned(t *testing.T) (*app, *time.Time) {
	t.Helper()
	a, _, _ := tabApp(t)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a, &now
}

// wallKeyPress is one key through the wall's own door.
func wallKeyPress(a *app, s string) tea.Cmd { return a.wallKey(key(s)) }

// wallRectBlank reports whether every cell of r is blank in the plain frame.
func wallRectBlank(rows []string, r wallRect) bool {
	for y := r.y0; y < r.y1 && y < len(rows); y++ {
		line := []rune(plain(rows[y]))
		for x := r.x0; x < r.x1 && x < len(line); x++ {
			if line[x] != ' ' {
				return false
			}
		}
	}
	return true
}

// ONE PRESS ON A TILE OPENS IT, and the pointer resting on another first
// moved nothing: hover is a preview, and the keyboard's focus is its own.
func TestWallOnePressOnATileOpensIt(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	target := -1
	for i, tile := range tiles {
		if !tile.here {
			target = i
		}
	}
	focus := a.wall.focus
	body := wallHitFor(t, a, wallHitTile, target)
	a.wallMotion(body.x0+4, body.y0+3)
	_ = a.wallFrame(a.width, a.height)
	if a.wall.focus != focus {
		t.Fatalf("the hover moved the focus from %d to %d", focus, a.wall.focus)
	}
	if _, took := a.wallPress(body.x0+4, body.y0+3); !took {
		t.Fatal("the wall did not take the press")
	}
	if a.wall.on {
		t.Fatal("one press on a tile did not open it")
	}
	if a.frontTabKey() != tiles[target].tab.key {
		t.Fatalf("the press opened %q, want %q", a.frontTabKey(), tiles[target].tab.key)
	}
}

// THE WHEEL MOVES THE VIEW A ROW A NOTCH, clamped to the list; a burst inside
// one settle is one row; a notch back is taken at once; and the focus moves
// only when the scroll would leave it off screen.
func TestWallWheelScrollsTheViewARowANotch(t *testing.T) {
	a, now := wallPinned(t)
	a.height = 30
	a.wall.cols = 1
	_ = a.openWall()
	a.wallMove(0, len(a.wallShown(a.now())))
	_ = a.wallFrame(a.width, a.height)
	n := len(a.wallShown(a.now()))
	vis := a.wallRowsOnScreen(n)
	if n-vis < 2 {
		t.Fatalf("the fixture scrolls %d rows; the test needs two", n-vis)
	}
	x, y := 10, a.wall.headRows+6

	a.wallWheel(x, y, true)
	if a.wall.scroll != 1 || a.wall.focus != 1 {
		t.Fatalf("one notch: scroll %d focus %d, want 1 and 1 (the focus left the screen)", a.wall.scroll, a.wall.focus)
	}
	*now = now.Add(10 * time.Millisecond)
	a.wallWheel(x, y, true)
	if a.wall.scroll != 1 {
		t.Fatalf("a notch inside the settle moved the view to %d", a.wall.scroll)
	}
	*now = now.Add(wallWheelSettle)
	a.wallWheel(x, y, true)
	a.wallWheel(x, y, true)
	if a.wall.scroll != 2 {
		t.Fatalf("after the settle the view is at %d, want 2", a.wall.scroll)
	}
	*now = now.Add(wallWheelSettle)
	a.wallWheel(x, y, true)
	if a.wall.scroll != 2 {
		t.Fatalf("the wheel scrolled past the end, to %d", a.wall.scroll)
	}
	a.wallWheel(x, y, false)
	if a.wall.scroll != 1 {
		t.Fatalf("a notch back inside the settle was not taken: %d", a.wall.scroll)
	}
	// The frame draws the scroll the wheel left, and lights what slid under
	// the resting pointer.
	_ = a.wallFrame(a.width, a.height)
	if a.wall.scroll != 1 || a.wall.hover.kind != wallHitTile || a.wall.hover.arg != 1 {
		t.Fatalf("after the frame: scroll %d hover %+v", a.wall.scroll, a.wall.hover)
	}

	// A focus already on screen stays where it is.
	a.wall.cols = 2
	a.wall.scroll = 0
	a.wallMove(0, n)
	_ = a.wallFrame(a.width, a.height)
	*now = now.Add(wallWheelSettle)
	a.wallWheel(x, y, true)
	if a.wall.focus != 0 && a.wallRowsOnScreen(n) > 1 {
		t.Fatalf("a focus on screen moved to %d", a.wall.focus)
	}
}

// THE WALL COMES IN ROW BY ROW, in about a seventh of a second, and a key
// finishes it at once; the linear tier draws it whole.
func TestWallOpeningRevealsRowByRowAndAKeyFinishesIt(t *testing.T) {
	a, now := wallPinned(t)
	a.wall.cols = 1
	_ = a.openWall()
	a.wallMove(0, len(a.wallShown(a.now())))
	a.wall.revealAt = *now
	rows := a.wallFrame(a.width, a.height)
	if a.wallRowsOnScreen(3) < 2 {
		t.Fatal("the fixture shows one row; the reveal needs two")
	}
	first, _ := a.wallTileRect(0)
	second, _ := a.wallTileRect(1)
	if wallRectBlank(rows, first) {
		t.Fatal("the first row waited: it is where the eye lands")
	}
	if !wallRectBlank(rows, second) {
		t.Fatalf("the second row was drawn on the first frame:\n%s", plain(strings.Join(rows, "\n")))
	}
	if !a.wallAnimating() {
		t.Fatal("the reveal does not keep the paint clock turning")
	}
	*now = now.Add(wallRevealStep)
	rows = a.wallFrame(a.width, a.height)
	if wallRectBlank(rows, second) || !a.wall.revealAt.IsZero() {
		t.Fatal("the second row is not in after its step")
	}

	// A key during the reveal finishes it before it acts.
	_ = a.openWall()
	a.wall.revealAt = *now
	_ = a.wallFrame(a.width, a.height)
	wallKeyPress(a, "right")
	if !a.wall.revealAt.IsZero() {
		t.Fatal("a key did not finish the reveal")
	}
	if rows := a.wallFrame(a.width, a.height); wallRectBlank(rows, second) {
		t.Fatal("the frame after the key is still revealing")
	}

	a.closeWall()
	a.linear = true
	_ = a.openWall()
	if !a.wall.revealAt.IsZero() {
		t.Fatal("the linear tier was given a reveal")
	}
}

// AN OPENED TILE GROWS INTO THE FRAME over a few frames, around a conversation
// that is already in front, and a key ends the growing at once.
func TestWallOpenedTileZoomsIntoTheFrame(t *testing.T) {
	a, now := wallPinned(t)
	_ = a.openWall()
	a.wallSettle()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	target := -1
	for i, tile := range tiles {
		if !tile.here {
			target = i
		}
	}
	a.wallMove(target, len(tiles))
	_ = a.wallFrame(a.width, a.height)
	from, _ := a.wallTileRect(target)
	wallKeyPress(a, "enter")
	if a.wall.on || a.frontTabKey() != tiles[target].tab.key {
		t.Fatal("enter did not open the tile")
	}
	if !a.wallAnimating() {
		t.Fatal("the zoom does not keep the paint clock turning")
	}
	lines := screenLines(a)
	if from.y0 == 0 || strings.TrimSpace(lines[0]) != "" {
		t.Fatalf("the first frame of the zoom drew outside the tile: %q", lines[0])
	}
	if !strings.HasPrefix(strings.TrimLeft(lines[from.y0], " "), "╭") {
		t.Fatalf("the zoom's edge is not on the tile's top row: %q", lines[from.y0])
	}
	*now = now.Add(wallZoomFor)
	_ = screenLines(a)
	if !a.wall.zoomAt.IsZero() || a.wallAnimating() {
		t.Fatal("the zoom outlived its time")
	}

	// A key ends it on the spot.
	a.wall.zoomFrom, a.wall.zoomAt = from, *now
	a.key(key("h"))
	if !a.wall.zoomAt.IsZero() {
		t.Fatal("a key did not end the zoom")
	}
}

// ESC TAKES OFF THE INNERMOST THING FIRST: the popover, the card, the
// selection, the filter, and only then the wall.
func TestWallEscClosesTheInnermostLayerFirst(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	word := tiles[len(tiles)-1].tab.word
	for _, r := range "/" + word[:3] {
		wallKeyPress(a, string(r))
	}
	wallKeyPress(a, "enter")
	if a.wall.filter == "" || a.wall.filterOn {
		t.Fatalf("filter %q on %v", a.wall.filter, a.wall.filterOn)
	}
	wallKeyPress(a, " ")
	wallKeyPress(a, "m")
	if a.wall.pop.kind != wallPopMembers || len(a.wall.marked) != 1 {
		t.Fatalf("the setup: pop %+v marked %v", a.wall.pop, a.wall.marked)
	}
	steps := []struct {
		name string
		gone func() bool
	}{
		{"the popover", func() bool { return a.wall.pop.kind == wallPopNone && len(a.wall.marked) == 1 }},
		{"the selection", func() bool { return len(a.wall.marked) == 0 && a.wall.filter != "" }},
		{"the filter", func() bool { return a.wall.filter == "" && a.wall.on }},
		{"the wall", func() bool { return !a.wall.on }},
	}
	for _, step := range steps {
		wallKeyPress(a, "esc")
		if !step.gone() {
			t.Fatalf("esc did not take off %s alone: pop %+v marked %v filter %q on %v",
				step.name, a.wall.pop, a.wall.marked, a.wall.filter, a.wall.on)
		}
	}

	// The card goes before the selection under it.
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	wallKeyPress(a, "s")
	wallKeyPress(a, "esc")
	if a.wall.naming || len(a.wall.marked) != 1 || !a.wall.on {
		t.Fatalf("esc on the card: naming %v marked %v on %v", a.wall.naming, a.wall.marked, a.wall.on)
	}
}

// A PRESS OFF A POPOVER OR A CARD PUTS IT AWAY and does nothing else; a press
// on the card's own blank does nothing at all.
func TestWallPressOffACardPutsItAway(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	a.wallSettle()
	_ = a.wallFrame(a.width, a.height)
	focus := a.wall.focus
	wallKeyPress(a, "m")
	_ = a.wallFrame(a.width, a.height)
	card := a.wall.card
	if card.w() == 0 {
		t.Fatal("the popover's place was not recorded")
	}
	// Its own border is on the card and answers nothing.
	if _, took := a.wallPress(card.x0, card.y0); !took || a.wall.pop.kind == wallPopNone {
		t.Fatal("a press on the popover's border put it away")
	}
	x, y := a.width-3, a.height-3
	if card.holds(x, y) {
		t.Fatal("the test's far corner is on the card")
	}
	if _, took := a.wallPress(x, y); !took {
		t.Fatal("the wall did not take the press")
	}
	if a.wall.pop.kind != wallPopNone || !a.wall.on || a.wall.focus != focus {
		t.Fatalf("a press off the popover: pop %+v on %v focus %d", a.wall.pop, a.wall.on, a.wall.focus)
	}

	wallKeyPress(a, "s")
	_ = a.wallFrame(a.width, a.height)
	if a.wall.card.w() == 0 {
		t.Fatal("the card's place was not recorded")
	}
	if _, took := a.wallPress(1, a.height-3); !took || a.wall.naming || !a.wall.on || len(a.wall.marked) != 1 {
		t.Fatalf("a press off the card: naming %v on %v marked %v", a.wall.naming, a.wall.on, a.wall.marked)
	}
}

// CLOSING A TILE HANDS THE FOCUS TO ITS RIGHT, or at the end of the list to
// its left, and never back to the start; closing another tile keeps the
// focus on the conversation it was on.
func TestWallCloseHandsTheFocusToTheNeighbour(t *testing.T) {
	// The fixture is two conversations behind and the one in front, last.
	fresh := func() (*app, []wallTile) {
		a, _, _ := tabApp(t)
		_ = a.openWall()
		_ = a.wallFrame(a.width, a.height)
		tiles := a.wallShown(a.now())
		if len(tiles) != 3 || tiles[0].here || tiles[1].here {
			t.Fatalf("the fixture changed: %d tiles", len(tiles))
		}
		return a, tiles
	}
	focused := func(a *app) string { return a.wallShown(a.now())[a.wall.focus].tab.key }

	a, tiles := fresh()
	a.wallMove(0, len(tiles))
	wallKeyPress(a, "x")
	if got := focused(a); got != tiles[1].tab.key {
		t.Fatalf("closing the focused tile focused %q, want its right neighbour %q", got, tiles[1].tab.key)
	}

	a, tiles = fresh()
	a.wallMove(1, len(tiles))
	_ = a.wallDismissAt(tiles, 0)
	if got := focused(a); got != tiles[1].tab.key {
		t.Fatalf("closing another tile moved the focus to %q", got)
	}

	// At the end of the list there is no right: the left neighbour takes it.
	a, tiles = fresh()
	before := append(append([]wallTile(nil), tiles...), wallTile{tab: chatTab{key: "gone"}})
	a.wallRefocus(before, "gone")
	if a.wall.focus != len(tiles)-1 {
		t.Fatalf("the last tile gone focused %d, want %d", a.wall.focus, len(tiles)-1)
	}
}

// EACH TEAM KEEPS ITS PLACE while the wall is up: All, then harbor, then All
// again, returns to the tile that was focused.
func TestWallTeamsKeepTheirPlace(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	id, err := a.teamMake("harbor", []chatTab{tiles[0].tab, tiles[1].tab})
	if err != nil {
		t.Fatal(err)
	}
	a.wallMove(2, len(tiles))
	all := tiles[2].tab.key
	wallKeyPress(a, "1")
	if a.wall.activeID != id || a.wall.focus != 0 {
		t.Fatalf("1: team %q focus %d", a.wall.activeID, a.wall.focus)
	}
	wallKeyPress(a, "right")
	harbor := a.wallShown(a.now())[a.wall.focus].tab.key
	wallKeyPress(a, "1")
	if a.wall.activeID != "" {
		t.Fatalf("the shown team's digit did not go back to All: %q", a.wall.activeID)
	}
	if got := a.wallShown(a.now())[a.wall.focus].tab.key; got != all {
		t.Fatalf("All came back on %q, want %q", got, all)
	}
	wallKeyPress(a, "tab")
	if got := a.wallShown(a.now())[a.wall.focus].tab.key; a.wall.activeID != id || got != harbor {
		t.Fatalf("harbor came back on %q, want %q", got, harbor)
	}
}

// A FILTER FOCUSES ITS FIRST MATCH, the arrows walk the matches while the box
// is up, and clearing it keeps the conversation the focus was on.
func TestWallFilterFocusesTheFirstMatchAndClearingKeepsIt(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	a.wallMove(0, len(tiles))
	want := tiles[len(tiles)-1]
	wallKeyPress(a, "/")
	for _, r := range want.tab.word {
		wallKeyPress(a, string(r))
	}
	shown := a.wallShown(a.now())
	if len(shown) == 0 || a.wall.focus != 0 || shown[0].tab.key != want.tab.key {
		t.Fatalf("the filter focused %d of %d", a.wall.focus, len(shown))
	}
	wallKeyPress(a, "esc")
	if a.wall.filter != "" || a.wallShown(a.now())[a.wall.focus].tab.key != want.tab.key {
		t.Fatalf("clearing the filter lost the match: focus %d", a.wall.focus)
	}
}

// THE PICKED ARE ACTED ON BY THE SAME KEYS AS THE TRAY'S BUTTONS: m is Add
// to…, x is Close views; e opens the shown team's settings.
func TestWallKeysDoWhatTheButtonsDo(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	tiles := a.wallShown(a.now())
	var behind []int
	for i, tile := range tiles {
		if !tile.here {
			behind = append(behind, i)
		}
	}
	for _, i := range behind {
		a.wallToggle(tiles, i)
	}
	wallKeyPress(a, "m")
	if a.wall.pop.kind != wallPopMembers || len(a.wall.pop.targets) != len(behind) {
		t.Fatalf("m with %d picked: %+v", len(behind), a.wall.pop)
	}
	wallKeyPress(a, "esc")
	wallKeyPress(a, "x")
	if n := len(a.wallShown(a.now())); n != len(tiles)-len(behind) {
		t.Fatalf("x with %d picked left %d of %d", len(behind), n, len(tiles))
	}

	harbor, err := a.teamMake("harbor", []chatTab{tiles[0].tab})
	if err != nil {
		t.Fatal(err)
	}
	wallKeyPress(a, "e")
	if a.wall.pop.kind != wallPopNone {
		t.Fatal("e with no team shown opened something")
	}
	wallKeyPress(a, "1")
	_ = a.wallFrame(a.width, a.height)
	wallKeyPress(a, "e")
	if a.wall.pop.kind != wallPopSettings || a.wall.pop.team != harbor {
		t.Fatalf("e: %+v", a.wall.pop)
	}
}

// A POINTER WANDERING INSIDE ONE TARGET REUSES THE FRAME it was given; one
// that crosses onto another target, or rests there after a scroll, does not.
func TestWallPointerInsideOneTargetDrawsNothing(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	body := wallHitFor(t, a, wallHitTile, 0)
	a.wallMotion(body.x0+4, body.y0+3)
	_ = a.wallFrame(a.width, a.height)
	a.ptr.still = false
	a.wallMotion(body.x0+6, body.y0+4)
	if !a.ptr.still {
		t.Fatal("a motion inside the same tile asked for a new frame")
	}
	a.ptr.still = false
	a.wall.stirred = true
	a.wallMotion(body.x0+7, body.y0+4)
	if a.ptr.still {
		t.Fatal("a motion after the grid moved reused the old frame")
	}
}

// THE WALL'S SPINNER COUNTS IN TIME, so a wall drawn on its half-second tick
// shows where the spinner has got to; the reduced tiers draw it still.
func TestWallSpinnerCountsInTime(t *testing.T) {
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	step := spinnerStep * frameInterval
	if wallSpin(at.Add(step)) != wallSpin(at)+1 {
		t.Fatal("one spinner step of time is not one step of the spinner")
	}
	a, _, _ := tabApp(t)
	a.linear = true
	if !a.wallReduced() || a.wallMotionOK() {
		t.Fatal("the linear tier is not reduced")
	}
}

// A WAITING TILE'S ANSWER AND ITS OPEN ↗ SHARE A TARGET, and the painter is
// told the pointer's row so it lights the one under it: moving between the
// two rows on the same target is a new frame.
func TestWallPointerRowReachesThePainter(t *testing.T) {
	a, _, _ := tabApp(t)
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	body := wallHitFor(t, a, wallHitTile, 0)
	a.wallMotion(body.x0+4, body.y0+3)
	_ = a.wallFrame(a.width, a.height)
	if !a.wall.ptrIn || a.wall.ptrY-a.wall.headRows != body.y0+3-a.wall.headRows {
		t.Fatalf("the pointer's row is not kept: in %v y %d", a.wall.ptrIn, a.wall.ptrY)
	}
	a.wall.hover = wallHitRef{kind: wallHitOpen, arg: 0}
	a.wall.hits = []wallHit{{x0: 0, y0: 0, x1: a.width, y1: a.height, kind: wallHitOpen, arg: 0}}
	a.ptr.still = false
	a.wallMotion(5, a.wall.ptrY+1)
	if a.ptr.still || !a.wall.stirred {
		t.Fatal("a new row on a shared target reused the old frame")
	}
}
