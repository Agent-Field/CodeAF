//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	configpkg "github.com/Agent-Field/codeaf/internal/seniordev/config"
)

// A malformed compaction block must fail the config load, not the first turn.
func TestCompactionPolicyIsValidatedAtConfigLoad(t *testing.T) {
	for name, block := range map[string]map[string]any{
		"unknown policy":       {"policy": "adaptive"},
		"legacy policy":        {"policy": "legacy"},
		"fraction of one":      {"policy": "window", "preserve_recent_fraction": 1},
		"negative fraction":    {"policy": "window", "preserve_recent_fraction": -0.2},
		"zero capacity":        {"policy": "window", "capacity_tokens": 0},
		"non-numeric capacity": {"policy": "window", "capacity_tokens": "lots"},
	} {
		_, err := newSeniorDevConfig(configpkg.Info{"compaction": block})
		if err == nil || !strings.Contains(err.Error(), "compaction") {
			t.Errorf("%s: newSeniorDevConfig error = %v, want a compaction config failure", name, err)
		}
	}
	for name, block := range map[string]map[string]any{
		"window by name":    {"policy": "window", "capacity_tokens": 500000, "preserve_recent_fraction": 0.2},
		"window by absence": {"preserve_recent_tokens": 60000},
	} {
		if _, err := newSeniorDevConfig(configpkg.Info{"compaction": block}); err != nil {
			t.Errorf("%s: newSeniorDevConfig error = %v, want none", name, err)
		}
	}
	if _, err := newSeniorDevConfig(configpkg.Info{}); err != nil {
		t.Errorf("no compaction block: %v", err)
	}
}

// The configured event carries the compaction budget the turn runs under, so
// the budget a run used can be read back from the stream alone.
func TestConfiguredTurnProvenanceCarriesCompactionBudget(t *testing.T) {
	emit := func(t *testing.T, info configpkg.Info, backend backend) map[string]any {
		t.Helper()
		cfg, err := newSeniorDevConfig(info)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		runtime := &runtimeAdapter{config: cfg, backend: backend, events: newEventWriter(&output)}
		if _, err := runtime.configureTurn(turn{
			Agent: "coder", SessionID: "ses-compaction", AgentMarkdown: "prompt",
			ProviderID: "openrouter", ModelID: "fixture/vendor-model",
		}); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatalf("event line %q: %v", line, err)
			}
			if event["stage"] == "agent-runtime" && event["status"] == "configured" {
				data := event["data"].(map[string]any)
				record, ok := data["compaction"].(map[string]any)
				if !ok {
					t.Fatalf("configured event carries no compaction record: %s", line)
				}
				return record
			}
		}
		t.Fatal("no configured event emitted")
		return nil
	}
	// Fixture model: context 240,000, input 220,000, output 12,000. The
	// reservation is min(12,000, 32,000) = 12,000, so raw = 208,000.
	fixture := &openRouterBackend{catalog: seniorDevCatalogFixture(t)}

	t.Run("no backend records no budget", func(t *testing.T) {
		record := emit(t, configpkg.Info{}, nil)
		if record["capacity_tokens"] != nil {
			t.Fatalf("record = %v", record)
		}
	})
	t.Run("the default budgets from the window under the default cap", func(t *testing.T) {
		record := emit(t, configpkg.Info{}, fixture)
		// 208,000 is under the 500,000 default cap; high 124,800; low
		// 83,200; tail 0.2 x high = 24,960.
		if record["capacity_tokens"] != 208_000.0 ||
			record["high_tokens"] != 124_800.0 || record["low_tokens"] != 83_200.0 ||
			record["tail_budget_tokens"] != 24_960.0 || record["model_context_tokens"] != 240_000.0 {
			t.Fatalf("default record = %v", record)
		}
	})
	t.Run("a configured cap records both the cap and its effect", func(t *testing.T) {
		record := emit(t, configpkg.Info{"compaction": map[string]any{
			"policy": "window", "capacity_tokens": 100000, "preserve_recent_fraction": 0.1,
		}}, fixture)
		if record["configured_capacity_tokens"] != 100_000.0 || record["configured_preserve_recent_fraction"] != 0.1 ||
			record["capacity_tokens"] != 100_000.0 || record["high_tokens"] != 60_000.0 ||
			record["low_tokens"] != 40_000.0 || record["tail_budget_tokens"] != 6_000.0 {
			t.Fatalf("capped record = %v", record)
		}
	})
}
