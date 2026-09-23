package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// codexWindowShelf is a process shelf for a profile with the default service
// listing one model at 1.3M and Codex connected with the given listing rows,
// exactly as a process builds it when it reads the profile.
func codexWindowShelf(t *testing.T, listing []any) (*v3ModelShelf, *catalog.Catalog, modelsource.Set, string) {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": listing})
	}))
	t.Cleanup(backend.Close)
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	dir := t.TempDir()
	if _, err := config.ConnectCodex(t.Context(), dir, codexauth.Tokens{
		AccessToken: "window-access", RefreshToken: "window-refresh", IDToken: "window-identity",
		AccountID: "acct-window", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	options := catalog.Options{BaseURL: config.DefaultBaseURL, Dir: dir,
		HTTPClient: shelfRouter(`{"id":"deepseek/deepseek-v4-flash","context_length":1300000}`, nil)}
	launch := catalog.Load(t.Context(), options)
	sources := config.ResolveSources(dir, "", config.DefaultBaseURL)
	shelf := newV3ModelShelf(launch, catalog.Options{BaseURL: config.DefaultBaseURL, Dir: dir})
	shelf.setSources(sources)
	return shelf, launch, sources, dir
}

var codexListing = []any{
	map[string]any{"slug": "gpt-5.5", "visibility": "list", "context_window": 272000},
	map[string]any{"slug": "gpt-5.6-sol", "visibility": "list", "context_window": 272000},
}

func TestW4ASwitchAsksTheModelsOwnServiceForItsWindow(t *testing.T) {
	// W4 (#1383): the session's window on a switch comes from the NEW model's
	// own service. A conversation that started on an OpenRouter model and moved
	// to codex/gpt-5.5 asked OpenRouter's catalog for a Codex id and kept its old
	// figure; the engine road had nothing else, because it ignores the window a
	// surface sends.
	shelf, launch, _, _ := codexWindowShelf(t, codexListing)
	windowFor := v3WindowFor(shelf, launch)
	for model, want := range map[string]int{
		"codex/gpt-5.5":              272000,
		"codex/gpt-5.6-sol":          272000,
		"codex/gpt-5.5:high":         272000,
		"deepseek/deepseek-v4-flash": 1300000,
		"codex/not-listed":           0,
	} {
		if got := windowFor(model); got != want {
			t.Fatalf("window for %s = %d, want %d", model, got, want)
		}
	}
	// And from a conversation that STARTED on Codex, whose own catalog is the
	// Codex one: moving back to the router's model asks the router's rows.
	codexService, _ := config.ResolveSources(shelf.options.Dir, "", config.DefaultBaseURL).ByID("codex")
	fromCodex := v3WindowFor(shelf, catalog.Recall(config.CatalogOptionsFor(codexService, shelf.options.Dir)))
	if got := fromCodex("deepseek/deepseek-v4-flash"); got != 1300000 {
		t.Fatalf("a Codex-started conversation moving to the router's model got %d, want 1300000", got)
	}
}

// coldCatalog is a direct service's catalog at the moment a conversation
// opens: lazily loaded and not warmed yet, so it has no rows to answer from.
type coldCatalog struct{}

func (coldCatalog) ModelsNow() []catalog.Model { return nil }

func TestW5AConversationThatOpensOnCodexStartsWithItsWindow(t *testing.T) {
	// W5 (#1383): a conversation launched on codex/gpt-5.5 is handed 272000 at
	// once, although the Codex catalog it will use is still warming, because the
	// shelf read the remembered rows when the process read the profile.
	shelf, _, _, _ := codexWindowShelf(t, codexListing)
	if got := v3StartWindow(shelf, coldCatalog{}, "codex/gpt-5.5", "gpt-5.5"); got != 272000 {
		t.Fatalf("opening window on codex/gpt-5.5 = %d, want 272000", got)
	}
	// A model neither side knows still answers zero, which the session reads as
	// "keep your own conservative default".
	if got := v3StartWindow(shelf, coldCatalog{}, "codex/not-listed", "not-listed"); got != 0 {
		t.Fatalf("opening window on an unlisted model = %d, want 0", got)
	}
}

func TestW6AProfileConnectedBeforeTheFixReadsTheWindowWithoutReconnecting(t *testing.T) {
	// W6 (#1383): rows remembered by a build that dropped the window are read
	// with the fallback's observed figure for the ids it names; any other id
	// stays unknown rather than being given a guess.
	shelf, launch, sources, dir := codexWindowShelf(t, []any{
		map[string]any{"slug": "gpt-5.5", "visibility": "list"},
		map[string]any{"slug": "gpt-future", "visibility": "list"},
	})
	codexService, _ := sources.ByID("codex")
	for _, row := range catalog.Recall(config.CatalogOptionsFor(codexService, dir)).ModelsNow() {
		if row.ContextLength != 0 {
			t.Fatalf("the fixture is not a pre-fix profile: %+v", row)
		}
	}
	windowFor := v3WindowFor(shelf, launch)
	if got := windowFor("codex/gpt-5.5"); got != codexauth.FallbackContextWindow {
		t.Fatalf("pre-fix gpt-5.5 window = %d, want %d", got, codexauth.FallbackContextWindow)
	}
	if got := windowFor("codex/gpt-future"); got != 0 {
		t.Fatalf("an id the fallback does not name was given %d, want 0", got)
	}
}

func TestW6AReconnectOnTheSameAddressIsReadByARunningProcess(t *testing.T) {
	// W6 (#1383): a Codex compartment is re-read whenever the profile is, so a
	// reconnect — which rewrites the remembered catalog at the SAME address —
	// reaches a running engine's sessions without a restart.
	shelf, launch, sources, dir := codexWindowShelf(t, codexListing)
	codexService, _ := sources.ByID("codex")
	if err := catalog.Remember(config.CatalogOptionsFor(codexService, dir), []catalog.Model{{ID: "gpt-5.5", ContextLength: 400000, PriceUnknown: true}}); err != nil {
		t.Fatal(err)
	}
	shelf.setSources(sources)
	if got := v3WindowFor(shelf, launch)("codex/gpt-5.5"); got != 400000 {
		t.Fatalf("after a reconnect rewrote the catalog the window is %d, want 400000", got)
	}
}

func TestW7AShelfWithNoServicesAnswersFromTheLaunchCatalog(t *testing.T) {
	// W7: the control. A process whose profile names no services answers every
	// model from the rows it launched with, exactly as before.
	options := catalog.Options{BaseURL: config.DefaultBaseURL, Dir: t.TempDir(),
		HTTPClient: shelfRouter(`{"id":"deepseek/deepseek-v4-flash","context_length":1300000}`, nil)}
	launch := catalog.Load(t.Context(), options)
	shelf := newV3ModelShelf(launch, options)
	if got := v3WindowFor(shelf, launch)("deepseek/deepseek-v4-flash"); got != 1300000 {
		t.Fatalf("launch-catalog window = %d, want 1300000", got)
	}
	offline := catalog.Load(t.Context(), catalog.Options{BaseURL: config.DefaultBaseURL, Dir: t.TempDir(),
		HTTPClient: shelfRouter("", errors.New("offline"))})
	if got := v3WindowFor(nil, offline)("deepseek/deepseek-v4-flash"); got != 0 {
		t.Fatalf("with no shelf and no rows the window is %d, want 0", got)
	}
}

func TestW5ACodexConversationsProgramsAreSizedFromItsWindow(t *testing.T) {
	// W5 (#1383): the programs a conversation on codex/gpt-5.5 runs — the
	// generalist executor and every leaf — are sized from the same 272k the
	// conversation is. They used to ask the Codex catalog, spelled in bare ids,
	// about the qualified id and got zero for the life of the conversation.
	shelf, _, sources, dir := codexWindowShelf(t, codexListing)
	codexService, _ := sources.ByID("codex")
	codexCatalog := catalog.Recall(config.CatalogOptionsFor(codexService, dir))
	window := v3StartWindow(shelf, codexCatalog, "codex/gpt-5.5", "gpt-5.5")
	if window != 272000 {
		t.Fatalf("start window = %d, want 272000", window)
	}
	wiring := v3Subharnesses(config.Config{Sources: sources, ProfileDir: dir}, codexCatalog, "codex/gpt-5.5", window, t.TempDir(), nil)
	if wiring.Registry == nil {
		t.Fatal("the subharness registry was not built")
	}
	generalist, ok := wiring.Registry.Generalist().(*exec.Linear)
	if !ok || generalist.ContextLength() != 272000 {
		t.Fatalf("the generalist's window = %v, want 272000", wiring.Registry.Generalist())
	}
	leaf := leafBuild{model: "codex/gpt-5.5", models: codexCatalog, window: window}
	if got := leaf.contextLength(); got != 272000 {
		t.Fatalf("a leaf's window = %d, want 272000", got)
	}
	// The shape the defect had: the catalog asked about the qualified id.
	if got := (leafBuild{model: "codex/gpt-5.5", models: codexCatalog}).contextLength(); got != 0 {
		t.Fatalf("the unhanded leaf now answers %d; the control expects the catalog's own zero for a qualified id", got)
	}
}
