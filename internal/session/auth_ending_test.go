package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// The engine sends the ending's words without its wrapped refusal, so the
// surface's shared prefix must be the one the real turn actually ends on.
func TestExpiredKeyTurnEndsOnTheSharedUnauthorizedSentence(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "test-router-key")
	refusal := &provider.APIError{Status: 401, Message: "API key expired."}
	completer := &scriptedCompleter{steps: repeatedStep(2, func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, refusal
	})}
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Model, c.ProfileDir = config.DefaultModel, profile
		c.Sources = config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	})
	failure := turnFailure(t, agent, "hello")
	if failure.Err == nil || !strings.HasPrefix(failure.Err.Error(), UnauthorizedKeySentence) {
		t.Fatalf("expired-key turn ending = %v; want prefix %q", failure.Err, UnauthorizedKeySentence)
	}
	if !strings.HasSuffix(failure.Err.Error(), " — the shell's OPENROUTER_API_KEY") {
		t.Fatalf("expired-key turn lost its key source: %v", failure.Err)
	}
	if !provider.KeyExpiredFrom(failure.Err) {
		t.Fatal("the in-process ending lost the typed expired-key refusal")
	}
	verdict := taxonomy.Verdict{Reason: taxonomy.ReasonUnauthorized}
	if got := transportKeptWords(verdict); got != UnauthorizedKeySentence {
		t.Fatalf("repeated unauthorized refusal = %q; want %q", got, UnauthorizedKeySentence)
	}
}
