package chat

import (
	"image"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// §6's sidebar in THREE positions: the column, the handle, and nothing.
//
// The history behind this file is two overcorrections. It opened always-open,
// and a reader said so twice — "still in chat I see side rail" — because
// everything the column carried had got a better home in the meantime. So it
// became a drawer, shut at launch and remembered only for the session, and that
// overshot the other way: a window with no column beside it has to TELL the
// reader in prose that they have conversations and running work, where a column
// that is simply there says it in furniture.
//
// The middle rung is what lets the preference be persisted honestly. Open
// teaches; the handle keeps the one thing a hidden rail cannot say — something
// landed where you were not looking — for one column; hidden is for the reader
// who has decided. Nothing ever re-opens it on their behalf.
//
// The tests read the FRAME rather than [App.railState] wherever they can,
// because the flag is not what the reader is looking at.

// railSentinel is a row ONLY the open sidebar ever draws.
//
// It used to be a room's NAME from the fixture, on the reasoning that the rail
// is the only surface that draws the room list. The chats wave ended that: the
// bar row's title chip names the thread this window is in, permanently, which is
// the same string — so a frame with the drawer shut still carried it and every
// drawer test read as a failure.
//
// It was then the `+ new` door, "the rail's own affordance and nothing else in
// the product prints it" — and the overview prints it now, at the foot of its
// own threads section, which is the whole point of that section. So the sentinel
// moved again, to the one row of the rail that no page draws: the collapsed
// homes lid. It is chosen for exactly the property the two previous choices
// lost — nothing else on any frame says it — and if a page ever grows one, this
// is the comment that says to move it again rather than to loosen the test.
const railSentinel = homes.GroupWord

// railOnFrame reports whether the sidebar's own rows are drawn.
func railOnFrame(app *App, width, height int) bool {
	return strings.Contains(ansi.Strip(app.Frame(width, height)), railSentinel)
}

// A first-run window OPENS, on every page that can carry a column. The rail
// teaches by existing, and a reader who has never said anything about it has not
// asked for a window that explains itself in prose instead.
func TestTheRailOpensOnAFirstRunWindow(t *testing.T) {
	for _, target := range []page{pageThread, pageNotebook} {
		app := pageApp(t)
		app.showPage(target)
		if !railOnFrame(app, 120, 24) {
			t.Fatalf("the %s page opened with no sidebar:\n%s",
				target, ansi.Strip(app.Frame(120, 24)))
		}
		if app.shell.RailHidden() {
			t.Fatalf("the %s page took the rail off the frame", target)
		}
	}
	// The work page is the one that cannot have one: the board IS this list at
	// page altitude, and two copies of one list on one screen is §15's
	// same-fact-twice — which is also why it must not overwrite the preference.
	app := pageApp(t)
	app.showPage(pageBoard)
	if railOnFrame(app, 120, 24) {
		t.Fatal("the work page drew a sidebar beside its own list")
	}
	if app.railState != tui2.RailOpen {
		t.Fatalf("the work page rewrote the preference to %v", app.railState)
	}
}

// The chord walks ONE RING, and every position on it is reachable from every
// other: open (with the keyboard elsewhere) → the map → the handle → nothing →
// open. A ring with a rung you can only leave is a preference the reader can
// get stuck in.
func TestTheChordWalksTheRingOfThreeStates(t *testing.T) {
	app := pageApp(t)
	if app.railState != tui2.RailOpen {
		t.Fatalf("the fixture did not open: %v", app.railState)
	}

	press(app, "ctrl+o") // the keyboard, onto the map
	if !app.railFocus {
		t.Fatal("the first press did not hand the map the keyboard")
	}
	if app.railState != tui2.RailOpen {
		t.Fatalf("walking onto the map collapsed the rail to %v", app.railState)
	}

	press(app, "ctrl+o") // put it away, one step
	if app.railState != tui2.RailSlim {
		t.Fatalf("the second press left the rail at %v, want slim", app.railState)
	}
	if railOnFrame(app, 120, 24) {
		t.Fatalf("the handle still drew the map's rows:\n%s", ansi.Strip(app.Frame(120, 24)))
	}
	if app.railFocus {
		t.Fatal("the collapsed rail kept the keyboard")
	}
	if !app.composer.(*composerStack).draft.Focused() {
		t.Fatal("collapsing the rail did not give the mouth its keyboard back")
	}

	press(app, "ctrl+o") // all the way
	if app.railState != tui2.RailHidden {
		t.Fatalf("the third press left the rail at %v, want hidden", app.railState)
	}

	press(app, "ctrl+o") // and back, with the keyboard
	if app.railState != tui2.RailOpen {
		t.Fatalf("the fourth press left the rail at %v, want open", app.railState)
	}
	if !railOnFrame(app, 120, 24) {
		t.Fatal("the ring did not come back round to a drawn map")
	}
	if !app.railFocus {
		t.Fatal("the rail came back without the keyboard")
	}
}

// TestTheChordStillWalksBackToAnOpenMap is the rung that survives the collapse
// states.
//
// Entering a room hands the keyboard to that room's composer while the map
// stays open (rooms.go's handOverTheKeyboard). Collapsing this case into "put it
// away" would answer "let me go back to the map" by closing the map, which is
// the exact defect the chord's own comment records from a wave ago.
func TestTheChordStillWalksBackToAnOpenMap(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	if !app.railFocus {
		t.Fatal("the chord did not put the keyboard on the map")
	}
	// The keyboard leaves the map without the map leaving the frame.
	app.focusScope(false)
	if app.railState != tui2.RailOpen {
		t.Fatalf("the fixture collapsed the rail to %v", app.railState)
	}

	press(app, "ctrl+o")
	if !app.railFocus {
		t.Fatal("the chord collapsed an open map instead of walking back to it")
	}
	if app.railState != tui2.RailOpen {
		t.Fatalf("the chord collapsed the rail it was asked to step into: %v", app.railState)
	}
}

// Esc puts the rail away ONE STEP, to the handle. It is a retreat and not a
// decision — the reader is going back to the conversation, not declaring they
// never want the sidebar — and the handle is that distinction made visible.
func TestEscCollapsesToTheHandleFromTheMapsRoot(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	if !railOnFrame(app, 120, 30) {
		t.Fatal("the fixture did not draw the map")
	}
	// Descend, so the first esc has something to pop and the second reaches the
	// root — esc is a way OUT before it is a way to put things away.
	press(app, "enter")
	if app.railModel.Depth() == 0 {
		t.Skip("the fixture's first row does not descend")
	}
	press(app, "esc")
	if !railOnFrame(app, 120, 30) {
		t.Fatal("esc collapsed the rail while it still had a scope to pop")
	}
	press(app, "esc")
	if app.railState != tui2.RailSlim {
		t.Fatalf("esc at the map's root left the rail at %v, want slim", app.railState)
	}
}

// The rung is REMEMBERED: across page swaps within the window, and on disk for
// the next one. A preference a person expresses with a keystroke and finds
// thrown away at the next launch is a preference the product did not take
// seriously.
func TestTheRungIsRememberedAcrossPageSwapsAndWrittenDown(t *testing.T) {
	var written []string
	app := pageAppRail(t, "", func(state string) { written = append(written, state) })

	press(app, "ctrl+o") // keyboard onto the map
	press(app, "ctrl+o") // collapse to the handle
	if app.railState != tui2.RailSlim {
		t.Fatalf("the fixture is at %v", app.railState)
	}
	if len(written) == 0 || written[len(written)-1] != "slim" {
		t.Fatalf("the collapse was not written down: %v", written)
	}

	for _, target := range []page{pageBoard, pageNotebook, pageThread} {
		app.showPage(target)
		if railOnFrame(app, 120, 24) {
			t.Fatalf("the %s page re-opened the rail on its own", target)
		}
		if target != pageBoard && app.railState != tui2.RailSlim {
			t.Fatalf("the %s page moved the rung to %v", target, app.railState)
		}
	}

	// And the next window opens where this one was left. This is the read half
	// of the same pair: the entry point hands the persisted word back in.
	next := pageAppRail(t, written[len(written)-1], nil)
	if next.railState != tui2.RailSlim {
		t.Fatalf("a window told %q opened at %v", written[len(written)-1], next.railState)
	}
	if railOnFrame(next, 120, 24) {
		t.Fatal("the remembered handle opened as a column")
	}
}

// An unknown word — and the empty string a window that has never been told
// anything gets — opens. Nobody has collapsed it.
func TestAnUnknownRungOpens(t *testing.T) {
	for _, word := range []string{"", "   ", "sideways", "OPEN"} {
		if got := railStateOf(word); got != tui2.RailOpen {
			t.Fatalf("%q resolved to %v", word, got)
		}
	}
	if got := railStateOf("SLIM"); got != tui2.RailSlim {
		t.Fatalf("slim is case sensitive: %v", got)
	}
}

// TestTheDockSaysWhatTheCollapsedRailHolds: the counts the bar row carries while
// the column is away — hidden or slim, because a handle carries one dot and
// cannot carry a count.
func TestTheDockSaysWhatTheCollapsedRailHolds(t *testing.T) {
	app, _ := boardApp(t)
	if app.dockCounts().Shown {
		t.Fatal("the dock stood beside an open rail")
	}

	press(app, "ctrl+o")
	press(app, "ctrl+o") // to the handle
	dock := app.dockCounts()
	if !dock.Shown {
		t.Fatal("a collapsed rail draws no dock")
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
}

// TestTheWorkPageDrawsNoDock: the dock says "there is a list you cannot see",
// and on the work page the list is the whole lens.
func TestTheWorkPageDrawsNoDock(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "ctrl+o")
	if !app.dockCounts().Shown {
		t.Fatal("the fixture has no dock to lose")
	}
	app.showPage(pageBoard)
	if app.dockCounts().Shown {
		t.Fatal("the work page collapsed its own list into a dock beside itself")
	}
}

// 5.22's parity clause on the dock: pointing at it does what pressing the chord
// does, because it IS the chord's own function.
func TestClickingTheDockWalksTheSameRing(t *testing.T) {
	app, _ := boardApp(t)
	const width, height = 120, 30
	press(app, "ctrl+o")
	press(app, "ctrl+o")
	press(app, "ctrl+o") // hidden, so the dock is on the row and the ring reopens
	_ = app.Frame(width, height)

	ctx := app.status.focusContext(width)
	at := -1
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
		t.Fatalf("clicking the dock did not bring the rail back:\n%s",
			ansi.Strip(app.Frame(width, height)))
	}
	if !app.railFocus {
		t.Fatal("the rail opened by pointer did not take the keyboard")
	}
}

// A click ON THE HANDLE expands it, and it may not do anything else. The
// handle's whole content is an invitation — a dot saying something landed — and
// answering it by taking the last of the rail away would be the affordance lying
// at the one moment it had something to say.
func TestClickingTheHandleExpandsIt(t *testing.T) {
	app, _ := boardApp(t)
	const width, height = 120, 30
	press(app, "ctrl+o")
	press(app, "ctrl+o")
	if app.railState != tui2.RailSlim {
		t.Fatalf("the fixture is at %v", app.railState)
	}
	_ = app.Frame(width, height)
	if !app.railSlim() {
		t.Fatal("the solved frame did not draw the handle")
	}

	// Anywhere on the column: it is one cell wide and the thing on it is at most
	// one cell, so a target a reader had to hit exactly would be no target.
	for _, y := range []int{0, height / 2} {
		app, _ := boardApp(t)
		press(app, "ctrl+o")
		press(app, "ctrl+o")
		_ = app.Frame(width, height)
		if cmd := app.scope.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, image.Pt(0, y)); cmd != nil {
			cmd()
		}
		if app.railState != tui2.RailOpen {
			t.Fatalf("a click at row %d left the rail at %v", y, app.railState)
		}
		if !railOnFrame(app, width, height) {
			t.Fatalf("a click at row %d did not draw the map", y)
		}
	}
}

// A width too narrow for the column FORCES the handle, and does not touch the
// stored preference: a reader who half closes their terminal has not changed
// their mind about the sidebar. Widening it brings the column back with no
// keystroke at all.
func TestANarrowWidthForcesTheHandleWithoutOverwritingThePreference(t *testing.T) {
	var written []string
	app := pageAppRail(t, "open", func(state string) { written = append(written, state) })

	if got := ansi.Strip(app.Frame(80, 24)); strings.Contains(got, railSentinel) {
		t.Fatalf("80 columns drew the whole column:\n%s", got)
	}
	if !app.railSlim() {
		t.Fatal("80 columns did not fall back to the handle")
	}
	if app.railState != tui2.RailOpen {
		t.Fatalf("the width rewrote the preference to %v", app.railState)
	}
	if len(written) != 0 {
		t.Fatalf("the width was persisted as a choice: %v", written)
	}

	if !railOnFrame(app, 120, 24) {
		t.Fatal("widening the terminal did not bring the column back")
	}
}

// Linear gets nothing that is not linear (10.1.5): no column, no handle, and a
// chord that does not pretend otherwise.
func TestLinearModeHasNoRailAtAll(t *testing.T) {
	app := New(Options{
		Backend:   pageBackend(t),
		Commander: logCommander{&fakeCommander{model: "anthropic/claude-k3"}},
		Session:   testSession,
		Profile:   tokens.NoColor,
		Linear:    true,
		Now:       fixedNow,
		PollEvery: time.Millisecond,
	})
	poll(t, app)
	for _, width := range []int{80, 120, 200} {
		if got := ansi.Strip(app.Frame(width, 24)); strings.Contains(got, railSentinel) {
			t.Fatalf("linear at %d columns drew a rail:\n%s", width, got)
		}
	}
	before := app.railState
	press(app, "ctrl+o")
	if app.railState != before {
		t.Fatalf("the chord moved a rail linear mode does not have: %v", app.railState)
	}
	if app.railFocus {
		t.Fatal("linear mode handed the keyboard to a rail that is not drawn")
	}
}

// TestEnteringARoomWorksWithTheRailCollapsed: the palette's route into a room
// does not go through the sidebar, so collapsing the sidebar must not close it —
// and arriving in a room must not re-open it.
func TestEnteringARoomWorksWithTheRailCollapsed(t *testing.T) {
	app, _ := boardApp(t)
	press(app, "ctrl+o")
	press(app, "ctrl+o")
	press(app, "ctrl+o")
	if app.railState != tui2.RailHidden {
		t.Fatalf("the fixture is at %v", app.railState)
	}
	before := app.railModel.Selected().Name
	if cmd := app.choose(palette.JumpToRoom{ID: rowTaskPrefix + "job-1"}); cmd != nil {
		cmd()
	}
	if got := app.railModel.Selected().Name; got == before || got != "wisp-parity" {
		t.Fatalf("the jump landed on %q", got)
	}
	if app.railState != tui2.RailHidden {
		t.Fatal("entering a room re-opened the sidebar")
	}
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

// railBreakpointSanity keeps the narrow tests above honest about which side of
// the breakpoint they are on — a fixture that drifted to the wrong side of it
// would pass by measuring nothing.
func TestRailBreakpointSanity(t *testing.T) {
	if tokens.RailAtWidth <= 60 {
		t.Fatalf("60 columns is no longer narrow (breakpoint %d)", tokens.RailAtWidth)
	}
	if tui2.DefaultMetrics().RailBreakpoint <= 60 {
		t.Fatalf("the layout's own breakpoint is %d", tui2.DefaultMetrics().RailBreakpoint)
	}
	if tokens.RailSlimWidth < 1 || tokens.RailSlimWidth >= tokens.RailWidth {
		t.Fatalf("the handle is %d columns", tokens.RailSlimWidth)
	}
}

// pageAppRail is [pageApp] with the sidebar's persisted rung and its writer
// wired, which is the pair the entry point supplies (cmd/aforge/chatv2_rail.go).
func pageAppRail(t *testing.T, rung string, save func(string)) *App {
	t.Helper()
	app := New(Options{
		Backend:   pageBackend(t),
		Commander: logCommander{&fakeCommander{model: "anthropic/claude-k3"}},
		Session:   testSession,
		Profile:   tokens.NoColor,
		Now:       fixedNow,
		PollEvery: time.Millisecond,
		Rail:      rung,
		SaveRail:  save,
	})
	poll(t, app)
	return app
}
