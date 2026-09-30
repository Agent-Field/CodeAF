package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestContextMeterAfterCompactIncludesSentToolDefinitions(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 65_536 })
	personHeavy(agent, 8, 4_000)
	belt := agent.beltTokens()
	if belt == 0 {
		t.Fatal("fixture has no tool definitions")
	}
	hub := newEventHub()
	if changed, err := agent.compactWithPolicy(context.Background(), hub, agent.requestedCompactPolicy()); err != nil || !changed {
		t.Fatalf("compact = %v, %v", changed, err)
	}
	agent.mu.Lock()
	transcript := agent.transcriptTokensLocked()
	agent.mu.Unlock()
	if got := agent.ContextTokens(); got < belt+transcript {
		t.Fatalf("post-compact meter %d omits belt %d from transcript %d", got, belt, transcript)
	}
	shown := agent.ContextTokens()
	if hint := lastCompacted(t, hub).Hint; !strings.Contains(hint, approxTokens(shown)) {
		t.Fatalf("compaction line %q does not use the post-pass meter %d", hint, shown)
	}
	reported := 0
	model.answer = func(_ int, messages []ai.Message) (*ai.Response, error) {
		bytes := 0
		for _, message := range messages {
			bytes += messageBytes(message)
		}
		reported = EstimateTokens(bytes) + belt
		response := textResponse("yes")
		response.Usage.PromptTokens = reported
		return response, nil
	}
	for _, event := range collect(t, mustSubmit(t, agent, "one more question")) {
		if event.Kind == EventError {
			t.Fatal(event.Err)
		}
	}
	if reported == 0 || abs(reported-shown) > max(100, reported/20) {
		t.Fatalf("meter after compact %d differs from next reported prompt %d", shown, reported)
	}
}
