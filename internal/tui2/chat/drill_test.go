package chat

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/settings"
)

// THE PATH, ACROSS DOORS — the wiring half of internal/tui2/palette/trail.go.
//
// Two user reports converge here. The first: reaching settings THROUGH the
// palette left the reader with no way back, because the sheet was never joined
// to the drill grammar the palette already had. The second: clicking a model row
// showed "some weird lists like voice, architect, skeptic" — the picker opened
// on the ROLE list when the reader had already named a role.
//
// The components hold words and never learn what they open, so this file is
// where a word becomes a door again, and these are the tests for that.

// escKey and backKey are the two keys the grammar turns on.
func escKey() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func backKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

// richCommander is a head this window can actually ask things of: the settings
// registry the sheet renders, and the daily-cached OpenRouter read the picker
// draws. The bare fakeCommander answers neither, which is its own honest case
// and is what the degradation tests below want.
type richCommander struct {
	fakeCommander
	registry *config.Settings
	models   []catalog.Model
}

func (c *richCommander) Settings() *config.Settings {
	if c.registry == nil {
		c.registry = config.NewSettings(config.SettingsOptions{})
	}
	return c.registry
}

func (c *richCommander) CatalogModels() []catalog.Model { return c.models }

// drilled opens the palette and chooses one of its rows, which is the only way
// a door gets a path above it.
func drilled(t *testing.T, app *App, result palette.Result) {
	t.Helper()
	app.openPalette()
	app.chooseDrilled(result)
}

// THE KEYBOARD ROUND TRIP. Palette → settings → back, and the way back is
// backspace on an empty query, exactly as it is one level inside the palette.
func TestThePaletteDrillsIntoSettingsAndBackspaceReturns(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)

	drilled(t, app, palette.RunEntry{ID: settings.EntryID})
	if app.overlay != overlaySettings {
		t.Fatalf("the palette's settings row left the overlay at %d", app.overlay)
	}
	if got := app.settings.Trail(); len(got) != 1 || got[0] != palette.RootWord {
		t.Fatalf("the sheet wears the path %v, want the palette's one word", got)
	}

	app.overlayKey(backKey())
	if app.overlay != overlayPalette {
		t.Fatalf("backspace on the drilled sheet left the overlay at %d, want the palette", app.overlay)
	}
}

// THE POINTER ROUND TRIP. The trail word is the door a reader with a mouse has,
// and it was the one the report said did nothing.
func TestClickingTheTrailWordReturnsToThePalette(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	drilled(t, app, palette.RunEntry{ID: settings.EntryID})

	// The frame is what records where the words landed; a hit test against a
	// path nobody drew would be arithmetic rather than a click.
	_ = frame(app)
	header := strings.Split(app.settings.Render(70, 20), "\n")[0]
	at := strings.Index(header, palette.RootWord)
	if at < 0 {
		t.Fatalf("the drilled header does not carry the palette's word: %q", header)
	}
	app.settings.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, image.Pt(at, 0))
	if app.overlay != overlayPalette {
		t.Fatalf("clicking the trail word left the overlay at %d, want the palette", app.overlay)
	}
}

// A RETURN, NOT A FRESH OPEN. The reader's query and their place in the list are
// theirs; a palette that came back blank would have moved them (7.2).
func TestReturningToThePaletteKeepsTheReadersPlace(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	app.openPalette()
	app.palette.Key(tea.KeyPressMsg{Code: 's', Text: "s"})
	app.palette.Key(tea.KeyPressMsg{Code: 'e', Text: "e"})
	want := app.palette.Query()
	if want == "" {
		t.Fatal("the query did not take")
	}

	app.chooseDrilled(palette.RunEntry{ID: settings.EntryID})
	app.overlayKey(backKey())
	if got := app.palette.Query(); got != want {
		t.Fatalf("the palette came back holding %q, want the reader's own %q", got, want)
	}
}

// ESC CLOSES THE WHOLE FLOW, from any rung. It is not the back key with a
// longer reach — 8.2.21 is that esc acts on what you are watching.
func TestEscFromADrilledSheetClosesEverything(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	drilled(t, app, palette.RunEntry{ID: settings.EntryID})

	app.overlayKey(escKey())
	if app.overlay != overlayNone {
		t.Fatalf("esc left the overlay at %d, want the whole flow closed", app.overlay)
	}

	// And esc NEVER ascends a rung, even when it has unsubmitted state to drop
	// first. The sheet's own ladder is about what is typed on this screen, not
	// about where the screen sits: a search is cleared, and the next esc closes
	// everything — neither one goes back to the palette.
	drilled(t, app, palette.RunEntry{ID: settings.EntryID})
	app.overlayKey(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if app.settings.Query() == "" {
		t.Fatal("the query did not take")
	}
	app.overlayKey(escKey())
	if app.overlay != overlaySettings || app.settings.Query() != "" {
		t.Fatalf("esc on a searched sheet left the overlay at %d with query %q",
			app.overlay, app.settings.Query())
	}
	app.overlayKey(escKey())
	if app.overlay != overlayNone {
		t.Fatalf("the second esc left the overlay at %d, want everything closed", app.overlay)
	}
}

// A sheet opened on its own has no path and answers backspace the way it always
// did: the back grammar is a property of how you GOT here.
func TestASheetOpenedOnItsOwnHasNoWayBackAndNeedsNone(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	app.openSettings()

	if got := app.settings.Trail(); len(got) != 0 {
		t.Fatalf("the standalone sheet wears the path %v, want none", got)
	}
	app.overlayKey(backKey())
	if app.overlay != overlaySettings {
		t.Fatalf("backspace on a standalone sheet moved the overlay to %d", app.overlay)
	}
	app.overlayKey(escKey())
	if app.overlay != overlayNone {
		t.Fatal("esc did not close the standalone sheet")
	}
}

// The `?` sheet is raised OVER the room rather than navigated to, so a door it
// opens has nowhere to go back to that esc is not already.
func TestTheCapabilitySheetOpensSettingsWithNoPathAboveIt(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	app.openCapability()
	app.choose(palette.RunEntry{ID: settings.EntryID})

	if app.overlay != overlaySettings {
		t.Fatalf("the `?` sheet's settings row left the overlay at %d", app.overlay)
	}
	if got := app.settings.Trail(); len(got) != 0 {
		t.Fatalf("the sheet wears the path %v, want none", got)
	}
}

// THE SEAM, CLOSED. Picking a setting from the palette takes you TO it, not to
// the top of a sheet you then have to search a second time.
func TestChoosingASettingLandsOnThatRow(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &richCommander{}, nil)
	_ = frame(app)
	drilled(t, app, palette.OpenSetting{Key: config.KeyDailyBudget})

	if app.overlay != overlaySettings {
		t.Fatalf("the settings row left the overlay at %d", app.overlay)
	}
	got, ok := app.settings.Selected()
	if !ok || got != config.KeyDailyBudget {
		t.Fatalf("the band is on %q (ok=%v), want %q", got, ok, config.KeyDailyBudget)
	}
	if trail := app.settings.Trail(); len(trail) != 1 {
		t.Fatalf("the sheet wears the path %v, want the palette's one word", trail)
	}
}

// ONE CLICK, ONE LIST. A settings row naming one slot opens that slot's
// catalog — the models, not the five slot words the reader was shown instead.
func TestASettingsModelRowOpensThatRolesCatalog(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	app.openSettings()
	app.openModelSlot(settings.ModelMsg{Slot: "plan"})

	if app.overlay != overlayModel {
		t.Fatalf("the model row left the overlay at %d", app.overlay)
	}
	if app.models.Level() != modelui.LevelModels {
		t.Fatalf("the picker opened at level %v, want the role's models", app.models.Level())
	}
	if app.models.Role() != store.RolePlan {
		t.Fatalf("the picker opened role %q, want %q", app.models.Role(), store.RolePlan)
	}
	if got := app.models.Trail(); len(got) != 1 || got[0] != settings.TrailWord {
		t.Fatalf("the picker wears the path %v, want the sheet it came from", got)
	}
}

// A capability slot is not one of the five, so there is no catalog to open at:
// the picker stays on the role list and says why on every row.
func TestACapabilitySlotOpensTheRoleListWithItsReason(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	app.openSettings()
	app.openModelSlot(settings.ModelMsg{Slot: "image"})

	if app.models.Level() != modelui.LevelRoles {
		t.Fatalf("a capability slot opened level %v, want the roles", app.models.Level())
	}
}

// THE WHOLE LADDER, walked back one rung at a time: models → roles → the sheet →
// the palette. One key, one meaning, at four depths across three components.
func TestTheLadderWalksBackOneRungAtATime(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	drilled(t, app, palette.RunEntry{ID: settings.EntryID})
	app.openModelSlot(settings.ModelMsg{Slot: "work"})

	if got := app.models.Trail(); len(got) != 2 ||
		got[0] != palette.RootWord || got[1] != settings.TrailWord {
		t.Fatalf("the picker wears the path %v, want the palette then the sheet", got)
	}

	app.overlayKey(backKey())
	if app.models.Level() != modelui.LevelRoles {
		t.Fatal("the first step back did not leave the models level")
	}
	if app.overlay != overlayModel {
		t.Fatalf("the first step back left the picker entirely (overlay %d)", app.overlay)
	}

	app.overlayKey(backKey())
	if app.overlay != overlaySettings {
		t.Fatalf("the second step back left the overlay at %d, want the sheet", app.overlay)
	}
	if got := app.settings.Trail(); len(got) != 1 || got[0] != palette.RootWord {
		t.Fatalf("the sheet came back wearing %v, want the palette above it", got)
	}

	app.overlayKey(backKey())
	if app.overlay != overlayPalette {
		t.Fatalf("the third step back left the overlay at %d, want the palette", app.overlay)
	}
}

// And esc still ends the whole thing from the deepest rung of that ladder.
func TestEscFromTheDeepestRungClosesEverything(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	drilled(t, app, palette.RunEntry{ID: settings.EntryID})
	app.openModelSlot(settings.ModelMsg{Slot: "work"})

	app.overlayKey(escKey())
	if app.overlay != overlayNone {
		t.Fatalf("esc from the picker left the overlay at %d", app.overlay)
	}
}

// ONE SPELLING, THREE SURFACES. The sheet, the palette that lists its rows, and
// the picker either of them opens all name a slot the same way — and none of
// them names it the way the roles table names it for itself (§14).
//
// `voice` is deliberately NOT scanned for on these two surfaces, and the
// omission is the decision: it is the roles table's word for the conversation
// slot AND the product's true name for the voice modality, which has a real
// settings row of its own. A scan that failed on it would be banning a word for
// what it says somewhere else. modelui's own frame scan covers it, on a surface
// that has no media rows to confuse it with.
func TestNoInventedRoleWordReachesTheSheetOrThePalette(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &richCommander{}, nil)
	_ = frame(app)

	banned := []string{}
	for _, role := range store.ModelRoles() {
		word := role.Word()
		if word == "" || word == modelui.RoleWord(role) || word == "voice" {
			continue
		}
		banned = append(banned, word)
	}
	if len(banned) == 0 {
		t.Fatal("nothing is banned, so this test is asserting nothing")
	}

	app.openSettings()
	sheet := app.settings.Render(100, 40)
	app.closeOverlay()
	app.openPalette()
	list := app.palette.Render(100, 40)

	for where, rendered := range map[string]string{"the sheet": sheet, "the palette": list} {
		for _, word := range banned {
			if strings.Contains(rendered, word) {
				t.Errorf("%s carries the invented role word %q:\n%s", where, word, rendered)
			}
		}
	}
	// And the plain words are actually on the sheet, so a pass cannot be a
	// surface that drew nothing.
	for _, role := range store.ModelRoles() {
		if !strings.Contains(sheet, modelui.RoleWord(role)) {
			t.Errorf("the sheet does not name the %q slot as %q", role, modelui.RoleWord(role))
		}
	}
}

// THE CATALOG ACTUALLY LOADS. The rich read is what puts the price, the window
// and the score on the row, and a picker opened from a settings row must take
// that door like any other.
func TestThePickerFromASettingsRowShowsTheCatalogItWasGiven(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &richCommander{models: []catalog.Model{
		{ID: "anthropic/claude-opus-5", Name: "Claude Opus 5", ContextLength: 200_000,
			PromptPrice: 0.000003, CompletionPrice: 0.000015},
		{ID: "openai/gpt-oss-120b", Name: "gpt-oss-120b", ContextLength: 131_072,
			PromptPrice: 0.0000001, CompletionPrice: 0.0000005},
	}}, nil)
	_ = frame(app)
	app.openSettings()
	app.openModelSlot(settings.ModelMsg{Slot: "work"})

	rendered := app.models.Render(100, 14)
	// The models themselves, in the product's word for them, on the level the
	// one click landed on.
	for _, want := range []string{"claude-opus-5", "gpt-oss-120b"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the picker does not show %q:\n%s", want, rendered)
		}
	}
	// And the raw provider id is nowhere on it (§14, and 5.10's model words).
	if strings.Contains(rendered, "anthropic/") {
		t.Fatalf("the picker rendered a provider id:\n%s", rendered)
	}
}

// AND A CATALOG THAT NEVER ARRIVED IS SAID. The old door answered an empty
// catalog by leaving the reader on the role words, which is what they reported
// seeing instead of models.
func TestAMissingCatalogDegradesToTheHonestSentence(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	_ = frame(app)
	app.openSettings()
	app.openModelSlot(settings.ModelMsg{Slot: "work"})

	rendered := app.models.Render(100, 14)
	if !strings.Contains(rendered, "no models to offer yet") {
		t.Fatalf("an empty catalog did not say so:\n%s", rendered)
	}
	for _, role := range store.ModelRoles() {
		if role == store.RoleWork {
			// The open role names itself on the trail, which is where you are.
			continue
		}
		body := strings.SplitN(rendered, "\n", 2)[1]
		if strings.Contains(body, modelui.RoleWord(role)) {
			t.Errorf("the empty models level is showing the role word %q", modelui.RoleWord(role))
		}
	}
}
