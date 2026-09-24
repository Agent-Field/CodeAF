package tui3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
)

// codexListingProfile connects Codex in a fresh profile against a listing that
// answers the given rows, the way a finished browser sign-in does.
func codexListingProfile(t *testing.T, rows []any) string {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": rows})
	}))
	t.Cleanup(backend.Close)
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	dir := t.TempDir()
	if _, err := config.ConnectCodex(context.Background(), dir, codexauth.Tokens{
		AccessToken: "surface-window-access", RefreshToken: "surface-window-refresh", IDToken: "surface-window-identity",
		AccountID: "acct-surface-window", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestW4ASurfaceWithNoShelfSwitchesOntoCodexWithItsWindowAfterASignIn(t *testing.T) {
	// W4 (#1383): the engine road's surface has no process shelf behind it, so
	// the rows a Codex sign-in hands it are the rows a later switch reads. Kept
	// as the outcome's bare ids they carried no window, the switch told nobody,
	// and the status line kept the previous model's figure.
	dir := codexListingProfile(t, []any{
		map[string]any{"slug": "gpt-5.5", "visibility": "list", "context_window": 272000},
		map[string]any{"slug": "gpt-5.6-sol", "visibility": "list", "context_window": 272000},
	})
	agent := &fakeAgent{model: "deepseek/deepseek-v4-flash"}
	a := newTestAppWithProfile(dir, agent)
	a.ctxWindow = 1300000
	listed := modelsFromListedIDs([]string{"gpt-5.5", "gpt-5.6-sol"})
	rows := codexConnectedModels(dir, listed)
	if !everyRowHasAWindow(rows) {
		t.Fatalf("the rows a sign-in hands the surface = %+v, want each with its window", rows)
	}
	a.reloadModelSources()
	a.sourceModels["codex"] = cleanModels(rows)
	a.switchModel("codex/gpt-5.5", 0)
	if a.ctxWindow != 272000 || agent.window != 272000 {
		t.Fatalf("after switching onto codex/gpt-5.5 the surface shows %d and told the session %d, want 272000", a.ctxWindow, agent.window)
	}
	// AND THE LIST OUTLIVES THIS PROCESS: the cache the next launch reads was
	// written on the way back, with the windows in it.
	service, _ := a.sources.ByID("codex")
	a.forgetModelList("codex", service.Address)
	a.modelLists.learn(modelCacheNameFor("codex", service.Address))
	if cached := a.cachedModelsFor("codex", service.Address); !everyRowHasAWindow(cached) {
		t.Fatalf("the Codex list cached for the next launch = %+v", cached)
	}
}

func TestW6ASurfaceOpeningAPreFixCodexProfileReadsTheWindow(t *testing.T) {
	// W6 (#1383): a profile whose Codex catalog was remembered with no windows,
	// and which has no Codex list for this surface at all, is read on open with
	// the fallback's observed figure — so a switch onto codex/gpt-5.5 says 272k
	// without a reconnect.
	dir := codexListingProfile(t, []any{
		map[string]any{"slug": "gpt-5.5", "visibility": "list"},
	})
	agent := &fakeAgent{model: "deepseek/deepseek-v4-flash"}
	a := newTestAppWithProfile(dir, agent)
	a.ctxWindow = 1300000
	a.reloadModelSources()
	a.learnModelLists()
	a.switchModel("codex/gpt-5.5", 0)
	if a.ctxWindow != codexauth.FallbackContextWindow || agent.window != codexauth.FallbackContextWindow {
		t.Fatalf("a pre-fix profile's switch shows %d and told the session %d, want %d", a.ctxWindow, agent.window, codexauth.FallbackContextWindow)
	}
}

// everyRowHasAWindow reports whether a list is non-empty and every row in it
// says how many tokens its model accepts.
func everyRowHasAWindow(models []Model) bool {
	for _, model := range models {
		if model.ContextLength <= 0 {
			return false
		}
	}
	return len(models) > 0
}

func TestW6ALaterListingsTrueWindowReplacesTheHealedFigureOnTheNextOpen(t *testing.T) {
	// W6 (#1383): the surface's Codex list is a copy of the catalog. A profile
	// healed to the fallback's 272k must read a DIFFERENT true figure once a
	// later listing — taken by any door, the terminal's included, which rewrites
	// the catalog and never this surface's copy — remembers one. And an open
	// with nothing changed writes nothing.
	dir := codexListingProfile(t, []any{
		map[string]any{"slug": "gpt-5.5", "visibility": "list"},
	})
	first := newTestAppWithProfile(dir, &fakeAgent{model: "deepseek/deepseek-v4-flash"})
	first.reloadModelSources()
	first.learnModelLists()
	service, ok := first.sources.ByID("codex")
	if !ok {
		t.Fatal("codex did not resolve")
	}
	if rows := first.cachedModelsFor("codex", service.Address); len(rows) != 1 || rows[0].ContextLength != codexauth.FallbackContextWindow {
		t.Fatalf("the healed list = %+v, want gpt-5.5 at the fallback's figure", rows)
	}
	// A later listing says 400000 for the same model.
	if err := modelcatalog.Remember(config.CatalogOptionsFor(service, dir), []modelcatalog.Model{{ID: "gpt-5.5", ContextLength: 400000, PriceUnknown: true}}); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{model: "deepseek/deepseek-v4-flash"}
	second := newTestAppWithProfile(dir, agent)
	second.ctxWindow = 1300000
	second.reloadModelSources()
	second.learnModelLists()
	second.switchModel("codex/gpt-5.5", 0)
	if second.ctxWindow != 400000 || agent.window != 400000 {
		t.Fatalf("after the listing changed the switch shows %d and told the session %d, want 400000", second.ctxWindow, agent.window)
	}
	// Nothing changed since: the next open leaves the file alone.
	path := ModelCachePathFor("codex", service.Address)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	past := before.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	third := newTestAppWithProfile(dir, &fakeAgent{})
	third.reloadModelSources()
	third.learnModelLists()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(past) {
		t.Fatalf("an open with the list already in step rewrote it (%v → %v)", past, after.ModTime())
	}
}
