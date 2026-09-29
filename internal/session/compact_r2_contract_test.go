package session

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestManualSummaryKeepsThreeWhenNoCutCanReachTarget(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) {
		c.ContextWindow = 32_768
		c.System = strings.Repeat("fixed system instruction ", 3500)
	})
	personHeavy(agent, 8, 4_000)
	if fixed := EstimateTokens(agent.transcriptMessageBytesLocked(0)) + agent.beltTokens(); fixed <= agent.compactTargetTokens() {
		t.Fatalf("fixed prefix %d does not exceed target %d", fixed, agent.compactTargetTokens())
	}
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if model.calls() != 1 {
		t.Fatalf("summary calls = %d, want one", model.calls())
	}
	messages := liveTranscript(agent)
	for turn := 6; turn <= 8; turn++ {
		if !holdsText(messages, fmt.Sprintf("question %d:", turn)) {
			t.Fatalf("last three messages lost question %d", turn)
		}
	}
	if holdsText(messages, "question 5:") {
		t.Fatal("older question was not summarized")
	}
}

func TestAutomaticSummaryKeepsThreeWhenNoCutCanReachTarget(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) {
		c.ContextWindow = 32_768
		c.System = strings.Repeat("fixed system instruction ", 900)
	})
	personHeavy(agent, 8, 4_000)
	policy := agent.automaticCompactPolicy()
	if !policy.summarize {
		t.Fatal("fixture has no reachable summary call")
	}
	if changed, err := agent.compactWithPolicy(context.Background(), nil, policy); !changed || err != nil {
		t.Fatalf("automatic compact = %v, %v", changed, err)
	}
	if model.calls() != 1 {
		t.Fatalf("automatic summary calls = %d, want one", model.calls())
	}
	messages := liveTranscript(agent)
	for turn := 6; turn <= 8; turn++ {
		if !holdsText(messages, fmt.Sprintf("question %d:", turn)) {
			t.Fatalf("automatic pass lost recent question %d", turn)
		}
	}
	if holdsText(messages, "question 5:") {
		t.Fatal("automatic pass did not summarize the older question")
	}
}

func TestRefusedRequestCanKeepFewerThanThreeWhenRoomIsMissing(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 32_768 })
	personHeavy(agent, 8, 4_000)
	failure := &provider.APIError{Status: 400, Overflow: true, ContextLimit: 32_768, InputTokens: 40_000, OutputTokens: 512}
	if !agent.recoverContext(context.Background(), nil, failure) {
		t.Fatal("refused request was not shortened")
	}
	messages := liveTranscript(agent)
	if !holdsText(messages, "question 8:") || holdsText(messages, "question 6:") {
		t.Fatal("recovery did not keep the latest message and reclaim older ones")
	}
}
