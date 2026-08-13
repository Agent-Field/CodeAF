package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The place line (place.go): one persistent breadcrumb above the composer, the
// spatial truth of this surface, and the one row that answers both "where am I"
// and "where are the words I am typing going".
//
// Three laws are pinned here, and they are the three the wave was written to:
//
//  1. esc pops exactly one segment, always, and the pop order is one order.
//  2. every segment is a button, and the one you are standing on is not.
//  3. width pressure collapses the MIDDLE and never the ends.

// -- helpers -----------------------------------------------------------------

// placeWords is the path as words, which is what every assertion here is really
// about: the line is prose about where the reader is, and an index into a slice
// of structs is not.
func placeWords(app *App) []string {
	app.refresh()
	out := make([]string, 0, len(app.placeBar.segs))
	for _, seg := range app.placeBar.segs {
		out = append(out, seg.Word)
	}
	return out
}

// placeThreadWord is the conversation's own segment, or "" when the path has
// not got one (a page is a sibling of the thread, not a place inside it).
func placeThreadWord(app *App) string {
	app.refresh()
	for _, seg := range app.placeBar.segs {
		if seg.Kind == placeThread {
			return seg.Word
		}
	}
	return ""
}

// placeMouthWord is the segment the composer's words would land in, or "" when
// that is the segment the reader is standing on.
func placeMouthWord(app *App) string {
	app.refresh()
	if app.placeBar.mouth < 0 {
		return ""
	}
	return app.placeBar.segs[app.placeBar.mouth].Word
}

// -- the derivation ----------------------------------------------------------

// The path is DERIVED, never kept, so every state the surface can be in has to
// produce the right words from the state the surface already holds.
func TestThePlacePathIsDerivedFromWhereTheReaderIs(t *testing.T) {
	app, _ := boardApp(t)

	if got := placeWords(app); len(got) != 2 || got[0] != placeRootWord {
		t.Fatalf("a window in its own thread stands at %q", got)
	}
	if kind := app.placeBar.segs[1].Kind; kind != placeThread {
		t.Fatalf("the second segment is kind %d and not the thread", kind)
	}

	// A task room: the rail's scope becomes the third rung.
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	got := placeWords(app)
	if len(got) != 3 || got[0] != placeRootWord || got[2] != "wisp-parity" {
		t.Fatalf("inside a task the path reads %q", got)
	}
	if kind := app.placeBar.segs[2].Kind; kind != placeScope {
		t.Fatalf("the task segment is kind %d and not a scope", kind)
	}

	// A page is a SIBLING of the thread and stops the path: arriving at one
	// leaves whatever room the reader had descended into.
	app.showPage(pageBoard)
	got = placeWords(app)
	if len(got) != 2 || got[1] != pageBoard.String() {
		t.Fatalf("the board page reads %q, want the root and the page word", got)
	}
	if kind := app.placeBar.segs[1].Kind; kind != placePage {
		t.Fatalf("the page segment is kind %d and not a page", kind)
	}
}

// A thread the scribe has not named yet still has a rung, and the rung says the
// only true thing there is to say. The title chip could decline to name one —
// an absent chip reads as no chip — but a path that lost its second segment
// would leave the room the reader is in with no name at all.
func TestAnUnnamedThreadSaysUntitledRoomOnThePath(t *testing.T) {
	backend := &threadsBackend{boardBackend: board()}
	backend.sessions = []store.Session{{ID: testSession}}
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)

	if got := placeThreadWord(app); got != untitledRoom {
		t.Fatalf("an unnamed thread is called %q on the path", got)
	}
	for _, word := range placeWords(app) {
		if strings.Contains(word, testSession) {
			t.Fatalf("the path drew a session id: %q", word)
		}
	}
}

// A record page drilled into from a record page is one more segment, and the
// model is a slice for exactly that reason — the nesting is unbounded.
func TestARecordPageAddsASegmentPerDrill(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	before := len(placeWords(app))

	app.drillInto("job-1-part", "chapter two")
	got := placeWords(app)
	if len(got) != before+1 || got[len(got)-1] != "chapter two" {
		t.Fatalf("the drill did not add its own segment: %q", got)
	}
	if kind := app.placeBar.segs[len(got)-1].Kind; kind != placePart {
		t.Fatalf("the drilled segment is kind %d and not a part", kind)
	}
}

// 12.13.2's merge law: an entered room says its name ONCE. The rail's last
// crumb and the main pane's title are the same object seen from two sides.
func TestThePathNeverRepeatsASegment(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")

	got := placeWords(app)
	if n := strings.Count(strings.Join(got, "\x00"), "wisp-parity"); n != 1 {
		t.Fatalf("the path names one room %d times: %q", n, got)
	}
}

// -- law 1: the pop order ----------------------------------------------------

// A RAISED OVERLAY IS A SCOPE ABOVE THE PATH, so esc closes it first and moves
// nobody. That is the law rather than an exception to it.
func TestEscClosesAnOverlayBeforeItTouchesThePlace(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	deep := placeWords(app)

	app.openPalette()
	pressThrough(app, "esc")
	if app.overlay != overlayNone {
		t.Fatal("esc did not close the raised overlay")
	}
	if got := placeWords(app); len(got) != len(deep) {
		t.Fatalf("closing an overlay also popped a segment: %q -> %q", deep, got)
	}
}

// A STREAMING TURN IS THE INNERMOST LIVE THING, so esc stops it before it moves
// the reader anywhere (8.2.21).
func TestEscInterruptsBeforeItPopsAPlace(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	deep := placeWords(app)

	app.Update(streamBatchMsg{events: []StreamEvent{
		{Kind: StreamStarted, Session: testSession},
		{Kind: StreamDelta, Session: testSession, Delta: `{"reply":"reading`},
	}})
	if !app.canInterrupt() {
		t.Fatal("the fixture is not streaming, so the rung cannot be pinned")
	}
	pressThrough(app, "esc")
	if !app.turn.stopped {
		t.Fatal("esc did not interrupt the turn it was watching")
	}
	if got := placeWords(app); len(got) != len(deep) {
		t.Fatalf("the interrupt also popped a segment: %q -> %q", deep, got)
	}
}

// ONE SEGMENT PER PRESS, AND THE ROOT IS A FLOOR. Repeated esc from a deep
// record page walks out to the thread and then stays: esc that keeps going
// eventually throws a reader out of the conversation they were in, and this
// surface has no undo for that.
func TestRepeatedEscWalksToTheThreadAndStops(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	app.drillInto("job-1-part", "chapter two")
	app.refresh()

	if got := placeWords(app); len(got) != 4 {
		t.Fatalf("the fixture is %d deep, want 4: %q", len(got), got)
	}
	for want := 3; want >= 2; want-- {
		pressThrough(app, "esc")
		if got := placeWords(app); len(got) != want {
			t.Fatalf("esc did not pop exactly one segment: want %d, got %q", want, got)
		}
	}
	// The floor. Two more presses change nothing at all.
	for range 2 {
		pressThrough(app, "esc")
		got := placeWords(app)
		if len(got) != 2 || got[0] != placeRootWord {
			t.Fatalf("esc walked past the thread: %q", got)
		}
	}
}

// A PAGE IS A SEGMENT TOO, and esc pops it like any other.
func TestEscPopsThePageSegment(t *testing.T) {
	app, _ := boardApp(t)
	app.showPage(pageBoard)
	pressThrough(app, "esc")
	if app.page != pageThread {
		t.Fatalf("esc left the reader on the %s page", app.page)
	}
}

// -- law 2: every segment is a button ----------------------------------------

// Clicking an ancestor walks straight there, by taking the SAME pop esc takes
// as many times as the path is deep — so a jump and a held-down esc leave the
// surface in the same state.
func TestClickingAnAncestorJumpsToIt(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	app.drillInto("job-1-part", "chapter two")
	app.refresh()

	app.drain(app.jumpPlace(1))
	got := placeWords(app)
	if len(got) != 2 || got[1] == "wisp-parity" {
		t.Fatalf("the jump did not land on the thread: %q", got)
	}
}

// And clicking the word you are standing on does nothing, because a button that
// repeats where you already are is a button lying about having done something.
func TestClickingTheCurrentSegmentIsANoOp(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	before := placeWords(app)

	if cmd := app.jumpPlace(len(before) - 1); cmd != nil {
		t.Fatal("the current segment performed something")
	}
	if got := placeWords(app); strings.Join(got, " ") != strings.Join(before, " ") {
		t.Fatalf("the current segment moved the reader: %q -> %q", before, got)
	}
}

// The ROOT is the one segment that is not a pop: it is the surface's own home,
// which is the board the product already has. See [App.jumpPlace].
func TestClickingTheRootGoesToTheBoard(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")

	app.drain(app.jumpPlace(0))
	if app.page != pageBoard {
		t.Fatalf("the root landed on the %s page", app.page)
	}
}

// A CLICK ON THE ROW RESOLVES TO THE WORD UNDER THE POINTER, through the same
// fitted layout the paint solves — the footer's own discipline, so the eye and
// the hand cannot disagree about where a segment starts.
func TestAPointerOnASegmentResolvesToThatSegment(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	app.refresh()

	line := app.placeBar
	line.retarget(100)
	if len(line.hits) != len(line.segs) {
		t.Fatalf("the hit table has %d runs for %d segments", len(line.hits), len(line.segs))
	}
	for i, hit := range line.hits {
		seg, ok := line.targetAt(hit.from)
		if !ok || seg != i {
			t.Fatalf("column %d resolved to segment %d (ok=%v), want %d", hit.from, seg, ok, i)
		}
	}
	if _, ok := line.targetAt(line.hits[0].to); ok {
		t.Fatal("the ground between two words is a button")
	}
}

// -- law 3: the middle collapses, the ends never do --------------------------

func deepPath() []placeSeg {
	return []placeSeg{
		{Word: "aforge", Kind: placeRoot},
		{Word: "pricing ideation", Kind: placeThread},
		{Word: "30-page story", Kind: placeScope},
		{Word: "chapter two", Kind: placePart},
	}
}

// Root and current are the two segments that always survive, and everything
// between them goes behind one mark.
func TestWidthPressureCollapsesTheMiddleAndKeepsTheEnds(t *testing.T) {
	segs := deepPath()
	full := placeFit(segs, -1, 120)
	if len(full) != len(segs) {
		t.Fatalf("a wide row dropped something: %v", full)
	}

	tight := placeFit(segs, -1, 30)
	if len(tight) != 3 {
		t.Fatalf("the tight row is %d items, want root, mark, current: %v", len(tight), tight)
	}
	if tight[0].word != "aforge" {
		t.Fatalf("the root did not survive: %v", tight)
	}
	if tight[1].seg != placeHidden || tight[1].word != tokens.GlyphEllipsis {
		t.Fatalf("the collapse mark is %v", tight[1])
	}
	if tight[len(tight)-1].word != "chapter two" {
		t.Fatalf("the current segment did not survive: %v", tight)
	}
	if w := placeWidth(tight); w > 30 {
		t.Fatalf("the collapsed row is %d cells wide", w)
	}
}

// The mark opens exactly the segments it stands for, and no others.
func TestTheCollapseMarkOffersExactlyTheHiddenSegments(t *testing.T) {
	segs := deepPath()
	hidden := placeHiddenSegs(segs, placeFit(segs, -1, 30))
	if len(hidden) != 2 || hidden[0] != 1 || hidden[1] != 2 {
		t.Fatalf("the mark stands for %v, want segments 1 and 2", hidden)
	}
	if none := placeHiddenSegs(segs, placeFit(segs, -1, 120)); len(none) != 0 {
		t.Fatalf("a wide row hid %v", none)
	}
}

// The mark is a door, and it is the one overlay grammar this product has.
func TestTheCollapseMarkOpensThePicker(t *testing.T) {
	app, _ := boardApp(t)
	app.placeBar.setPath(deepPath(), -1, false, 0)
	app.placeBar.retarget(30)

	if cmd := app.openPlacePicker(); cmd != nil {
		app.drain(cmd)
	}
	if app.overlay != overlayPlace {
		t.Fatalf("the mark raised overlay %d", app.overlay)
	}
	out := ansi.Strip(app.placePicker.Render(60, 12))
	for _, word := range []string{"pricing ideation", "30-page story"} {
		if !strings.Contains(out, word) {
			t.Fatalf("the picker does not offer %q:\n%s", word, out)
		}
	}
	if strings.Contains(out, "chapter two") {
		t.Fatalf("the picker offers the segment the reader is standing on:\n%s", out)
	}
}

// The row never exceeds the width it was given, never panics, and says nothing
// rather than something unreadable at the bottom of the ladder.
func TestThePlaceLineSurvivesEveryWidth(t *testing.T) {
	line := newPlaceLine(nil)
	line.setPath(deepPath(), 1, true, 3)
	for width := 0; width <= 140; width++ {
		row := line.Render(width)
		if strings.Contains(row, "\n") {
			t.Fatalf("width %d produced more than one row: %q", width, row)
		}
		if w := ansi.StringWidth(row); w > width {
			t.Fatalf("width %d produced %d cells: %q", width, w, row)
		}
		line.retarget(width)
	}
}

// -- the ornament ------------------------------------------------------------

// ONE ORNAMENT AND ONLY ONE: the unseen dot and the running count, in the exact
// glyphs the rail's handle and the bar's dock already spend on them. No money,
// no context gauge — those are standing facts and they have a home in the bar.
func TestTheOrnamentIsTheDotAndTheRunningCount(t *testing.T) {
	line := newPlaceLine(nil)
	line.setPath(deepPath(), -1, false, 0)
	if text, _ := line.ornament(); text != "" {
		t.Fatalf("a quiet window drew %q", text)
	}

	line.setPath(deepPath(), -1, true, 0)
	if text, _ := line.ornament(); text != tokens.GlyphStepDone {
		t.Fatalf("the unseen dot is %q", text)
	}

	line.setPath(deepPath(), -1, false, 1)
	if text, _ := line.ornament(); text != tokens.GlyphWorking+" 1" {
		t.Fatalf("the running count is %q", text)
	}

	line.setPath(deepPath(), -1, true, 2)
	text, _ := line.ornament()
	if text != tokens.GlyphStepDone+" "+tokens.GlyphWorking+" 2" {
		t.Fatalf("both facts read %q", text)
	}
	row := line.Render(80)
	if strings.Contains(row, "$") || strings.Contains(row, "%") {
		t.Fatalf("money or a gauge reached the place line: %q", row)
	}
}

// The ornament is dropped WHOLE rather than crowding the answer out, which is
// the bar row's own law about a column that will not fit.
func TestANarrowRowKeepsThePathAndDropsTheOrnament(t *testing.T) {
	line := newPlaceLine(nil)
	line.setPath(deepPath(), -1, true, 9)
	row := line.Render(22)
	if strings.Contains(row, tokens.GlyphWorking) {
		t.Fatalf("the ornament crowded the path at 22 cells: %q", row)
	}
	if !strings.Contains(row, "aforge") {
		t.Fatalf("the root did not survive at 22 cells: %q", row)
	}
}

// -- the walk ----------------------------------------------------------------

// The chord is registry-declared, so the `?` sheet teaches a key that fires.
func TestThePlaceChordIsDeclaredAndFocusesTheLine(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	app.refresh()

	press(app, placeChord)
	if !app.placeBar.focused() {
		t.Fatalf("%s did not focus the place line", placeChord)
	}
	// The walk opens on the segment the reader is already standing on.
	if got, want := app.placeBar.focus, len(app.placeBar.segs)-1; got != want {
		t.Fatalf("the walk opened on segment %d, want %d", got, want)
	}
}

// ←/→ walk and clamp, enter jumps, esc leaves. The clamp is the rail's own
// refusal to wrap: a cursor that wraps teleports the eye.
func TestTheWalkMovesJumpsAndLeaves(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	app.refresh()
	press(app, placeChord)

	press(app, "left")
	if got := app.placeBar.focus; got != 1 {
		t.Fatalf("left landed on segment %d", got)
	}
	press(app, "left")
	press(app, "left")
	if got := app.placeBar.focus; got != 0 {
		t.Fatalf("the walk did not clamp at the root: %d", got)
	}
	press(app, "right")
	if got := app.placeBar.focus; got != 1 {
		t.Fatalf("right landed on segment %d", got)
	}

	press(app, "enter")
	if app.placeBar.focused() {
		t.Fatal("enter left the keyboard on the line")
	}
	if got := placeWords(app); len(got) != 2 {
		t.Fatalf("enter did not jump to the focused segment: %q", got)
	}

	press(app, placeChord)
	pressThrough(app, "esc")
	if app.placeBar.focused() {
		t.Fatal("esc did not leave the walk")
	}
}

// ESC LEAVES THE WALK BEFORE IT MEANS ANYTHING ELSE, which is rung 2 of the pop
// order: a focus mode is an overlay with no panel.
func TestEscLeavesTheWalkBeforeItPopsAPlace(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	deep := placeWords(app)

	press(app, placeChord)
	pressThrough(app, "esc")
	if got := placeWords(app); len(got) != len(deep) {
		t.Fatalf("leaving the walk also popped a segment: %q -> %q", deep, got)
	}
}

// A key the walk does not claim drops the focus and carries on: once a reader
// starts typing, they have stopped navigating.
func TestTypingLeavesTheWalkAndReachesTheDraft(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	app.refresh()
	press(app, placeChord)

	app.drain(app.key(tea.KeyPressMsg{Code: 'h', Text: "h"}))
	if app.placeBar.focused() {
		t.Fatal("a printable key left the keyboard on the place line")
	}
	if draft := app.composer.Draft(); !strings.Contains(draft, "h") {
		t.Fatalf("the keystroke never reached the draft: %q", draft)
	}
}
