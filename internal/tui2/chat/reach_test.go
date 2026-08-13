package chat

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
)

// REACHABILITY. The switcher was unreachable in the shipped build — the chord
// was eaten by the terminal, the title chip is absent until the scribe has named
// the conversation, and nothing else on the frame said the feature existed. The
// tests here are the ones that would have caught it: every door asserted as the
// event a real terminal sends, and asserted to be VISIBLE and not merely bound.

// -- the chords ----------------------------------------------------------------

// Both spellings open the switcher, each driven as the exact event Bubble Tea
// v2 builds for it: the letter in Code, the modifier in Mod, and Text empty.
func TestBothChordsOpenTheSwitcher(t *testing.T) {
	for _, chord := range []string{threadsCtrl, threadsChord} {
		t.Run(chord, func(t *testing.T) {
			app, _ := threadsApp(t)
			_ = app.Frame(120, 30)
			msg, ok := chordKey(chord)
			if !ok {
				t.Fatalf("%q is not a chord this test can build", chord)
			}
			if cmd := app.drain(app.key(msg)); cmd != nil {
				_ = cmd()
			}
			if app.overlay != overlayThreads {
				t.Fatalf("%s raised overlay %d, want the switcher", chord, app.overlay)
			}
		})
	}
}

// THE DEFECT ITSELF, PINNED.
//
// A macOS terminal does not send Option+t as alt+t: Option is a compose key, and
// Terminal.app and iTerm2 both deliver the precomposed glyph `†` — one printable
// rune, no modifier bit, Text filled in. Bubble Tea's Key.String() returns Text
// when Text is set, so the surface is handed "†" and every alt binding on the
// row is silently unreachable.
//
// The assertion is in two halves, and the second is the one that makes the
// first safe: the glyph must not open the switcher (it is not that key), and it
// must reach the DRAFT as ordinary text (it is a character the reader typed, and
// swallowing it would be the composer losing input). This is why the door could
// not stay chord-only, and why ctrl+t exists beside it.
func TestTheOptionComposedGlyphIsTextAndNeverADoor(t *testing.T) {
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)
	// Exactly what a macOS terminal sends for Option+t.
	dagger := tea.KeyPressMsg{Code: '†', Text: "†"}
	if got := dagger.String(); got != "†" {
		t.Fatalf("Option+t stringifies as %q; this test is asserting the wrong event", got)
	}
	if cmd := app.drain(app.key(dagger)); cmd != nil {
		_ = cmd()
	}
	if app.overlay == overlayThreads {
		t.Fatal("the composed glyph opened the switcher; it is not that key")
	}
	if draft := app.composer.Draft(); !strings.Contains(draft, "†") {
		t.Fatalf("the composed glyph never reached the draft: %q", draft)
	}
}

// THE `?` SHEET MAY ONLY TEACH A KEY THAT FIRES. The registry is what the sheet
// and the palette are drawn from, so the chord it records for the switcher has
// to be a chord this surface actually answers — asserted by pressing whatever
// the catalog says, rather than by comparing two spellings.
func TestTheCatalogTeachesAChordThatActuallyOpensTheSwitcher(t *testing.T) {
	entry, ok := registry.ByID(threadsEntryID)
	if !ok {
		t.Fatalf("the catalog has no %q row", threadsEntryID)
	}
	taught := entry.KeyOn(registry.SurfaceComposerFirst)
	if taught == "" {
		t.Fatal("the catalog teaches no chord for the switcher in a composer-first room")
	}
	msg, ok := chordKey(taught)
	if !ok {
		t.Fatalf("the catalog teaches %q, which is not a chord a terminal can send", taught)
	}
	app, _ := threadsApp(t)
	_ = app.Frame(120, 30)
	if cmd := app.drain(app.key(msg)); cmd != nil {
		_ = cmd()
	}
	if app.overlay != overlayThreads {
		t.Fatalf("the catalog teaches %q and pressing it raised overlay %d", taught, app.overlay)
	}
}

// -- the visible doors ---------------------------------------------------------

// doorTarget finds one door on the bar and reports the column it starts at.
func doorTarget(t *testing.T, app *App, width int, id string) int {
	t.Helper()
	for _, target := range app.status.bar.Targets(app.status.focusContext(width), width) {
		if target.ID == id {
			return target.From
		}
	}
	return -1
}

// THE DOORS ARE ON THE FRAME EVEN WHEN NOTHING ELSE IS. A window whose thread
// the scribe has not named yet draws NO title chip — that is 5.3's silence rule
// and it is correct — so on a first run the chip cannot be the door. The words
// beside the tabs are, and they are there whatever the conversation is called.
func TestTheBarOffersTheThreadDoorsEvenWithNoTitleChip(t *testing.T) {
	app, _ := threadsApp(t)
	app.source.titles = map[string]string{}
	const width = 120
	frame := ansi.Strip(app.Frame(width, 30))
	if name := app.threadName(app.session); name != "" {
		t.Fatalf("this fixture has a named thread (%q); it cannot test the unnamed case", name)
	}
	if !strings.Contains(frame, threadsDoorWord) {
		t.Fatalf("the bar offers no threads door:\n%s", frame)
	}
	for _, id := range []string{footer.ThreadsDoorTarget, footer.NewThreadTarget} {
		if at := doorTarget(t, app, width, id); at < 0 {
			t.Fatalf("%s is drawn but is not a click target", id)
		}
	}
}

// PARITY, both doors. The pointer reaches exactly what the keyboard reaches,
// through the same call — 5.22 rule 5's "the row IS the button" read onto the
// bar.
func TestTheThreadDoorsAgreeWithTheKeyboard(t *testing.T) {
	const width = 120

	t.Run("threads opens the switcher", func(t *testing.T) {
		app, _ := threadsApp(t)
		_ = app.Frame(width, 30)
		at := doorTarget(t, app, width, footer.ThreadsDoorTarget)
		if at < 0 {
			t.Fatal("the threads door is not a click target")
		}
		if cmd := app.status.Mouse(clickAt(at, 0), image.Point{X: at}); cmd != nil {
			_ = cmd()
		}
		if app.overlay != overlayThreads {
			t.Fatalf("clicking the threads door raised overlay %d", app.overlay)
		}
	})

	t.Run("+ mints a thread", func(t *testing.T) {
		app, backend := threadsApp(t)
		_ = app.Frame(width, 30)
		at := doorTarget(t, app, width, footer.NewThreadTarget)
		if at < 0 {
			t.Fatal("the + door is not a click target")
		}
		cmd := app.status.Mouse(clickAt(at, 0), image.Point{X: at})
		if cmd == nil {
			t.Fatal("clicking + produced no command")
		}
		msg, ok := cmd().(roomOpenedMsg)
		if !ok {
			t.Fatalf("clicking + yielded %T", cmd())
		}
		if msg.err != nil {
			t.Fatalf("minting failed: %v", msg.err)
		}
		if len(backend.opened) != 1 {
			t.Fatalf("the store took %d opens, want exactly one", len(backend.opened))
		}
	})
}

// A door is offered where it means something and nowhere else. The work and
// notebook lenses are not conversations, and a thread door drawn over them would
// be offering an act about a thing the reader is not looking at.
func TestTheThreadDoorsAreDrawnOnlyInTheChat(t *testing.T) {
	app, _ := threadsApp(t)
	const width = 120
	if got := len(app.doors()); got != 2 {
		t.Fatalf("the chat offers %d doors, want both", got)
	}
	for _, target := range []page{pageBoard, pageNotebook} {
		app.setPage(target)
		app.refresh()
		if got := len(app.doors()); got != 0 {
			t.Fatalf("the %s page offers %d thread doors", target, got)
		}
		frame := ansi.Strip(app.Frame(width, 30))
		if strings.Contains(frame, threadsDoorWord) {
			t.Fatalf("the %s page drew a threads door:\n%s", target, frame)
		}
	}
}

// THE DOORS YIELD BEFORE THE WINDOW'S CAPABILITY HONESTY DOES. They are the
// point of this wave and they still lose the last cells on the row: a `visitor`
// notice says this window cannot stop the turn it is watching, and 5.20 does not
// let a shortcut push that off the frame. Asserted as the ladder rather than at
// one width — the doors survive a wide row and are gone from a cramped one.
func TestTheDoorsShedBeforeTheStandingFacts(t *testing.T) {
	app, _ := threadsApp(t)
	if wide := ansi.Strip(app.Frame(120, 30)); !strings.Contains(wide, threadsDoorWord) {
		t.Fatalf("the doors are missing from a wide row:\n%s", wide)
	}
	narrow := ansi.Strip(app.Frame(46, 20))
	if strings.Contains(narrow, threadsDoorWord) {
		t.Fatalf("the doors survived a row too narrow to carry them:\n%s", narrow)
	}
	// And what a row that narrow keeps is where the reader is.
	if !strings.Contains(narrow, "chat") {
		t.Fatalf("a narrow row dropped the place the reader is in:\n%s", narrow)
	}
}

// -- every advertised chord ----------------------------------------------------

// NO DEAD DOORS ON THE FIRST FRAME.
//
// alt+g, alt+1, alt+2 and alt+3 were all declared in the catalog and drawn on
// the empty first frame, and none of them reached anything: the key ladder had
// no arm for any of them. Two of the three doors the product showed a new reader
// did nothing at all.
//
// This walks the CATALOG rather than a list of four spellings, so the next row
// that promises a chord is covered the day it is added. A row the room has an
// honest reason against is skipped — the sheet draws it disabled, and it is not
// supposed to fire.
func TestEveryChordTheCatalogPromisesActuallyDoesSomething(t *testing.T) {
	for _, entry := range registry.ForScope(registry.ScopeThread) {
		key := entry.KeyOn(registry.SurfaceComposerFirst)
		if key == "" || !strings.Contains(key, "+") {
			continue
		}
		msg, ok := chordKey(key)
		if !ok {
			// A named-key chord (ctrl+space, alt+enter): not something this
			// helper synthesizes, and each has its own test already.
			continue
		}
		t.Run(entry.ID+" "+key, func(t *testing.T) {
			app, _ := threadsApp(t)
			_ = app.Frame(120, 30)
			if reason := app.entryReason(entry.ID); reason != "" {
				t.Skipf("this room honestly cannot do it: %s", reason)
			}
			// ROUTED, not necessarily VISIBLE. Some of these acts are honest
			// no-ops in the state a fresh window is in — alt+1 asks for the
			// lens already on screen, ctrl+u clears a draft that is empty — and
			// asserting a state change would be asserting that the fixture is
			// in the right mood rather than that the door is wired. What the
			// dead keys had in common is that NOTHING claimed them, so that is
			// what is asserted: the registry arm takes it, or the explicit
			// ladder above it does something with it.
			if _, claimed := app.registryKey(key); claimed {
				return
			}
			before := surfaceMark(app)
			cmd := app.drain(app.key(msg))
			if cmd != nil {
				_ = cmd()
			}
			if cmd == nil && surfaceMark(app) == before {
				t.Fatalf("%s promises %s and nothing in the key ladder claims it", entry.ID, key)
			}
		})
	}
}

// surfaceMark is enough of the window's state to notice that a key moved it:
// which lens is up, which overlay is raised, and where the keyboard is.
func surfaceMark(app *App) [4]int {
	focus, shown := 0, 0
	if app.railFocus {
		focus = 1
	}
	if app.railShown {
		shown = 1
	}
	return [4]int{int(app.page), int(app.overlay), focus, shown}
}
