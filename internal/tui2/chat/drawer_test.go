package chat

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// §6's sidebar as a DRAWER: shut on every page, opened by the chord or by the
// dock, shut by the chord or by esc, and remembered for the session.
//
// THE DEFECT, twice reported: "still in chat I see side rail". Everything the
// column carried has a better home now — work and notebook are full pages, the
// palette searches every room — so an always-open copy of one of those lists is
// §15's same-fact-twice standing permanently in a quarter of the window.
//
// The tests read the FRAME rather than [App.railShown], because the flag is not
// what the reader is complaining about.

// railSentinel is a row ONLY the sidebar ever draws.
//
// It used to be a room's NAME from the fixture, on the reasoning that the rail
// is the only surface that draws the room list. The chats wave ended that: the
// bar row's title chip names the thread this window is in, permanently, which is
// the same string — so a frame with the drawer shut still carried it and every
// drawer test read as a failure. The `+ new room` door has no such twin: it is
// the rail's own affordance and nothing else in the product prints it.
const railSentinel = "+ new room"

// railOnFrame reports whether the sidebar's own rows are drawn.
func railOnFrame(app *App, width, height int) bool {
	return strings.Contains(ansi.Strip(app.Frame(width, height)), railSentinel)
}

// TestTheDrawerIsShutOnEveryPage is the reported defect, one assertion per page.
func TestTheDrawerIsShutOnEveryPage(t *testing.T) {
	for _, target := range []page{pageThread, pageBoard, pageNotebook} {
		app := pageApp(t)
		app.showPage(target)
		if railOnFrame(app, 120, 24) {
			t.Fatalf("the %s page opened with the sidebar standing:\n%s",
				target, ansi.Strip(app.Frame(120, 24)))
		}
		if !app.shell.RailHidden() {
			t.Fatalf("the %s page left the rail on the frame", target)
		}
	}
}

// TestTheChordOpensTheDrawerAndTheChordShutsIt: no new key was invented. The
// chord that has always meant "let me talk to the map" is the one that produces
// a map to talk to.
func TestTheChordOpensTheDrawerAndTheChordShutsIt(t *testing.T) {
	app := pageApp(t)
	if railOnFrame(app, 120, 24) {
		t.Fatal("the drawer was open before anything was pressed")
	}

	press(app, "ctrl+o")
	if !railOnFrame(app, 120, 24) {
		t.Fatalf("the chord did not open the drawer:\n%s", ansi.Strip(app.Frame(120, 24)))
	}
	// It arrives with the keyboard: a column that opened and then had to be
	// reached with a second chord would be two gestures for one intent.
	if !app.railFocus {
		t.Fatal("the drawer opened without the keyboard")
	}

	press(app, "ctrl+o")
	if railOnFrame(app, 120, 24) {
		t.Fatalf("the chord did not shut the drawer:\n%s", ansi.Strip(app.Frame(120, 24)))
	}
	if app.railFocus {
		t.Fatal("the shut drawer kept the keyboard")
	}
	// And the keyboard is back where a person types.
	if !app.composer.(*composerStack).draft.Focused() {
		t.Fatal("shutting the drawer did not give the mouth its keyboard back")
	}
}

// TestTheChordStillWalksBackToAnOpenMap is the rung that survives the drawer.
//
// Entering a room hands the keyboard to that room's composer while the map
// stays open (rooms.go's handOverTheKeyboard). Collapsing this case into "shut
// it" would answer "let me go back to the map" by closing the map, which is the
// exact defect the chord's own comment records from a wave ago.
func TestTheChordStillWalksBackToAnOpenMap(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	if !app.railFocus {
		t.Fatal("the chord did not open the drawer onto the map")
	}
	// The keyboard leaves the map without the map leaving the frame.
	app.focusScope(false)
	if !app.railShown {
		t.Fatal("the fixture shut the drawer")
	}

	press(app, "ctrl+o")
	if !app.railFocus {
		t.Fatal("the chord shut an open map instead of walking back to it")
	}
	if !app.railShown {
		t.Fatal("the chord shut the drawer it was asked to step into")
	}
}

// TestEscShutsTheDrawerFromItsRoot: esc means "put this away" everywhere else
// in the product. A rail that stayed standing after esc had nothing left to pop
// would be the one surface where it meant "look elsewhere".
func TestEscShutsTheDrawerFromItsRoot(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	if !railOnFrame(app, 120, 30) {
		t.Fatal("the fixture did not open the drawer")
	}
	// Descend, so the first esc has something to pop and the second reaches the
	// root — esc is a way OUT before it is a way to put things away.
	press(app, "enter")
	if app.railModel.Depth() == 0 {
		t.Skip("the fixture's first row does not descend")
	}
	press(app, "esc")
	if !railOnFrame(app, 120, 30) {
		t.Fatal("esc shut the drawer while it still had a scope to pop")
	}
	press(app, "esc")
	if railOnFrame(app, 120, 30) {
		t.Fatalf("esc at the map's root left the drawer standing:\n%s",
			ansi.Strip(app.Frame(120, 30)))
	}
}

// TestTheDrawerIsRememberedAcrossPageSwaps: the state is the session's, so a
// reader who opened the map and stepped over to the notebook finds it where
// they left it — and the work page, which cannot have one, does not forget it.
func TestTheDrawerIsRememberedAcrossPageSwaps(t *testing.T) {
	app := pageApp(t)
	press(app, "ctrl+o")
	if !railOnFrame(app, 120, 24) {
		t.Fatal("the fixture did not open the drawer")
	}

	app.showPage(pageBoard)
	if railOnFrame(app, 120, 24) {
		t.Fatal("the work page drew a sidebar beside its own list")
	}
	app.showPage(pageNotebook)
	if !railOnFrame(app, 120, 24) {
		t.Fatalf("the notebook page forgot the open drawer:\n%s",
			ansi.Strip(app.Frame(120, 24)))
	}
	app.showPage(pageThread)
	if !railOnFrame(app, 120, 24) {
		t.Fatal("the thread forgot the open drawer")
	}

	// And a shut drawer stays shut across the same trip. Shutting it takes two
	// presses here and that is the ladder working: the trip through the work
	// page took the keyboard off the map (nothing undrawn may hold it), so the
	// first press walks back to the map and the second puts it away.
	press(app, "ctrl+o")
	press(app, "ctrl+o")
	if app.railShown {
		t.Fatal("two presses did not shut the drawer")
	}
	for _, target := range []page{pageBoard, pageNotebook, pageThread} {
		app.showPage(target)
		if railOnFrame(app, 120, 24) {
			t.Fatalf("the %s page opened the drawer on its own", target)
		}
	}
}

// TestTheDockSaysWhatTheShutDrawerHolds: the counts the bar row carries while
// the column is away, and the silence it keeps when there is nothing to carry.
func TestTheDockSaysWhatTheShutDrawerHolds(t *testing.T) {
	app, _ := boardApp(t)
	dock := app.dockCounts()
	if !dock.Shown {
		t.Fatal("a shut drawer draws no dock")
	}
	if dock.Working <= 0 {
		t.Fatalf("the dock counts %d working jobs on a board with live work", dock.Working)
	}
	// The frame carries it, at the right end of the bar row, where the rail's
	// own column would have been.
	frame := ansi.Strip(app.Frame(120, 30))
	rows := strings.Split(frame, "\n")
	bar := rows[len(rows)-1]
	if !strings.Contains(bar, tokens.GlyphWorking) {
		t.Fatalf("the bar row carries no dock:\n%q", bar)
	}

	// Open the drawer and the dock goes: the list is on screen, so a collapsed
	// copy of it is §15's same fact twice.
	press(app, "ctrl+o")
	if app.dockCounts().Shown {
		t.Fatal("the dock stayed on the row beside the open rail")
	}
}

// TestTheWorkPageDrawsNoDock: the dock says "there is a list you cannot see",
// and on the work page the list is the whole lens.
func TestTheWorkPageDrawsNoDock(t *testing.T) {
	app, _ := boardApp(t)
	if !app.dockCounts().Shown {
		t.Fatal("the fixture starts without a dock")
	}
	app.showPage(pageBoard)
	if app.dockCounts().Shown {
		t.Fatal("the work page collapsed its own list into a dock beside itself")
	}
}

// TestClickingTheDockOpensTheDrawer is 5.22's parity clause on the one door
// this wave added: pointing at it does what pressing the chord does.
func TestClickingTheDockOpensTheDrawer(t *testing.T) {
	app, _ := boardApp(t)
	const width, height = 120, 30
	_ = app.Frame(width, height)

	ctx := app.status.focusContext(width)
	var at = -1
	for _, target := range app.status.bar.Targets(ctx, width) {
		if target.ID == footer.DockTarget {
			at = target.From
		}
	}
	if at < 0 {
		t.Fatalf("the dock is not a target on the row: %+v", app.status.bar.Targets(ctx, width))
	}
	if cmd := app.status.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, image.Pt(at, 0)); cmd != nil {
		cmd()
	}
	if !railOnFrame(app, width, height) {
		t.Fatalf("clicking the dock did not open the drawer:\n%s",
			ansi.Strip(app.Frame(width, height)))
	}
	if !app.railFocus {
		t.Fatal("the drawer opened by pointer did not take the keyboard")
	}
}

// TestANarrowFrameStillReachesTheMapThroughTheDrawer: below the rail's
// breakpoint the map is not a column, it is the PANE (layout.go's Narrow). The
// drawer has to survive that, because a hidden rail there is a rail with no
// fallback at all.
func TestANarrowFrameStillReachesTheMapThroughTheDrawer(t *testing.T) {
	app, _ := boardApp(t)
	const width, height = 60, 24
	shut := ansi.Strip(app.Frame(width, height))
	if strings.Contains(shut, railSentinel) {
		t.Fatalf("a narrow frame opened with the map as its pane:\n%s", shut)
	}

	press(app, "ctrl+o")
	opened := ansi.Strip(app.Frame(width, height))
	if !strings.Contains(opened, railSentinel) {
		t.Fatalf("the chord did not swap the narrow pane to the map:\n%s", opened)
	}
	// The transcript yields rather than shrinking — the map IS the pane here.
	if app.shell.RailHidden() {
		t.Fatal("the narrow frame kept the rail hidden while showing the map")
	}

	press(app, "ctrl+o")
	if back := ansi.Strip(app.Frame(width, height)); strings.Contains(back, railSentinel) {
		t.Fatalf("the chord did not put the narrow map away:\n%s", back)
	}
}

// TestEnteringARoomWorksWithTheDrawerShut: the palette's route into a room does
// not go through the sidebar, so shutting the sidebar must not close it.
func TestEnteringARoomWorksWithTheDrawerShut(t *testing.T) {
	app, _ := boardApp(t)
	if app.railShown {
		t.Fatal("the fixture opened the drawer")
	}
	before := app.railModel.Selected().Name
	if cmd := app.choose(palette.JumpToRoom{ID: rowTaskPrefix + "job-1"}); cmd != nil {
		cmd()
	}
	if got := app.railModel.Selected().Name; got == before || got != "wisp-parity" {
		t.Fatalf("the jump landed on %q", got)
	}
	if app.railShown {
		t.Fatal("entering a room opened the sidebar")
	}
	// The window is usable: the composer holds the keyboard and the frame draws.
	if app.railFocus {
		t.Fatal("the keyboard went to a rail that is not on the frame")
	}
	if got := ansi.Strip(app.Frame(120, 30)); got == "" {
		t.Fatal("the frame after a jump is empty")
	}
}

// TestTheHiddenRailNeverBecomesTheMainPane: layout.go's narrow fallback swaps
// the main pane to the rail, and §6's hidden form must not be swapped INTO —
// there would be nothing to fall back to and the swap would draw the duplicate
// the hiding prevented.
func TestTheHiddenRailNeverBecomesTheMainPane(t *testing.T) {
	app, _ := boardApp(t)
	app.showPage(pageBoard)
	// The board hides the rail unconditionally; asking for scope must not put a
	// rail that is off the frame into the lens.
	app.setScope(true)
	frame := ansi.Strip(app.Frame(60, 24))
	if strings.Contains(frame, railSentinel) {
		t.Fatalf("a narrow board swapped its lens for a hidden rail:\n%s", frame)
	}
	if !app.shell.RailHidden() {
		t.Fatal("the board put the rail back on the frame")
	}
}

// railBreakpointSanity keeps the two narrow tests above honest about which side
// of the breakpoint they are on — a fixture that drifted to the wrong side of it
// would pass by measuring nothing.
func TestRailBreakpointSanity(t *testing.T) {
	if tokens.RailAtWidth <= 60 {
		t.Fatalf("60 columns is no longer narrow (breakpoint %d)", tokens.RailAtWidth)
	}
	if tui2.DefaultMetrics().RailBreakpoint <= 60 {
		t.Fatalf("the layout's own breakpoint is %d", tui2.DefaultMetrics().RailBreakpoint)
	}
}
