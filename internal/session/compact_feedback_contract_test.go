package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestFoldReportsWhyItsSummaryWasSkipped(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		answer     func(int, []ai.Message) (*ai.Response, error)
	}{
		{"error", "provider unavailable", func(int, []ai.Message) (*ai.Response, error) { return nil, errors.New("provider unavailable") }},
		{"empty", "empty or unreadable", func(int, []ai.Message) (*ai.Response, error) { return textResponse(""), nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &summarizer{answer: tc.answer}
			agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 65_536 })
			for i := 0; i < 10; i++ {
				agent.mu.Lock()
				agent.messages = append(agent.messages, textMessage("user", fmt.Sprintf("question %d %s", i, strings.Repeat("pasted log ", 1400))), textMessage("assistant", strings.Repeat("long answer ", 400)))
				agent.mu.Unlock()
			}
			hub := newEventHub()
			changed, err := agent.compactWithPolicy(context.Background(), hub, agent.requestedCompactPolicy())
			if err != nil || !changed {
				t.Fatalf("compact = %v, %v", changed, err)
			}
			hint := lastCompacted(t, hub).Hint
			if !strings.Contains(hint, "folded") || !strings.Contains(hint, "summary skipped: ") || !strings.Contains(hint, tc.want) {
				t.Fatalf("partial pass hid the failed summary: %q", hint)
			}
		})
	}
}

func TestCompactionHintOmitsEqualRoundedSizes(t *testing.T) {
	hint := compactionHint(compactionPass{folded: 1}, 7_850, 7_800)
	if strings.Contains(hint, "→") {
		t.Fatalf("the rounded size did not change: %q", hint)
	}
}
