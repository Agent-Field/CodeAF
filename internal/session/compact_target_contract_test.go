package session

import (
	"context"
	"strings"
	"testing"
)

func TestAutomaticPassSkipsSummaryWhenFixedPrefixCannotFit(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) {
		c.ContextWindow = 32_768
		c.System = strings.Repeat("fixed system instruction ", 3500)
	})
	personHeavy(agent, 8, 4_000)
	_, _ = agent.compact(context.Background(), newEventHub())
	if got := model.calls(); got != 0 {
		t.Fatalf("automatic pass bought %d summaries for an unreachable target", got)
	}
	if !agent.recoverContext(context.Background(), nil, refusalOver(32_768, 2_000)) {
		t.Fatal("refusal recovery did not shrink the conversation")
	}
	if got := model.calls(); got == 0 {
		t.Fatal("refusal recovery skipped its useful summary")
	}
}
