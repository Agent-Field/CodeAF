package main

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
)

// costlyCompleter answers every call at a fixed provider-reported cost.
type costlyCompleter struct {
	usd   float64
	calls int
}

func (c *costlyCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.calls++
	cost := c.usd
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant"}}},
		Usage: &ai.Usage{PromptTokens: 100, CompletionTokens: 10, Cost: &cost}}, nil
}

// -YES-SPEND DOES NOT LIFT THE PER-TASK LIMIT: the guard a `do` run is held
// to carries it with and without the flag, with and without a routed crew,
// and a run under the flag stops at it.
func TestYesSpendKeepsThePerTaskLimit(t *testing.T) {
	dir := t.TempDir()
	crew := &crewroute.Decision{}
	for _, c := range []struct {
		crew          *crewroute.Decision
		preauthorized bool
	}{{crew, true}, {crew, false}, {nil, true}, {nil, false}} {
		guard := doSpendGuard(dir, c.crew, c.preauthorized)
		if guard.TaskCap != 5 || guard.TaskAction != config.CrewTaskCapAction(5) {
			t.Fatalf("crew=%v yes-spend=%v: per-task limit %v, %q", c.crew != nil, c.preauthorized, guard.TaskCap, guard.TaskAction)
		}
	}

	guard := doSpendGuard(dir, crew, true)
	guard.Price = func(string) (float64, float64, float64, bool) { return 1e-6, 1e-5, 0, true }
	guard.Day = session.NewSpendDay(0)
	seat := &costlyCompleter{usd: 2}
	wrapped := guard.Wrap("vendor/worker", seat)
	var stopped session.ErrSpendStopped
	var err error
	for i := 0; i < 10 && err == nil; i++ {
		_, err = wrapped.CompleteWithMessages(t.Context(), []ai.Message{{Role: "user"}})
	}
	if !errors.As(err, &stopped) || stopped.Action != "this task reached its $5 limit · raise it in /crew" {
		t.Fatalf("a run under -yes-spend ended on %v", err)
	}
	if seat.calls != 2 {
		t.Fatalf("%d calls were made under a $5 limit at $2 a call, want 2", seat.calls)
	}
}
