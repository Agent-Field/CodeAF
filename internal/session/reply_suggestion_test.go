package session

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type replyCompleter struct {
	calls  atomic.Int32
	answer func(context.Context, []ai.Message, ai.Request) (*ai.Response, error)
}

func (c *replyCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	c.calls.Add(1)
	var req ai.Request
	for _, opt := range opts {
		_ = opt(&req)
	}
	return c.answer(ctx, messages, req)
}

func TestReplySuggestionUsesOneMeteredOptionalCallAndCaches(t *testing.T) {
	budget := &liveBudgetStub{cap: 1}
	c := &replyCompleter{answer: func(_ context.Context, msgs []ai.Message, req ai.Request) (*ai.Response, error) {
		if len(req.Tools) != 0 || req.MaxTokens == nil || *req.MaxTokens > 160 {
			t.Error("suggestion can use tools or unbounded output")
		}
		if len(msgs) != 2 || !strings.Contains(messageContentText(msgs[0]), "DATA") || messageContentText(msgs[1]) != "The plan proposes option B. Which tests cover it?" {
			t.Error("context was not isolated from instructions")
		}
		return textResponse(`{"reply":"Which tests cover option B?"}`), nil
	}}
	a, _ := newTestAgent(t, pricedAt(c, 0.02), func(cfg *Config) { cfg.OneModel = true; cfg.JobBudget = budget })
	for range 2 {
		text, err := a.SuggestReply(t.Context(), "The plan proposes option B. Which tests cover it?")
		if err != nil || text != "Which tests cover option B?" {
			t.Fatalf("text=%q err=%v", text, err)
		}
	}
	if c.calls.Load() != 1 {
		t.Fatal("unchanged context bought another request")
	}
	budget.mu.Lock()
	spent := budget.spent
	budget.mu.Unlock()
	if spent != 0.02 {
		t.Fatalf("suggestion cost was not charged exactly once: %v", spent)
	}
	if a.turnSeq != 0 {
		t.Fatal("suggestion started a user turn")
	}
}

func TestReplySuggestionRejectsUnusableOutput(t *testing.T) {
	for _, text := range []string{`I suggest that you approve`, "```json\n{\"reply\":\"Go ahead\"}\n```", `{"reply":"line one\nline two"}`, `{"reply":"` + strings.Repeat("é", 241) + `"}`, `{"reply":1}`} {
		if got := cleanReplySuggestion(text); got != "" {
			t.Fatalf("accepted malformed draft: %q", got)
		}
	}
	if cleanReplySuggestion(`{"reply":""}`) != "" || cleanReplySuggestion(`{"reply":"  Explain option B.  "}`) != "Explain option B." {
		t.Fatal("empty or trimmed reply")
	}
}

func TestReplySuggestionBudgetStopsInflightAndBlocksNewCalls(t *testing.T) {
	budget := &liveBudgetStub{cap: 1}
	started := make(chan struct{})
	c := &replyCompleter{answer: func(ctx context.Context, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true; cfg.JobBudget = budget })
	done := make(chan struct{})
	go func() { _, _ = a.SuggestReply(context.Background(), "The plan is ready."); close(done) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("suggestion did not start")
	}
	// A second context must not start a competing optional call.
	if got, _ := a.SuggestReply(t.Context(), "Different context"); got != "" || c.calls.Load() != 1 {
		t.Fatal("parallel suggestions were queued")
	}
	budget.Spend(1)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("budget did not cancel suggestion")
	}
	_, _ = a.SuggestReply(t.Context(), "Another context")
	if c.calls.Load() != 1 {
		t.Fatal("exhausted job bought another suggestion")
	}
}

func TestReplySuggestionClosesWithConversation(t *testing.T) {
	started := make(chan struct{})
	c := &replyCompleter{answer: func(ctx context.Context, _ []ai.Message, _ ai.Request) (*ai.Response, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true })
	ended := make(chan struct{})
	go func() { _, _ = a.SuggestReply(context.Background(), "Two options are available."); close(ended) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("suggestion did not start")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("suggestion outlived the conversation")
	}
}

func TestReplySuggestionRespectsConversationLimitBeforeGenerationOrCache(t *testing.T) {
	c := &replyCompleter{answer: func(context.Context, []ai.Message, ai.Request) (*ai.Response, error) {
		return textResponse(`{"reply":"Explain the second option."}`), nil
	}}
	a, _ := newTestAgent(t, c, func(cfg *Config) { cfg.OneModel = true; cfg.SpendRailUSD = 1 })
	spend(a, 1.01)
	text, _ := a.SuggestReply(t.Context(), "Two options.")
	if text != "" || c.calls.Load() != 0 {
		t.Fatal("exhausted conversation bought a suggestion")
	}
	if err := a.SetSpendRail(2); err != nil {
		t.Fatal(err)
	}
	text, err := a.SuggestReply(t.Context(), "Two options.")
	if err != nil || text == "" || c.calls.Load() != 1 {
		t.Fatalf("raised limit: %q %v", text, err)
	}
	spend(a, 2.01)
	text, _ = a.SuggestReply(t.Context(), "Two options.")
	if text != "" || c.calls.Load() != 1 {
		t.Fatal("exhausted conversation returned a cached suggestion")
	}
}
