package session

import (
	"strings"
	"testing"
)

func TestTheSystemPromptDoesNotNameOptionalHandsThatAreOff(t *testing.T) {
	rendered := renderSystem(Config{Workspace: t.TempDir()})
	for _, verb := range []string{"build_harness", "propose_subharness", "run_adaptive"} {
		if strings.Contains(rendered, verb) {
			t.Fatalf("prompt without optional hands names %s", verb)
		}
	}
}

func TestSetModelUsesTheSessionsOwnContextCatalog(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Model = "small"
		config.ContextWindow = 100
		config.ContextWindowFor = func(model string) int {
			if model == "large" {
				return 900
			}
			return 0
		}
	})
	agent.SetModel("large")
	if got := agent.contextWindow.Load(); got != 900 {
		t.Fatalf("context window = %d, want engine catalog's 900", got)
	}
}
