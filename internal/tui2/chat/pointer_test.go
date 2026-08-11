package chat

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The click map, asserted where it is felt: on the panes, through the same
// messages the shell delivers. Every test here answers one question — does
// pointing at this thing do what pressing its key does? — because that is the
// whole of what 5.22's parity clause asks for, and a pointer that did something
// ELSE would be a second product with a second set of laws.

func clickAt(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}
}

func wheelAt(up bool) tea.MouseWheelMsg {
	button := tea.MouseWheelDown
	if up {
		button = tea.MouseWheelUp
	}
	return tea.MouseWheelMsg{Button: button}
}

// -- the map ------------------------------------------------------------------

// 5.15: "j/k (or click) moves the rail selection", and "selection previews;
// enter opens". A click on an unselected row is the first half; a click on the
// selected row is the second.
func TestAClickPreviewsAndASecondClickEnters(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)

	// The row the wisp-parity card is on, found by asking the model rather than
	// by counting lines — the pane's own table is what turns a line into a row,
	// and this test is about what the app does with the answer.
	target := 0
	for i, row := range app.railModel.Rows() {
		if row.Name == "wisp-parity" {
			target = i
		}
	}
	if target == 0 {
		t.Fatal("no wisp-parity row in the fixture")
	}

	app.scopePoint(railPoint{row: target})
	if app.railModel.Cursor() != target {
		t.Fatalf("the first click left the cursor on %d", app.railModel.Cursor())
	}
	if app.railModel.Depth() != 0 {
		t.Fatal("the first click entered the scope; it should only have previewed")
	}
	if !app.railFocus {
		t.Fatal("pointing at the map did not give it the keyboard")
	}

	app.scopePoint(railPoint{row: target})
	if app.railModel.Depth() != 1 {
		t.Fatalf("the second click did not enter: depth %d", app.railModel.Depth())
	}
}

// A click on the ‹ pops, and it is the same call esc makes.
func TestAClickOnTheWayOutPops(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "5")
	press(app, "enter")
	if app.railModel.Depth() != 1 {
		t.Fatal("the fixture did not enter a scope")
	}
	app.scopePoint(railPoint{row: -1, up: true})
	if app.railModel.Depth() != 0 {
		t.Fatalf("the way out left the depth at %d", app.railModel.Depth())
	}
}

// A wheel notch over the map walks the cursor. The rail has no viewport — the
// fold accounts for rows that do not fit — so the only thing a notch can
// honestly move is the selection.
func TestAWheelOverTheMapWalksTheSelection(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)
	before := app.railModel.Cursor()
	app.scope.Mouse(wheelAt(false), image.Point{})
	after := app.railModel.Cursor()
	if after != before+1 {
		t.Fatalf("a notch down moved the cursor %d → %d", before, after)
	}
	app.scope.Mouse(wheelAt(true), image.Point{})
	if got := app.railModel.Cursor(); got != before {
		t.Fatalf("a notch up left the cursor on %d, want %d", got, before)
	}
}

// Hover previews and never commits: the cursor stays, the scope stays, and the
// composer stays bound to whatever it was bound to (5.14).
func TestHoveringTheMapMovesNothing(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)
	cursor, depth, bind := app.railModel.Cursor(), app.railModel.Depth(), app.composerMode()

	for y := range 20 {
		app.scope.Hover(image.Point{X: 2, Y: y}, true)
	}
	app.scope.Hover(image.Point{}, false)

	if app.railModel.Cursor() != cursor || app.railModel.Depth() != depth {
		t.Fatalf("a pointer moved the cursor to %d at depth %d",
			app.railModel.Cursor(), app.railModel.Depth())
	}
	if app.composerMode() != bind {
		t.Fatalf("a pointer rebound the composer to %+v", app.composerMode())
	}
}

// -- the transcript -----------------------------------------------------------

// JOURNEY 6 by pointer: a click on an option row answers the question, through
// the same call the digit makes.
func TestClickingAnOptionRowAnswersTheQuestion(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "which way?", QuestionSeq: 7,
		Options: []store.QuestionOption{
			{Label: "the first way", Value: "first"},
			{Label: "the second way", Value: "second"},
			{Label: "the third way", Value: "third"},
		}})
	poll(t, app)
	frame := ansi.Strip(app.Frame(90, 24))
	if !strings.Contains(frame, "the second way") {
		t.Fatalf("the question did not render its options:\n%s", frame)
	}

	// Find the row the second option is drawn on, in the picture.
	y := -1
	for i, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "the second way") {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("no line carries the second option")
	}

	if cmd := app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y}); cmd == nil {
		t.Fatal("clicking the option produced no answer")
	} else if _, ok := cmd().(postResultMsg); !ok {
		t.Fatal("the answer did not go through the one post door")
	}
	if len(backend.posted) != 1 {
		t.Fatalf("the journal took %d writes", len(backend.posted))
	}
	if got := backend.posted[0].Body; got != "second" {
		t.Fatalf("the click answered %q, want the second option", got)
	}
}

// A click on prose is not an answer. The transcript is a conversation, and a
// surface where clicking anywhere near a question answered it would be
// answering on the reader's behalf.
func TestClickingProseAnswersNothing(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "which way?", QuestionSeq: 7,
		Options: []store.QuestionOption{{Label: "one"}, {Label: "two"}}})
	poll(t, app)
	frame := ansi.Strip(app.Frame(90, 24))

	y := -1
	for i, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "which way?") {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("the question's prose is not on screen")
	}
	if cmd := app.pane.Mouse(clickAt(4, y), image.Point{X: 4, Y: y}); cmd != nil {
		t.Fatal("clicking the question's own words answered it")
	}
	if len(backend.posted) != 0 {
		t.Fatalf("the journal took %d writes", len(backend.posted))
	}
}

// -- the footer ---------------------------------------------------------------

// The row has advertised `? help` since this surface existed. Clicking the
// words opens the door they name.
func TestClickingTheFooterHelpWordsOpensTheSheet(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	const width = 120
	_ = app.Frame(width, 30)

	x := -1
	for _, target := range app.status.bar.Targets(app.status.focusContext(width), width) {
		if target.ID == footer.HelpTarget {
			x = target.From
		}
	}
	if x < 0 {
		t.Fatal("the footer does not offer the help door")
	}
	app.status.Mouse(clickAt(x, 0), image.Point{X: x})
	if app.overlay != overlayCapability {
		t.Fatalf("clicking help left the overlay at %d", app.overlay)
	}
}

// And the footer never takes the cursor. It is chrome: it explains the surface
// and is never talked to (the shell's own focusable list says so).
func TestClickingTheFooterDoesNotMoveTheCursor(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = app.Frame(120, 30)
	before := app.railFocus
	app.status.Mouse(clickAt(0, 0), image.Point{})
	if app.railFocus != before {
		t.Fatal("a footer click moved the keyboard")
	}
}

// -- the composer's two chips -------------------------------------------------

// 5.22 rule 5: the model chip is the affordance for acting on what it shows.
func TestClickingTheModelChipOpensTheModelPalette(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{model: "claude-k3"}, nil)
	const width = 120
	_ = app.Frame(width, 30)

	stack, ok := app.composer.(*composerStack)
	if !ok {
		t.Skip("the composer region is not the stack this test drives")
	}
	from, _, found := stack.meta.chipSpan(width)
	if !found {
		t.Fatal("the meta strip drew no model chip")
	}
	stack.Mouse(clickAt(from, stack.lastHeight-1), image.Point{X: from, Y: stack.lastHeight - 1})
	if app.overlay != overlayModel {
		t.Fatalf("clicking the chip left the overlay at %d", app.overlay)
	}
}

// 5.19: the place line's path is copied by click, the same act `y` performs.
func TestClickingThePlaceLineCopiesTheGround(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = app.Frame(120, 30)
	stack, ok := app.composer.(*composerStack)
	if !ok {
		t.Skip("the composer region is not the stack this test drives")
	}
	want := stack.place.CopyText()
	if want == "" {
		t.Skip("this assembly has no ground to copy")
	}
	cmd := stack.Mouse(clickAt(2, 0), image.Point{X: 2})
	if cmd == nil {
		t.Fatal("clicking the place line copied nothing")
	}
	// The clipboard command is the runtime's own; what this test can assert is
	// that it was produced and that the path it would carry is the ground the
	// place line shows.
	if !strings.Contains(want, "/") {
		t.Fatalf("the ground is not a path: %q", want)
	}
}

// The whole surface, one property: a pointer resting anywhere never changes
// what a key would do. This is 5.14's one cursor, asserted as a sweep rather
// than as four separate claims.
func TestAPointerSweepChangesNoState(t *testing.T) {
	app, _ := boardApp(t)
	const w, h = 120, 30
	before := ansi.Strip(app.Frame(w, h))
	cursor, depth := app.railModel.Cursor(), app.railModel.Depth()
	draft := app.composer.Draft()

	for y := range h {
		for x := 0; x < w; x += 7 {
			app.scope.Hover(image.Point{X: x, Y: y}, true)
			app.pane.Hover(image.Point{X: x, Y: y}, true)
			app.status.Hover(image.Point{X: x, Y: y}, true)
			app.composer.Hover(image.Point{X: x, Y: y}, true)
		}
	}
	app.scope.Hover(image.Point{}, false)
	app.pane.Hover(image.Point{}, false)
	app.status.Hover(image.Point{}, false)
	app.composer.Hover(image.Point{}, false)

	if app.railModel.Cursor() != cursor || app.railModel.Depth() != depth {
		t.Fatalf("a sweep moved the cursor to %d at depth %d",
			app.railModel.Cursor(), app.railModel.Depth())
	}
	if got := app.composer.Draft(); got != draft {
		t.Fatalf("a sweep changed the draft to %q", got)
	}
	if app.overlay != overlayNone {
		t.Fatalf("a sweep raised overlay %d", app.overlay)
	}
	if got := ansi.Strip(app.Frame(w, h)); got != before {
		t.Fatal("a sweep that ended outside every pane left the frame changed")
	}
}

// -- the keyboard follows the pointer -----------------------------------------
//
// 5.14's rule, read for a hand: you talk to what you are looking at, and
// pointing IS looking. 13.14 wired the map's half ("pointing at the map is
// talking to the map") and nothing wired the way back, so custody was a one-way
// door and ctrl+o was the only key out of it. These four tests are the round
// trip, asserted where it is felt — on what a KEYSTROKE does afterwards, never
// on the flag, because the flag was never what the reader complained about.

// clickComposer clicks the first row of the draft, wherever the region's chrome
// has left it, at column x of the draft's own text.
func clickComposer(t *testing.T, app *App, col int) *composerStack {
	t.Helper()
	stack, ok := app.composer.(*composerStack)
	if !ok {
		t.Skip("the composer region is not the stack this test drives")
	}
	top, body := stack.draftBand()
	if body <= 0 {
		t.Fatal("the composer region drew no draft rows")
	}
	// The draft's text starts after the prompt glyph and its space; the pointer
	// asks for a text column and this is where that column is on screen.
	x := composer.TextColumn(stack.lastWidth) + col
	stack.Mouse(clickAt(x, top), image.Point{X: x, Y: top})
	return stack
}

// THE REPORT, verbatim: "clicking on the typing part or anywhere does not seem
// to go there — I have to press ctrl+o."
func TestClickingTheComposerTakesTheKeyboardBackFromTheMap(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)

	press(app, "ctrl+o")
	if !app.railFocus {
		t.Fatal("the fixture did not put the keyboard on the map")
	}

	clickComposer(t, app, 0)
	if app.railFocus {
		t.Fatal("clicking the composer left the keyboard on the map")
	}

	// The assertion that matters is not the flag: it is that the letters of a
	// sentence arrive. j and k are the map's movement keys, so a draft typed at
	// a map that still held the keyboard came out with holes in it — 13.14's
	// "jack knife kayak" → "ac nife aya", the same fault reached by a pointer.
	typeInto(app, "jack knife kayak")
	if got := app.composer.Draft(); got != "jack knife kayak" {
		t.Fatalf("the draft took %q — the map was still eating the keyboard", got)
	}
}

// The transcript is the other half of the conversation side. A click on prose
// answers nothing and folds nothing (the two tests above), and it still settles
// who the reader is talking to.
func TestClickingTheTranscriptTakesTheKeyboardBackFromTheMap(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)
	press(app, "ctrl+o")
	if !app.railFocus {
		t.Fatal("the fixture did not put the keyboard on the map")
	}

	app.pane.Mouse(clickAt(4, 2), image.Point{X: 4, Y: 2})
	if app.railFocus {
		t.Fatal("clicking the transcript left the keyboard on the map")
	}
	typeInto(app, "kg")
	if got := app.composer.Draft(); got != "kg" {
		t.Fatalf("the draft took %q after a click on the conversation", got)
	}
	// And the transcript's own vocabulary is the scroll keys, which the ladder
	// routes here whoever holds custody — so a reader who clicked to read can
	// still read.
	if cmd := app.key(tea.KeyPressMsg{Code: tea.KeyPgUp}); cmd != nil {
		t.Fatal("pgup produced a command; it should only have scrolled")
	}
}

// The map's direction, asserted the same way: after a click, j walks.
func TestClickingARailRowLetsJAndKWalkAtOnce(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)
	if app.railFocus {
		t.Fatal("the fixture opened with the keyboard already on the map")
	}

	app.scopePoint(railPoint{row: 1})
	before := app.railModel.Cursor()
	press(app, "j")
	if app.railModel.Cursor() == before {
		t.Fatal("j did not walk the map after a click on it")
	}
	if got := app.composer.Draft(); got != "" {
		t.Fatalf("j reached the draft as a letter: %q", got)
	}
}

// The one refusal, and it is [handOverTheKeyboard]'s own: a composer that takes
// no draft may not take the keyboard either, or the click would move the cursor
// to a pane that refuses every key and leave j/k walking nothing.
func TestAClickCannotHandTheKeyboardToADisabledComposer(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)
	press(app, "ctrl+o")

	target := -1
	for i, row := range app.railModel.Rows() {
		if row.ID == rowNewRoomID {
			target = i
		}
	}
	if target < 0 {
		t.Fatal("no + new room row in the fixture")
	}
	app.scopePoint(railPoint{row: target})
	if app.composerMode().mode != rail.ComposerDisabled {
		t.Fatalf("previewing + new room left the composer at %v", app.composerMode().mode)
	}

	app.pane.Mouse(clickAt(4, 2), image.Point{X: 4, Y: 2})
	if !app.railFocus {
		t.Fatal("a click handed the keyboard to a composer that refuses every key")
	}
}

// 5.22's parity clause on the caret: a click in the draft puts the caret where
// the finger went, which is what every other text surface on the machine does.
func TestClickingInsideTheDraftPlacesTheCaret(t *testing.T) {
	app, _ := boardApp(t)
	_ = app.Frame(120, 30)
	typeInto(app, "hello world")
	if got := app.composer.Draft(); got != "hello world" {
		t.Fatalf("the fixture draft is %q", got)
	}

	clickComposer(t, app, 5)
	typeInto(app, ",")
	if got := app.composer.Draft(); got != "hello, world" {
		t.Fatalf("the caret landed elsewhere: %q", got)
	}
}

// Nothing above may have needed a colour to be true; this pins that the hover
// tokens exist at all, so a palette rename fails here rather than silently
// painting nothing.
func TestTheHoverTierIsARealToken(t *testing.T) {
	if tokens.Promote(tokens.TextTertiary) != tokens.TextSecondary {
		t.Fatal("the hover promotion no longer climbs the grey ramp")
	}
}
