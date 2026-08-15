package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/settings"
)

// The slash surface is a projection, and these are the tests that keep it one.
// Nothing here checks that a particular command exists — the registry decides
// that — only that what the `/` line offers and what it does are the catalog's
// and the executor's, unchanged.

// Every row the line offers is a registry row with an alias, and every registry
// row with an alias is offered. A list that could differ from the catalog in
// either direction is the parallel table 5.22 puts on its kill list.
func TestTheSlashLineIsExactlyTheAliasedCatalog(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)

	want := map[string]string{}
	for _, entry := range registry.ForScope(registry.ScopeThread) {
		if entry.Slash != "" {
			want[entry.ID] = entry.Slash
		}
	}
	if len(want) == 0 {
		t.Fatal("no aliased rows in the catalog; the test asserted nothing")
	}

	got := map[string]string{}
	for _, command := range app.slashCommands() {
		if command.ID == "" || command.Word == "" {
			t.Fatalf("the line offered a nameless row: %+v", command)
		}
		got[command.ID] = command.Word
	}
	if len(got) != len(want) {
		t.Fatalf("the line offers %d rows, the catalog has %d aliases", len(got), len(want))
	}
	for id, alias := range want {
		if got[id] != alias {
			t.Fatalf("%s is /%s in the catalog and /%s on the line", id, alias, got[id])
		}
	}
}

// A row carries the SAME description and the SAME refusal the `?` sheet draws.
// Two surfaces that disagreed about whether a verb works would be worse than
// one surface that never offered it.
func TestASlashRowSaysWhatTheSheetSaysAboutIt(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)

	refusals := 0
	for _, command := range app.slashCommands() {
		entry, ok := registry.ByID(command.ID)
		if !ok {
			t.Fatalf("%s is not a registry row", command.ID)
		}
		if command.Title != entry.Description {
			t.Fatalf("%s reads %q on the line and %q in the catalog",
				command.ID, command.Title, entry.Description)
		}
		if want := app.entryReason(command.ID); command.Reason != want {
			t.Fatalf("%s is refused with %q on the line and %q in the sheet",
				command.ID, command.Reason, want)
		}
		if command.Reason != "" {
			refusals++
		}
	}
	if refusals == 0 {
		t.Fatal("no refused row in the catalog; the agreement was not exercised")
	}
}

// The accelerator a row teaches is the one this surface can actually bind. A
// bare letter is not one — every printable key belongs to the draft here — so
// the line makes registry.Entry.KeyOn's composer-first projection, exactly as
// the palette does.
func TestTheLineTeachesOnlyChordsThisSurfaceCanBind(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	for _, command := range app.slashCommands() {
		entry, _ := registry.ByID(command.ID)
		if want := entry.KeyOn(registry.SurfaceComposerFirst); command.Key != want {
			t.Fatalf("%s teaches %q, want %q", command.ID, command.Key, want)
		}
		if len(command.Key) == 1 {
			t.Fatalf("%s teaches the bare key %q, which lands in the draft here",
				command.ID, command.Key)
		}
	}
}

// Completing a row reaches the one executor. This is the property that makes
// the slash line a third hand on the same door rather than a second door:
// runSlash is runEntry, and the assertion is the observable effect of it.
func TestCompletingASlashRowRunsTheOneExecutor(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)

	app.runSlash(helpEntryID)
	if app.overlay != overlayCapability {
		t.Fatalf("/help left the overlay at %d", app.overlay)
	}
	app.closeOverlay()

	app.railFocus = false
	app.runSlash("slash.tasks")
	if !app.railFocus {
		t.Fatal("/tasks did not put the keyboard on the map")
	}
}

// Typed end to end through the real ladder: the popup opens, narrows, and the
// completion opens the door — no chord, no palette, no second catalog.
//
// `/help` and not `/settings`, and the reason is itself the point: this
// assembly has no engine behind it, so entryReason refuses the settings row and
// the line refuses to run it. A test that drove the refused row would have been
// asserting that a door opens when the surface has already said it cannot.
func TestTypingASlashCommandRunsIt(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)

	for _, key := range []string{"/", "h", "e", "l", "p"} {
		press(app, key)
	}
	if got := app.composer.Draft(); got != "/help" {
		t.Fatalf("the draft reads %q", got)
	}
	if app.composer.HintRows() == 0 {
		t.Fatal("no candidate list under the draft")
	}
	press(app, "enter")
	if app.overlay != overlayCapability {
		t.Fatalf("enter left the overlay at %d", app.overlay)
	}
	if got := app.composer.Draft(); got != "" {
		t.Fatalf("the completed command stayed in the draft as %q", got)
	}
}

// And a row this room cannot run refuses from the line exactly as it refuses
// from the sheet: nothing opens, the draft keeps its words, and the reason is
// still on screen beside the row (5.20 rule 3). Without an engine, /settings is
// that row.
func TestTypingARefusedSlashCommandRefusesInPlace(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	if app.entryReason(settings.EntryID) == "" {
		t.Skip("this assembly can open settings; the refusal is not exercised")
	}
	for _, key := range []string{"/", "s", "e", "t", "t", "i", "n", "g", "s"} {
		press(app, key)
	}
	press(app, "enter")
	if app.overlay != overlayNone {
		t.Fatalf("a refused row opened overlay %d", app.overlay)
	}
	if got := app.composer.Draft(); got != "/settings" {
		t.Fatalf("a refused row changed the draft to %q", got)
	}
}

// And the region grows for the list rather than the list being cut to the one
// spare row the metric table left. A completion showing one candidate out of
// seventeen is a list only in the sense that it is not zero.
func TestTheComposerRegionGrowsForTheList(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	press(app, "/")
	rows := 0
	for _, line := range strings.Split(ansi.Strip(app.Frame(90, 24)), "\n") {
		if strings.Contains(line, "/settings") || strings.Contains(line, "/notebook") {
			rows++
		}
	}
	if rows == 0 {
		t.Fatal("the list drew no candidate rows; the region never grew")
	}
	if app.composer.HintRows() < 2 {
		t.Fatalf("the region asked for %d rows", app.composer.HintRows())
	}
}
