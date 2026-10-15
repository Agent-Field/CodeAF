package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
)

// A model selected for a summary can be rerouted before the request. Provenance
// must name the answering model, just as the summary spend ledger does.
func TestReroutedSummaryFactNamesAnsweringModel(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	profile := t.TempDir()
	if err := config.WriteAPIKey(profile, "sk-or-v1-test-0123456789"); err != nil {
		t.Fatal(err)
	}
	const chat = "z-ai/glm-5.3-flash"
	const worker = "moonshotai/kimi-k3"
	oldCatalog, oldHistory, oldGood := config.CrewCatalog, config.CrewRouteHistory, config.CrewLastGood
	t.Cleanup(func() {
		config.CrewCatalog, config.CrewRouteHistory, config.CrewLastGood = oldCatalog, oldHistory, oldGood
	})
	config.CrewCatalog = func() []catalog.Model {
		return []catalog.Model{
			{ID: chat, OpenWeights: true, PromptPrice: 1.5e-7, CompletionPrice: 5e-7, IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ArenaElo: 1348, ContextLength: 1310720, Parameters: []string{"tools"}},
			{ID: worker, OpenWeights: true, PromptPrice: 3e-6, CompletionPrice: 1.5e-5, IntelligenceIndex: 43.6, CodingIndex: 76.2, AgenticIndex: 50, ArenaElo: 1421, ContextLength: 1048576, Parameters: []string{"tools"}},
		}
	}
	config.CrewRouteHistory = func(string) []router.CrewRouteOutcome {
		return []router.CrewRouteOutcome{{At: time.Now(), Send: chat, Provider: "openrouter", Kind: "forbidden"}}
	}
	config.CrewLastGood = func(string) *router.CrewRecord { return nil }
	if err := config.SetCrewPin(profile, crewroute.Worker, worker); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &summarizer{}, func(c *Config) {
		c.Model = chat
		c.ProfileDir = profile
		c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return crewroute.Decision{}, nil }
		c.ContextWindow = 65_536
		c.SessionFile = path
	})
	if got := agent.healthyModel(context.Background(), purposeSummary, chat); got != worker {
		t.Fatalf("fixture did not reroute the summary: %q", got)
	}
	personHeavy(agent, 8, 20_000)
	events, err := observed(t, agent, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == EventCompacted && event.Compaction != nil && event.Compaction.Summarized > 0 {
			if got := event.Compaction.Model; got != worker {
				t.Fatalf("summary fact names requested model %q, want actual answering model %q", got, worker)
			}
			return
		}
	}
	t.Fatal("the summary wrote no structured compaction fact")
}
