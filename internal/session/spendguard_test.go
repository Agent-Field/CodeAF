package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// spendingCompleter answers every call at a fixed cost, as the provider's own
// usage figure, and counts what it was asked.
type spendingCompleter struct {
	usd   float64
	calls int
}

func (c *spendingCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.calls++
	cost := c.usd
	return &ai.Response{Choices: []ai.Choice{{Message: textMessage("assistant", "ok")}},
		Usage: &ai.Usage{PromptTokens: 1000, CompletionTokens: 100, Cost: &cost}}, nil
}

// kimiPrice is a checker's prices per token.
func kimiPrice(string) (float64, float64, float64, bool) { return 3e-6, 1.5e-5, 3e-7, true }

// A DEAR CHECKER UNDER A SMALL DAY: an open-ended task under a $0.25 day whose
// checker would run far past its estimate. Every call is priced before it is
// made: the checker stops at its own ceiling, three times its estimate, and
// the day never passes its cap by more than one call's misjudgement.
func TestTheCheckerStopsAtItsCeilingAndTheDayAtItsCap(t *testing.T) {
	messages := []ai.Message{textMessage("user", strings.Repeat("the diff and the tests ", 800))}
	checker := &spendingCompleter{usd: 0.06}
	guard := &SpendGuard{Price: kimiPrice, Day: NewSpendDay(0), Cap: 1, CapAction: "today's spending limit of $1.00 is reached · raise it with /budget",
		Ceilings: map[string]float64{"moonshotai/kimi-k3": 0.0882 * 3}, CeilingAction: "the check stopped at its spend ceiling of $%.2f"}
	seat := guard.Wrap("moonshotai/kimi-k3", checker)
	var stopped ErrSpendStopped
	for i := 0; i < 20; i++ {
		if _, err := seat.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(2000)); err != nil {
			if !errors.As(err, &stopped) {
				t.Fatalf("call %d: %v", i, err)
			}
			break
		}
	}
	if !strings.HasPrefix(stopped.Action, "the check stopped at its spend ceiling of $0.26") {
		t.Fatalf("the checker ended on %q after %d calls", stopped.Action, checker.calls)
	}
	if spent := guard.Day.Total(); spent > 0.0882*3 {
		t.Errorf("the checker spent $%.3f", spent)
	}

	// The day's cap binds a seat with no ceiling of its own, before its call.
	worker := &spendingCompleter{usd: 0.05}
	day := &SpendGuard{Price: kimiPrice, Day: NewSpendDay(0.23), Cap: 0.25, CapAction: "cap reached"}
	seat = day.Wrap("z-ai/glm-5.3", worker)
	_, err := seat.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(2000))
	if !errors.As(err, &stopped) || stopped.Action != "cap reached" || worker.calls != 0 {
		t.Errorf("a call that would cross the cap: %v after %d calls", err, worker.calls)
	}
}
