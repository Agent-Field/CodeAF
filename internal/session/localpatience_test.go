package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/claudemodel"
	"github.com/Agent-Field/codeaf/internal/config"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// EVERY OTHER TEST IN THIS PACKAGE RUNS UNDER A PATIENCE OF ONE, in the
// environment as well as in the process-wide figure. Their agents talk to
// scripted completers and loopback servers, which are "local endpoints" to the
// classifier, and a local endpoint is given ten times the hosted wait under
// `auto`; the ladders in modelhop_test.go size their clock from the hosted
// give-up and assert how many requests it buys, so the figure they describe is
// the hosted one. The variable and not only the number, because every
// config.Load a test runs installs the profile's row over the process figure,
// and the environment pin is what that row yields to.
//
// Tests about the stretch itself set their own with [usePatience].
func init() {
	os.Setenv("CODEAF_RESPONSE_PATIENCE", "1")
	lanes.UseResponsePatience(1)
}

func usePatience(t *testing.T, factor float64) {
	t.Helper()
	t.Setenv(config.EnvResponsePatience, config.FormatResponsePatience(factor))
	lanes.UseResponsePatience(factor)
	t.Cleanup(func() { lanes.UseResponsePatience(1) })
}

// CONTRACT: the turn gives a model as long as the transport gives its calls, so
// a conversation does not hop to another model while the first is still, by the
// transport's own reckoning, working — and a hosted model keeps the ninety
// seconds it always had.
func TestTheTurnsGiveUpIsInThePatienceOfTheModelItIsAsking(t *testing.T) {
	asking := func(base string) *Agent {
		t.Helper()
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.BaseURL = base })
		return agent
	}
	hosted := asking("https://openrouter.ai/api/v1")
	local := asking("http://localhost:11434/v1")
	const model = "test/model"

	usePatience(t, 1)
	base := hosted.giveUp()
	if base != 90*time.Second {
		t.Fatalf("the watched turn's give-up = %v, want the measured 90s", base)
	}
	for name, agent := range map[string]*Agent{"hosted": hosted, "local": local} {
		if got := agent.giveUpOn(model); got != base {
			t.Errorf("under 1, %s: give-up %v, want %v (a hosted turn must not move)", name, got, base)
		}
	}

	usePatience(t, 0)
	if got := hosted.giveUpOn(model); got != base {
		t.Errorf("auto, hosted: give-up %v, want exactly %v", got, base)
	}
	if got := local.giveUpOn(model); got != 15*time.Minute {
		t.Errorf("auto, local: give-up %v, want ten times the hosted ninety seconds", got)
	}

	usePatience(t, 3)
	for name, agent := range map[string]*Agent{"hosted": hosted, "local": local} {
		if got := agent.giveUpOn(model); got != 270*time.Second {
			t.Errorf("under 3, %s: give-up %v, want 4m30s on both", name, got)
		}
	}
}

func TestOpenAgentUsesTheSavedPatienceOnItsNextRequest(t *testing.T) {
	usePatience(t, 0)
	t.Setenv(config.EnvResponsePatience, "")
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", profile)
	const base = "http://localhost:11434/v1"
	seen := make(chan float64, 2)
	check := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		seen <- lanes.PatienceFor(base, "")
		return textResponse("Done."), nil
	}
	client := &scriptedCompleter{steps: []step{check, check}}
	agent, _ := newTestAgent(t, client, func(cfg *Config) {
		cfg.BaseURL = base
	})
	submit := func(want float64) {
		t.Helper()
		events, err := agent.Submit(t.Context(), "Say done.")
		if err != nil {
			t.Fatal(err)
		}
		for range events {
		}
		select {
		case got := <-seen:
			if got != want {
				t.Fatalf("request used patience %v, want %v", got, want)
			}
		default:
			t.Fatal("no model request")
		}
	}
	submit(10)
	// A separate terminal process changes the file without touching this
	// engine's in-memory setting.
	if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(`{"response.patience":"3x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	submit(3)
}

func TestCompactSummaryUsesTheConversationEndpointsPatience(t *testing.T) {
	usePatience(t, 0)
	for _, tc := range []struct {
		base string
		want time.Duration
	}{
		{"https://api.openai.com/v1", 2 * time.Minute},
		{"http://localhost:11434/v1", 20 * time.Minute},
	} {
		t.Run(tc.base, func(t *testing.T) {
			check := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				deadline, ok := ctx.Deadline()
				if left := time.Until(deadline); !ok || left > tc.want || left < tc.want-time.Second {
					t.Errorf("summary deadline = %v (present %v), want %v", left, ok, tc.want)
				}
				return textResponse("The conversation reviewed the authentication middleware."), nil
			}
			agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{check}}, func(cfg *Config) {
				cfg.BaseURL = tc.base
			})
			agent.mu.Lock()
			address, source := agent.summaryEndpointLocked("test/model")
			agent.mu.Unlock()
			plan := summaryPlan{model: "test/model", ceiling: 100, answer: 100, address: address, source: source}
			_, _, err := agent.askForSummary(t.Context(), plan, "",
				"The conversation reviewed the authentication middleware.")
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGiveUpUsesLiveSourcesWhileTheyAreReplaced(t *testing.T) {
	usePatience(t, 0)
	hosted := modelsource.Set{}.OrDefault("key", "https://api.openai.com/v1")
	local := modelsource.Set{}.OrDefault("key", "http://localhost:11434/v1")
	const model = "test/model"
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.Sources = hosted })
	base := agent.giveUp()
	agent.SetSources(local)
	if got := agent.giveUpOn(model); got != 10*base {
		t.Fatalf("give-up after switching to a local source = %v, want 10x %v", got, base)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			agent.SetSources(local)
			agent.SetSources(hosted)
		}
	}()
	defer func() { <-done }()
	for range 100 {
		if got := agent.giveUpOn(model); got != base && got != 10*base {
			t.Fatalf("give-up = %v with either a local or a hosted source", got)
		}
	}
}

// CONTRACT: a summary through Claude Code waits the hosted bound, not the
// local one. Claude Code's sentinel address ends in `.local`, and under `auto`
// that stretched a refused turn's summary from two minutes to twenty — a turn
// that sat on "Still on it" while the summary never came (2026-11-25). A real
// local server keeps its stretch, and a number the person wrote still applies
// to Claude Code as it does everywhere.
func TestASummaryThroughClaudeCodeWaitsTheHostedBound(t *testing.T) {
	usePatience(t, 0)
	if got := summaryPatience(claudemodel.Address, claudemodel.ID); got != 1 {
		t.Fatalf("Claude Code summary patience under auto = %v, want 1", got)
	}
	if got := lanes.Stretch(summaryCallWindow, summaryPatience(claudemodel.Address, claudemodel.ID)); got != summaryCallWindow {
		t.Fatalf("Claude Code summary bound = %v, want %v", got, summaryCallWindow)
	}
	if got := summaryPatience("http://localhost:11434/v1", "ollama"); got != lanes.LocalPatience {
		t.Fatalf("local server summary patience = %v, want %v", got, lanes.LocalPatience)
	}
	usePatience(t, 3)
	if got := summaryPatience(claudemodel.Address, claudemodel.ID); got != 3 {
		t.Fatalf("Claude Code summary patience with a written 3 = %v, want 3", got)
	}
}
