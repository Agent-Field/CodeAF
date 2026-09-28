package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// This follows the ordinary launch builder rather than supplying provider
// callbacks by hand: the missing callbacks were precisely the live failure.
func TestLinkedLocalProviderRefreshReachesConnectedCatalogs(t *testing.T) {
	surfaceProfile, engineProfile := t.TempDir(), t.TempDir()
	t.Setenv("CODEAF_HOME", surfaceProfile)
	t.Setenv("CODEAF_PROFILE_DIR", surfaceProfile)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "synthetic")
	defaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"default-model"}]}`)
	}))
	defer defaultServer.Close()
	t.Setenv("CODEAF_BASE_URL", defaultServer.URL)
	var requests atomic.Int32
	var unavailable atomic.Bool
	customServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if unavailable.Load() {
			http.Error(w, "catalog temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"fixture-model"}]}`)
	}))
	defer customServer.Close()
	if err := config.WriteSources(engineProfile, []config.PersistedSource{{ID: "custom-invoice", Written: "invoice-catalog", Address: customServer.URL, Key: "synthetic", Order: 1}}); err != nil {
		t.Fatal(err)
	}
	fleet := onePipeFleet("", hostedClient(t))
	t.Cleanup(fleet.closeAll)
	t.Cleanup(func() { stopPoolErrands(surfaceProfile) })
	welcome := remote.Welcome{Version: remote.Version, Workspace: t.TempDir(), ProfileDir: engineProfile}
	options, settings, shelf := hostOptionsWithShelf(fleet, welcome, false)
	if options.RefreshAllModels != nil || options.WarmEmptyProviders != nil || options.SubscribeServiceModels != nil {
		t.Fatal("remote builder exposed local provider operations")
	}
	localDoors(&options, welcome, settings)
	apply := options.ApplyModelSources
	var applied bool
	options.ApplyModelSources = func(sources modelsource.Set) {
		applied = true
		apply(sources)
	}
	localProviderModels(&options, shelf)
	if options.RefreshAllModels == nil || options.RefreshModelsForService == nil || options.WarmEmptyProviders == nil || options.SubscribeServiceModels == nil || options.ModelsForService == nil || options.ProviderFetchError == nil {
		t.Fatal("ordinary launch still lacks provider catalog operations")
	}
	if shelf.options.Dir != engineProfile {
		t.Fatalf("catalog profile = %q, want engine profile %q", shelf.options.Dir, engineProfile)
	}
	service, ok := options.Sources.ByID("custom-invoice")
	if !ok {
		t.Fatal("engine profile's custom provider absent")
	}
	var notices atomic.Int32
	remove := options.SubscribeServiceModels(func(id, address string) {
		if id == service.Source.ID {
			notices.Add(1)
		}
	})
	options.WarmEmptyProviders(t.Context())
	if requests.Load() != 1 || notices.Load() != 1 {
		t.Fatalf("cold warm requests=%d notices=%d", requests.Load(), notices.Load())
	}
	if rows := options.ModelsForService(service); len(rows) != 1 || rows[0].ID != "fixture-model" {
		t.Fatalf("warm rows=%+v", rows)
	}
	options.WarmEmptyProviders(t.Context())
	if requests.Load() != 1 {
		t.Fatal("warm provider fetched again on cold-only pass")
	}
	unavailable.Store(true)
	options.RefreshAllModels(t.Context())
	if requests.Load() != 2 || notices.Load() != 2 {
		t.Fatalf("refresh did not reach failed custom catalog: requests=%d notices=%d", requests.Load(), notices.Load())
	}
	if options.ProviderFetchError(service.Source.ID) == "" {
		t.Fatal("failed listing has no error state")
	}
	if rows := options.ModelsForService(service); len(rows) != 1 {
		t.Fatalf("failure lost cached rows: %+v", rows)
	}
	unavailable.Store(false)
	options.RefreshAllModels(t.Context())
	if requests.Load() != 3 || notices.Load() != 3 || options.ProviderFetchError(service.Source.ID) != "" {
		t.Fatal("successful retry did not replace failure state and notify")
	}
	remove()
	options.RefreshAllModels(t.Context())
	if requests.Load() != 4 || notices.Load() != 3 {
		t.Fatal("unsubscribed surface still received catalog notifications")
	}
	// A profile update must remove a disconnected provider from the same shelf
	// the picker reads, while preserving the engine's original update callback.
	initialDefault := options.Sources.All()[0]
	options.ApplyModelSources(modelsource.NewSet(initialDefault))
	if !applied {
		t.Fatal("provider binding dropped the engine source update callback")
	}
	before := requests.Load()
	options.RefreshAllModels(t.Context())
	if requests.Load() != before {
		t.Fatal("disconnected provider remained in refresh walk")
	}
}
