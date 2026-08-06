package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

func settings(t *testing.T) Config {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	// Nothing here may reach the network. A refused connection is the offline
	// case, which the catalog is required to tolerate.
	t.Setenv("AFORGE_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("AFORGE_PROFILE_DIR", t.TempDir())
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return config
}

// TestNoPanelIsTheKillSwitch is the promise the whole feature is gated on. With
// AFORGE_MODELS unset the harness must build the adapter it has always built and
// run the path it has always run — not a router with one rung, which would be a
// different code path wearing the same behaviour.
func TestNoPanelIsTheKillSwitch(t *testing.T) {
	t.Setenv("AFORGE_MODELS", "")
	config := settings(t)
	if len(config.Panel.Models) != 0 {
		t.Fatalf("panel = %+v, want none", config.Panel)
	}
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	if _, plain := client.(*provider.Client); !plain {
		t.Fatalf("client = %T, want the single-model adapter", client)
	}
	if client.Model() != DefaultModel {
		t.Fatalf("model = %q, want the configured default", client.Model())
	}
	// And the run context is stamped exactly as before: one cache key for the
	// run, the operator's reasoning level, and no call class until a call site
	// opens one.
	ctx := config.Context(context.Background(), "a goal")
	if provider.CacheKeyFrom(ctx) != provider.RunCacheKey("a goal", DefaultModel) {
		t.Fatal("the run cache key changed")
	}
	if provider.CallClassFrom(ctx) != "" {
		t.Fatal("a call class was stamped where no call site opened one")
	}
}

// TestAPanelSwitchesInTheRouter is the other side of the switch, and it is one
// line of environment away.
func TestAPanelSwitchesInTheRouter(t *testing.T) {
	t.Setenv("AFORGE_MODELS", "google/gemma-3-12b-it,~deepseek/deepseek-v4-flash-latest,moonshotai/kimi-k2.6")
	config := settings(t)
	if len(config.Panel.Models) != 3 {
		t.Fatalf("panel = %+v, want three models", config.Panel.Models)
	}
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	panel, routed := client.(*router.Router)
	if !routed {
		t.Fatalf("client = %T, want a router", client)
	}
	defer panel.Close()
	if panel.Rungs() != 3 {
		t.Fatalf("rungs = %d, want three", panel.Rungs())
	}
	if client.Model() != "google/gemma-3-12b-it" {
		t.Fatalf("model = %q, want the first panel entry", client.Model())
	}
}

// TestPanelStateLivesWhereTheProfileDoes keeps a run's memory in one place. An
// operator who redirected AFORGE_PROFILE_DIR — a test, a sandbox, a second
// account — must not find half of it in their home directory anyway.
func TestPanelStateLivesWhereTheProfileDoes(t *testing.T) {
	t.Setenv("AFORGE_MODELS", "a/one,b/two")
	config := settings(t)
	client, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	panel := client.(*router.Router)
	panel.Ledger().Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	if err := panel.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"router-ledger.json", "router-events.jsonl"} {
		if _, err := os.Stat(filepath.Join(config.ProfileDir, name)); err != nil {
			t.Fatalf("%s was not written to the configured profile directory: %v", name, err)
		}
	}
}

// TestAnUnreadablePanelIsAStartupError rather than a silent fallback. A typo in
// AFORGE_MODELS that quietly ran everything on one model would be discovered
// only in the bill, or never.
func TestAnUnreadablePanelIsAStartupError(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("AFORGE_MODELS", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := Load(); err == nil {
		t.Fatal("a panel file that does not exist was accepted")
	}
}
