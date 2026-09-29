package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestAuthenticationFailureNamesOnlyTheDefaultServicesKeySource(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "test-router-key")
	sources := config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	services := sources.All()
	services = append(services, modelsource.Connected{
		Source: modelsource.Source{ID: "direct", Written: "direct"}, Key: "test-direct-key",
	})
	sources = modelsource.NewSet(services...)
	for _, model := range []string{"deepseek/deepseek-v4-flash", "openrouter/deepseek/deepseek-v4-flash", "direct/model"} {
		t.Run(model, func(t *testing.T) {
			calls := 0
			completer := &scriptedCompleter{steps: repeatedStep(2, func(context.Context, []ai.Message) (*ai.Response, error) {
				calls++
				return nil, errors.New("API error (401): Missing Authentication header")
			})}
			agent, _ := newTestAgent(t, completer, func(c *Config) {
				c.Model, c.ProfileDir, c.Sources = model, profile, sources
			})
			failure := turnFailure(t, agent, "hello")
			want := "your key was not accepted for this model"
			if !strings.HasPrefix(model, "direct/") {
				want += " — the shell's OPENROUTER_API_KEY"
			}
			if failure.Err == nil || failure.Err.Error() != want {
				t.Errorf("auth ending = %v; want %q", failure.Err, want)
			}
			if calls != 1 {
				t.Errorf("auth refusal made %d calls; want 1", calls)
			}
		})
	}
}
