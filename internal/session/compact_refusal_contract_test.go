package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestRefusedSummaryDoesNotReplaceConversation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(int, []ai.Message) (*ai.Response, error)
	}{
		{"refusal prose", func(int, []ai.Message) (*ai.Response, error) {
			return textResponse("I'm sorry, but I can't help with that request."), nil
		}},
		{"content filter", func(int, []ai.Message) (*ai.Response, error) {
			response := textResponse(summaryWords)
			response.Choices[0].FinishReason = "content_filter"
			return response, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &summarizer{answer: tc.answer}
			agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 65_536 })
			personHeavy(agent, 8, 20_000)
			hub := newEventHub()
			_, _ = agent.compactWithPolicy(context.Background(), hub, agent.requestedCompactPolicy())
			if summaryNotes(liveTranscript(agent)) != 0 || !holdsText(liveTranscript(agent), "question 1:") {
				t.Fatal("the refusal replaced the older conversation")
			}
			if hint := lastCompacted(t, hub).Hint; !strings.Contains(hint, "summary skipped: the model declined to write a summary") {
				t.Fatalf("the pass did not explain the refusal: %q", hint)
			}
		})
	}
}

func TestRoleWordsDoNotMakeSummaryRefusalLookSubstantive(t *testing.T) {
	for _, answer := range []string{
		"I'm sorry, but I cannot help this person with that request.",
		"I'm sorry, but as an AI assistant I can't help with that.",
	} {
		model := &summarizer{answer: func(int, []ai.Message) (*ai.Response, error) { return textResponse(answer), nil }}
		agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 65_536 })
		personHeavy(agent, 8, 20_000)
		hub := newEventHub()
		_, _ = agent.compactWithPolicy(context.Background(), hub, agent.requestedCompactPolicy())
		if summaryNotes(liveTranscript(agent)) != 0 || !holdsText(liveTranscript(agent), "question 1:") {
			t.Fatalf("role word in refusal replaced conversation: %q", answer)
		}
	}
}
