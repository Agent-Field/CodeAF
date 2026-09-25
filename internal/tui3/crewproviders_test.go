package tui3

import (
	"strings"
	"testing"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/router"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE PROVIDERS ROW, DRIVEN: the chips walked and toggled, the last one on
// refused, a pin left on a provider turned off, the models count following,
// the `+` handing over to /connect, the row read back from the profile and
// undone, the fold at a narrow width, and the list enter opens.

// crewProvidersLab is [crewLab] with four connections: the OpenRouter key, the
// Z.ai coding plan, a local Ollama and a custom endpoint — one of each kind a
// chip says.
func crewProvidersLab(t *testing.T) (*app, string) {
	t.Helper()
	a, dir := crewLab(t)
	custom := config.PrepareCustomSource(dir, "http://127.0.0.1:9001/v1", "my-vllm")
	custom.Key, custom.Order = "sk-my-vllm-0123456789", 3
	if err := config.WriteSources(dir, []config.PersistedSource{
		{ID: "z-ai", Written: "z-ai", Key: "zai-key-0123456789", Door: "coding-plan", Order: 1},
		{ID: "ollama", Written: "ollama", Order: 2},
		custom,
	}); err != nil {
		t.Fatal(err)
	}
	return a, dir
}

// crewToProviders opens the panel with the cursor on the providers row.
func crewToProviders(t *testing.T, a *app) {
	t.Helper()
	if !a.crewUI.open {
		typeLine(t, a, "/crew")
	}
	for i := 0; a.crewUI.cursor != crewProviders; i++ {
		if !a.crewUI.open || a.crewUI.view != crewMain || i > crewStops {
			t.Fatalf("the panel never reached the providers row:\n%s", crewScreen(a))
		}
		drive(t, a, key("down"))
	}
}

// crewChipOf is the index of one provider's chip.
func crewChipOf(t *testing.T, a *app, id string) int {
	t.Helper()
	for i, provider := range a.crewUI.providers {
		if provider.ID == id {
			return i
		}
	}
	t.Fatalf("no chip for %s among %+v", id, a.crewUI.providers)
	return -1
}

// ←/→ WALK THE CHIPS AND SPACE TOGGLES THE ONE UNDER THE CURSOR: the row goes
// ○ and dim, wears the tick, offers the undo, and the profile holds the row —
// read back by a panel opened afresh — until z puts it back.
func TestCrewProvidersToggleAndUndo(t *testing.T) {
	a, dir := crewProvidersLab(t)
	crewToProviders(t, a)
	if footer := crewScreen(a); !strings.Contains(footer, crewChipKeys) {
		t.Fatalf("the footer on the providers row does not offer space:\n%s", footer)
	}
	drive(t, a, key("right"))
	if a.crewUI.chip != crewChipOf(t, a, "z-ai") {
		t.Fatalf("→ left the cursor on chip %d", a.crewUI.chip)
	}
	drive(t, a, key(" "))
	if off := config.CrewProvidersOffAt(dir); !off["z-ai"] || len(off) != 1 {
		t.Fatalf("space wrote %v", off)
	}
	row := crewLineWith(t, crewScreen(a), "providers")
	if !strings.Contains(row, a.icon(tokens.GQueued)+" z-ai sub") || !strings.HasSuffix(strings.TrimRight(strings.TrimSuffix(row, "│"), " "), a.icon(tokens.GSettled)) {
		t.Fatalf("the row does not show z-ai off and the tick: %q", row)
	}
	if !strings.Contains(crewScreen(a), crewUndoWord) {
		t.Fatal("a toggle offers no undo")
	}
	// PERSISTED: a panel opened afresh reads it off the profile.
	drive(t, a, key("esc"))
	typeLine(t, a, "/crew")
	if row := crewLineWith(t, crewScreen(a), "providers"); !strings.Contains(row, a.icon(tokens.GQueued)+" z-ai") {
		t.Fatalf("a reopened panel reads %q", row)
	}
	crewToProviders(t, a)
	drive(t, a, key("right"), key(" "), key("z"))
	if off := config.CrewProvidersOffAt(dir); !off["z-ai"] {
		t.Fatalf("undo of turning it back on reads %v", off)
	}
	drive(t, a, key(" "))
	if off := config.CrewProvidersOffAt(dir); len(off) != 0 {
		t.Fatalf("turning the last one back on left %v", off)
	}
	// AND THE FOOTER SAYS SPACE ONLY THERE.
	drive(t, a, key("down"))
	if screen := crewScreen(a); strings.Contains(screen, "space toggle") {
		t.Fatalf("the cap row's footer offers space:\n%s", screen)
	}
}

// THE LAST PROVIDER ON STAYS ON, refused under the rows in the writer's words.
func TestCrewProvidersRefuseTheLastOne(t *testing.T) {
	a, dir := crewLab(t)
	crewToProviders(t, a)
	drive(t, a, key(" "))
	if off := config.CrewProvidersOffAt(dir); len(off) != 0 {
		t.Fatalf("the last provider was turned off: %v", off)
	}
	screen := crewScreen(a)
	if !strings.Contains(screen, config.ErrCrewLastProvider.Error()) {
		t.Fatalf("the refusal was not said:\n%s", screen)
	}
	if strings.Contains(screen, crewUndoWord) {
		t.Fatal("a refused toggle offers an undo")
	}
}

// A PIN WHOSE ONLY PROVIDER IS OFF SAYS SO on its seat, and the models row
// counts only what a provider still on serves.
func TestCrewProvidersOffWarnsThePinAndNarrowsTheCount(t *testing.T) {
	a, dir := crewProvidersLab(t)
	if err := config.SetCrewPin(dir, crewroute.Checker, "z-ai/glm-5.3-flash@z-ai"); err != nil {
		t.Fatal(err)
	}
	crewToProviders(t, a)
	if row := crewLineWith(t, crewScreen(a), "models"); !strings.Contains(row, "(4)") {
		t.Fatalf("the models row reads %q", row)
	}
	drive(t, a, key("right"), key(" "))
	if row := crewLineWith(t, crewScreen(a), "checker"); !strings.Contains(row, crewProviderOff) {
		t.Fatalf("the pin on z-ai does not say its provider is off: %q", row)
	}
	// Z.ai back on, OpenRouter off: only the plan's own model is served.
	drive(t, a, key(" "), key("left"), key(" "))
	if off := config.CrewProvidersOffAt(dir); !off["openrouter"] || len(off) != 1 {
		t.Fatalf("the row reads %v", off)
	}
	screen := crewScreen(a)
	if row := crewLineWith(t, screen, "models"); !strings.Contains(row, "(1)") {
		t.Fatalf("the models row does not count what the providers on serve: %q", row)
	}
	if row := crewLineWith(t, screen, "checker"); strings.Contains(row, crewProviderOff) {
		t.Fatalf("the pin warns with its provider back on: %q", row)
	}
	for _, c := range config.CrewCandidatesAt(dir) {
		for _, r := range c.Routes {
			if r.Provider == "openrouter" {
				t.Fatalf("%s is still routed through openrouter", c.Model.ID)
			}
		}
	}
}

// THE `+` IS /connect: the panel goes and the connect list comes up.
func TestCrewProvidersPlusOpensConnect(t *testing.T) {
	a, _ := crewLab(t)
	a.modelCatalog = []modelsource.Source{{ID: "deepseek", Name: "DeepSeek", Written: "deepseek"}}
	crewToProviders(t, a)
	drive(t, a, key("right"))
	if a.crewUI.chip != len(a.crewUI.providers) {
		t.Fatalf("→ did not reach the + (chip %d)", a.crewUI.chip)
	}
	drive(t, a, key(" "))
	if a.crewUI.open || !a.connPanel.open {
		t.Fatalf("the + left crew open %v, connect open %v", a.crewUI.open, a.connPanel.open)
	}
	// ESC ON /connect COMES BACK to the crew panel, on the providers row.
	drive(t, a, key("esc"))
	if a.connPanel.open || !a.crewUI.open || a.crewUI.cursor != crewProviders || a.crewUI.chip != len(a.crewUI.providers) {
		t.Fatalf("esc on /connect left connect %v, crew %v, cursor %d, chip %d",
			a.connPanel.open, a.crewUI.open, a.crewUI.cursor, a.crewUI.chip)
	}
	// AND /connect opened on its own still just closes.
	drive(t, a, key("esc"))
	a.openConnect()
	drive(t, a, key("esc"))
	if a.connPanel.open || a.crewUI.open {
		t.Fatal("esc on a /connect the crew panel did not open brought the crew panel up")
	}
}

// A NARROW PROVIDERS LIST KEEPS THE STATE WORD: the day goes first, then the
// model count, then the kind, and nothing is cut.
func TestCrewProvidersListNarrow(t *testing.T) {
	for _, width := range []int{60, 44, 34} {
		a, _ := crewProvidersLab(t)
		a.width = width
		crewToProviders(t, a)
		drive(t, a, key("enter"))
		screen := crewScreen(a)
		for _, id := range []string{"openrouter", "z-ai", "ollama", "my-vllm"} {
			line := crewLineWith(t, screen, " "+id+" ")
			if strings.Contains(line, "…") || !(strings.Contains(line, " on ") || strings.Contains(line, " off ")) {
				t.Errorf("at %d columns the %s line reads %q", width, id, line)
			}
		}
		if width == 60 && (strings.Contains(screen, "nothing today") || !strings.Contains(screen, "subscription")) {
			t.Errorf("at 60 columns the day should go first and the kind stay:\n%s", screen)
		}
	}
}

// A SEAT NOTHING ALLOWED CAN SIT IS THE ONE WARNING: the weak-checker gap
// beside it would be the smaller half of the same trouble. And a day of one
// task says `1 task`.
func TestCrewWarningsAndTheDay(t *testing.T) {
	a, dir := crewLab(t)
	if err := config.SetCrewAllowed(dir, "vendor/nothing-here"); err != nil {
		t.Fatal(err)
	}
	record := router.CrewRecord{TaskClass: "bugfix", Seats: map[string]string{"worker": "z-ai/glm-5.3-flash"}}
	router.LogCrewDecision(config.ProfilePath(dir, ""), "crew:one", record, nil)
	router.LogCrewOutcome(config.ProfilePath(dir, ""), "crew:one", record, router.CrewAccepted, 0.01)
	typeLine(t, a, "/crew")
	screen := crewScreen(a)
	if !strings.Contains(screen, "nothing allowed can sit this seat") {
		t.Fatalf("the empty rule is not said on a seat:\n%s", screen)
	}
	if strings.Contains(screen, "strong checker") {
		t.Fatalf("the gap warning stands beside nothing allowed:\n%s", screen)
	}
	if !strings.Contains(screen, "· 1 task") || strings.Contains(screen, "1 tasks") {
		t.Fatalf("one task is not said as one:\n%s", screen)
	}
}

// ENTER ON THE MODELS ROW STEPS IT, as → does, until price or custom, where
// it opens what that answer holds.
func TestCrewEnterStepsTheModelsRow(t *testing.T) {
	a, dir := crewLab(t)
	typeLine(t, a, "/crew")
	drive(t, a, key("down"), key("down"), key("down"), key("enter"))
	if got := config.CrewAllowedAt(dir).String(); got != "open" {
		t.Fatalf("enter on all stepped to %q", got)
	}
	drive(t, a, key("enter"))
	if got := config.CrewAllowedAt(dir).String(); got != "≤1/5" {
		t.Fatalf("enter on open stepped to %q", got)
	}
	drive(t, a, key("enter"))
	if a.crewUI.edit == nil {
		t.Fatal("enter on price did not open its ceiling")
	}
}

// A PRESS ON A CHIP TOGGLES IT; a press on the row's name opens the list.
func TestCrewProvidersTakeThePointer(t *testing.T) {
	a, dir := crewProvidersLab(t)
	typeLine(t, a, "/crew")
	crewClick(t, a, "providers", "ollama")
	if off := config.CrewProvidersOffAt(dir); !off["ollama"] {
		t.Fatalf("a press on the ollama chip wrote %v", off)
	}
	crewClick(t, a, "providers", "providers")
	if a.crewUI.view != crewProviding {
		t.Fatal("a press on the row's name did not open the list")
	}
}

// A NARROW WINDOW FOLDS THE CHIPS INTO THEIR COUNT, and space there opens the
// list rather than toggling a chip nobody can see.
func TestCrewProvidersFoldWhenNarrow(t *testing.T) {
	a, dir := crewProvidersLab(t)
	a.width = 56
	crewToProviders(t, a)
	row := crewLineWith(t, crewScreen(a), "providers")
	if !strings.Contains(row, "4 of 4 on") || strings.Contains(row, "ollama") {
		t.Fatalf("a narrow row reads %q", row)
	}
	drive(t, a, key("right"), key(" "))
	if len(config.CrewProvidersOffAt(dir)) != 0 {
		t.Fatal("space on a folded row toggled a chip")
	}
	if a.crewUI.view != crewProviding {
		t.Fatal("space on a folded row did not open the list")
	}
}

// THE LIST TOGGLES TOO, ticks the line that changed, undoes with z, and esc
// comes back to the row on the provider it was on.
func TestCrewProvidersList(t *testing.T) {
	a, dir := crewProvidersLab(t)
	crewToProviders(t, a)
	drive(t, a, key("enter"))
	if a.crewUI.view != crewProviding {
		t.Fatal("enter on the providers row did not open the list")
	}
	drive(t, a, key("down"), key("down"), key("enter"))
	if off := config.CrewProvidersOffAt(dir); !off["ollama"] {
		t.Fatalf("enter on ollama wrote %v", off)
	}
	screen := crewScreen(a)
	if line := crewLineWith(t, screen, "ollama"); !strings.Contains(line, "off") || !strings.Contains(line, a.icon(tokens.GSettled)) {
		t.Fatalf("the ollama line reads %q", line)
	}
	if !strings.Contains(screen, crewUndoWord) {
		t.Fatalf("the list offers no undo:\n%s", screen)
	}
	drive(t, a, key("z"))
	if off := config.CrewProvidersOffAt(dir); len(off) != 0 {
		t.Fatalf("z in the list left %v", off)
	}
	drive(t, a, key("esc"))
	if a.crewUI.view != crewMain || a.crewUI.cursor != crewProviders || a.crewUI.chip != crewChipOf(t, a, "ollama") {
		t.Fatalf("esc came back to view %v cursor %d chip %d", a.crewUI.view, a.crewUI.cursor, a.crewUI.chip)
	}
}

// THE FREE ROUTES ARE THE LIST'S LAST LINE, off until turned on, with the
// tick and the undo every other change has — and never on the main rows.
func TestCrewProvidersListFreeRoutes(t *testing.T) {
	a, dir := crewLab(t)
	crewToProviders(t, a)
	if strings.Contains(crewScreen(a), crewFreeRoutesWord) {
		t.Fatal("the free routes are drawn on the main rows")
	}
	drive(t, a, key("enter"), key("end"))
	if line := crewLineWith(t, crewScreen(a), crewFreeRoutesWord); !strings.Contains(line, "off · "+crewFreeRoutesWhy) {
		t.Fatalf("the free routes line reads %q", line)
	}
	drive(t, a, key("enter"))
	if !config.CrewFreeRoutesAt(dir) {
		t.Fatal("enter on the free routes did not turn them on")
	}
	drive(t, a, key("z"))
	if config.CrewFreeRoutesAt(dir) {
		t.Fatal("z left the free routes on")
	}
	if strings.Contains(crewLineWith(t, crewScreen(a), crewFreeRoutesWord), " on ") {
		t.Fatal("the line did not come back off")
	}
}

// THE SETTINGS SEATS ROW COUNTS THE PROVIDERS ON.
func TestSettingsSeatsRowCountsProviders(t *testing.T) {
	a, dir := crewProvidersLab(t)
	if err := config.SetCrewProviderOn(dir, "ollama", false); err != nil {
		t.Fatal(err)
	}
	a.openSettings()
	item := a.sheet.crewDoorItem(nil)
	if !strings.Contains(item.crewValue, "3 of 4 providers") {
		t.Fatalf("the seats row reads %q", item.crewValue)
	}
}

// ── the seat list's filter ──────────────────────────────────────────────────

// A NAME IS FOUND FROM ITS FRONT: a prefix outranks a word inside, which
// outranks anywhere inside, and a row the letters only match scattered is
// left out while anything matched better.
func TestCrewSeatFilterRanksByTier(t *testing.T) {
	a, _ := crewLab(t)
	rows := config.CrewCatalog()
	for _, id := range []string{"x-ai/grok-imagine-2", "krea/krea-2-medium", "google/gemma-4", "z-ai/glm-5.3", "mistral/deepnote-0"} {
		rows = append(rows, modelcatalog.Model{ID: id, PromptPrice: 1e-7, CompletionPrice: 1e-7, Parameters: []string{"tools"}})
	}
	config.CrewCatalog = func() []modelcatalog.Model { return rows }
	cases := []struct {
		typed string
		first []string
		never []string
	}{
		{"kim", []string{"moonshotai/kimi-k3"}, []string{"grok-imagine", "krea-2-medium"}},
		{"glm", []string{"glm-5.3"}, []string{"gemma-4"}},
		{"deep", []string{"deepseek/deepseek-v4-flash", "mistral/deepnote-0"}, nil},
	}
	for _, tc := range cases {
		typeLine(t, a, "/crew")
		drive(t, a, key("enter"))
		typeText(t, a, tc.typed)
		k := a.crewUI.pick
		var got []string
		for _, at := range k.hits {
			got = append(got, k.rows[at].offer.Model.ID)
		}
		for i, want := range tc.first {
			if i >= len(got) || !strings.Contains(got[i], want) {
				t.Errorf("%q ranks %v, want %v first", tc.typed, got, tc.first)
				break
			}
		}
		for _, id := range got {
			for _, never := range tc.never {
				if strings.Contains(id, never) {
					t.Errorf("%q lists %s among %v", tc.typed, id, got)
				}
			}
		}
		drive(t, a, key("esc"), key("esc"))
	}
}
