package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

type ceilingSummarizer struct {
	ceilings    []int
	answerAt    int
	alwaysEmpty bool
}

func (s *ceilingSummarizer) CompleteWithMessages(_ context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := ai.Request{}
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	ceiling := 0
	if request.MaxTokens != nil {
		ceiling = *request.MaxTokens
	}
	s.ceilings = append(s.ceilings, ceiling)
	if s.alwaysEmpty || ceiling < s.answerAt {
		response := textResponse("")
		response.Choices[0].FinishReason = "length"
		response.Usage.CompletionTokens = ceiling
		return response, nil
	}
	return textResponse(summaryWords), nil
}

func TestSummaryRetriesOnceWhenThinkingConsumesTheFirstCeiling(t *testing.T) {
	model := &ceilingSummarizer{answerAt: 2500}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 32_768 })
	personHeavy(agent, 8, 4_000)
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(model.ceilings) != 2 || model.ceilings[0] >= model.answerAt || model.ceilings[1] < model.answerAt || model.ceilings[1] >= agent.trustedWindow() {
		t.Fatalf("summary ceilings = %v, want one bounded retry with thinking room", model.ceilings)
	}
	if summaryNotes(liveTranscript(agent)) != 1 {
		t.Fatal("the retried answer was not used")
	}
	if used := agent.Usage(); used.Calls != 2 {
		t.Fatalf("summary spend has %d calls, want both attempts", used.Calls)
	}
}

func TestSummaryNormalAnswerKeepsOneCallAndItsCeiling(t *testing.T) {
	model := &ceilingSummarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 32_768 })
	personHeavy(agent, 8, 4_000)
	agent.mu.Lock()
	plan, why, ok := agent.planSummaryLocked(agent.requestedCompactPolicy())
	agent.mu.Unlock()
	if !ok {
		t.Fatalf("fixture has no summary: %s", why)
	}
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(model.ceilings) != 1 || model.ceilings[0] != plan.ceiling {
		t.Fatalf("normal summary ceilings = %v, want only %d", model.ceilings, plan.ceiling)
	}
}

func TestSummaryChunksLeaveTheListedThinkingPassItsWindowRoom(t *testing.T) {
	profile := provider.ReasoningProfile{Mandatory: true, Efforts: []provider.Effort{provider.EffortHigh, "xhigh"}, Default: "xhigh"}
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) {
		c.ContextWindow = 16_384
		c.ReasoningProfile = func(string) (provider.ReasoningProfile, bool) { return profile, true }
	})
	personHeavy(agent, 10, 20_000)
	agent.mu.Lock()
	plan, why, ok := agent.planSummaryLocked(agent.requestedCompactPolicy())
	agent.mu.Unlock()
	if !ok {
		t.Fatalf("fixture has no summary: %s", why)
	}
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if model.calls() == 0 {
		t.Fatal("the summary was not requested")
	}
	reserve := provider.SummaryOutputReserve(profile, plan.ceiling)
	for index, ask := range model.asks {
		prompt := EstimateTokens(len(messageText(ask[0])) + len(messageText(ask[1])))
		if prompt+reserve+provider.ContextSafetyTokens(plan.window) > plan.window {
			t.Fatalf("summary chunk %d uses %d input tokens and leaves less than %d output tokens in the %d-token window", index, prompt, reserve, plan.window)
		}
	}
}

func TestSummaryStillEmptyAfterRetryKeepsTheExistingReason(t *testing.T) {
	model := &ceilingSummarizer{alwaysEmpty: true}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 32_768 })
	personHeavy(agent, 8, 4_000)
	err := agent.Compact(context.Background())
	why, ok := NothingToCompactWhy(err)
	if !ok || !strings.Contains(why, "the model's summary came back empty or unreadable") {
		t.Fatalf("Compact = %v, want the existing empty-summary reason", err)
	}
	if len(model.ceilings) != 2 {
		t.Fatalf("summary calls = %d, want exactly two", len(model.ceilings))
	}
	if summaryNotes(liveTranscript(agent)) != 0 {
		t.Fatal("an empty answer replaced the conversation")
	}
}

func TestUnreachableLineKeepsBothRemainingPersonMessages(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) {
		c.ContextWindow = 32_768
		c.System = strings.Repeat("fixed system instruction ", 3500)
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", summaryNote("The person established the earlier plan.", "grep or read x")),
		textMessage("user", "question one: "+strings.Repeat("pasted log line ", 800)),
		textMessage("assistant", "answer one"),
		textMessage("user", "question two"))
	agent.mu.Unlock()
	if fixed := EstimateTokens(agent.transcriptMessageBytesLocked(0)) + agent.beltTokens(); fixed <= agent.compactTargetTokens() {
		t.Fatalf("fixed prefix %d does not exceed target %d", fixed, agent.compactTargetTokens())
	}
	err := agent.Compact(context.Background())
	if !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("Compact = %v, want too little older material", err)
	}
	if model.calls() != 0 {
		t.Fatalf("summary calls = %d, want none", model.calls())
	}
	for _, want := range []string{"question one:", "question two"} {
		if !holdsText(liveTranscript(agent), want) {
			t.Fatal(fmt.Sprintf("protected message %q was lost", want))
		}
	}
}

func TestRollingSummaryRequestCarriesPreviousFactsVerbatim(t *testing.T) {
	model := &summarizer{}
	agent, _ := newTestAgent(t, model, func(c *Config) { c.ContextWindow = 65_536 })
	agent.mu.Lock()
	agent.messages = append(agent.messages, textMessage("user", summaryNote("The vault code is PLUM-7731 and the file is /tmp/vault.txt.", "grep or read x")))
	agent.mu.Unlock()
	personHeavy(agent, 8, 20_000)
	if err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if model.calls() == 0 {
		t.Fatal("the rolling summary was not requested")
	}
	first := model.asks[0]
	if !strings.Contains(messageText(first[1]), "PLUM-7731") || !strings.Contains(messageText(first[1]), "/tmp/vault.txt") {
		t.Fatal("the previous summary's facts were not sent")
	}
	if instruction := messageText(first[0]); !strings.Contains(instruction, "previous summary") || !strings.Contains(instruction, "word for word") || !strings.Contains(instruction, "supersedes") {
		t.Fatalf("rolling summary instruction does not protect prior facts: %q", instruction)
	}
}
