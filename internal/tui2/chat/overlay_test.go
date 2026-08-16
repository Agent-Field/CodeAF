package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
)

// The catalog's promise, checked against the catalog.
//
// 5.22 renders six surfaces FROM one registry so discoverability "holds by
// construction and can never drift". The drift it cannot prevent on its own is
// the one a user found by clicking: a row that renders perfectly and then does
// nothing. These tests are the missing half — they walk every row the surfaces
// can show and require it to be a door or a sentence, never silence.

// Every registry row a reader can reach either runs or says why. This is the
// test that makes runEntry's default branch safe: a new row with neither an
// executor nor a reason fails here, in the package that owns both.
func TestEveryReachableRowIsADoorOrASentence(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)

	for _, entry := range registry.ForScope(registry.ScopeThread) {
		if entry.ID == "key.quit" || entry.ID == "slash.quit" {
			// Quitting is a door, and driving it here would end the test binary
			// rather than the surface. It is covered by the id list below.
			continue
		}
		reason := app.entryReason(entry.ID)
		cmd := app.runEntry(entry.ID)
		if cmd == nil && reason == "" && !ranWithoutACommand(entry.ID) {
			t.Errorf("%s (%q) neither ran nor said why — a row that answers a "+
				"click with silence is the failure 5.22 exists to prevent",
				entry.ID, entry.Verb)
		}
		if cmd != nil && reason != "" {
			t.Errorf("%s both ran and refused: %q", entry.ID, reason)
		}
	}
}

// ranWithoutACommand names the verbs whose whole effect is on this side of the
// wire — moving the keyboard, opening the map, raising an overlay. They return
// no tea.Cmd because there is nothing for the runtime to do, which is a real
// answer and not a silent one, so they are named here rather than made to
// return a do-nothing command.
func ranWithoutACommand(id string) bool {
	switch id {
	case "slash.settings", "slash.help", "slash.model", "slash.graph",
		"slash.tasks", "slash.self", "slash.notebook", "slash.memory",
		"slash.standing", "slash.history", "slash.budget",
		"key.palette", "key.place-thread", "key.place-board", "key.place-line",
		"key.thread.receipts", "key.thread.clear-draft", "key.threads":
		return true
	}
	return false
}

// Every slash alias in the registry resolves back to an entry this surface
// knows. The slash line is a VIEW of the catalog (5.22 rule 3), so an alias the
// executor has never heard of would be a command the reader can type and the
// product cannot perform.
func TestEverySlashAliasResolvesToAnEntry(t *testing.T) {
	for _, entry := range registry.All() {
		if entry.Slash == "" {
			continue
		}
		found, ok := registry.BySlash(entry.Slash)
		if !ok || found.ID != entry.ID {
			t.Fatalf("/%s resolved to %q (ok=%v), want %s",
				entry.Slash, found.ID, ok, entry.ID)
		}
	}
}

// The three doors that were the user's actual report: choosing a row in the
// palette or the `?` sheet must do the thing, not close the sheet politely.
func TestChoosingASheetRowOpensTheDoorItNames(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)

	if cmd := app.choose(palette.RunEntry{ID: "slash.settings"}); cmd == nil && app.overlay != overlaySettings {
		t.Fatal("choosing the settings row opened nothing")
	}
	app.closeOverlay()

	app.choose(palette.RunEntry{ID: helpEntryID})
	if app.overlay != overlayCapability {
		t.Fatalf("choosing the help row left the overlay at %d", app.overlay)
	}
	app.closeOverlay()

	// The map, which is the verb a reader most often looks for and which had no
	// executor at all before.
	app.railFocus = false
	app.choose(palette.RunEntry{ID: "slash.tasks"})
	if !app.railFocus {
		t.Fatal("choosing 'focus tasks' left the keyboard on the composer")
	}
}

// A home that is still a RAIL SCOPE is reachable by its own name even while the
// group that holds it is collapsed — which is the default, so the row would
// otherwise be a name for a place the jump cannot find.
//
// The notebook is deliberately not the row under test any more: §7 made it a
// PAGE, so `/notebook` swaps the lens rather than descending the rail (see
// [App.showPage] and board_test.go's tab-swap round trip). The two homes that
// have not moved yet still take this door, and this is them.
func TestASlashHomeOpensTheCollapsedGroupOnTheWay(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	if app.source == nil {
		t.Skip("no scope source in this assembly")
	}
	app.source.homes.Expanded = false

	app.choose(palette.RunEntry{ID: "slash.self"})
	if !app.source.homes.Expanded {
		t.Fatal("the group stayed shut, so the jump had nothing to find")
	}
	if got := app.railModel.Selected().ID; got != homes.HomeSelf.ScopeID() {
		t.Fatalf("the cursor landed on %q, want the self room", got)
	}
}

// And the notebook's own row swaps the lens instead, from the palette exactly as
// from the tab — one executor, three hands on it (5.22).
func TestASlashNotebookSwapsToTheNotebookPage(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	app.choose(palette.RunEntry{ID: placeNotebookID})
	if app.page != pageNotebook {
		t.Fatalf("/notebook left the window on the %s page", app.page)
	}
}

// A refused row's sentence is shown INSTEAD of its accelerator, so the sentence
// has to read as a reason and not as a verb (5.20 rule 3).
func TestARefusedRowSaysWhyInWords(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	for _, id := range []string{
		"slash.node", "slash.open", "slash.session", "slash.cancel",
		"key.thread.cycle-focus", "key.thread.newline",
		"key.thread.narrow-split", "key.thread.widen-split",
		"key.voice", "key.boost",
	} {
		reason := app.entryReason(id)
		if reason == "" {
			t.Fatalf("%s is refused with no reason", id)
		}
		if reason != strings.ToLower(reason[:1])+reason[1:] {
			t.Fatalf("%s: a reason is a sentence fragment, not a title: %q", id, reason)
		}
		if app.runEntry(id) != nil {
			t.Fatalf("%s both refused and ran", id)
		}
	}
}
