package appapi

import (
	"context"
	"errors"

	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/session"
)

// turnStopReason is why a turn ended on an error, read off the error's TYPE and
// never off its sentence: the sentence is written for a person and is reworded
// freely, and a reason matched on words breaks the day somebody improves them.
type turnStopReason string

const (
	// turnStopBudget is a spending cap. Nothing failed: the work is waiting on a
	// raise, so the stop carries the budget question and its raise answer.
	turnStopBudget turnStopReason = "budget"
	// turnStopLimit is the limit the conversation was launched with, which no
	// answer on this surface can raise.
	turnStopLimit turnStopReason = "limit"
	// turnStopCancelled is the turn's own context ending.
	turnStopCancelled turnStopReason = "cancelled"
	// turnStopFailed is everything else: the provider, the wire, the engine.
	turnStopFailed turnStopReason = "failed"
)

const turnFailedWords = "Stopped · the model provider could not finish this reply"

// turnStop is one turn's last error, said the way the desk is told it.
type turnStop struct {
	reason turnStopReason
	words  string
}

// turnStopOf is the one mapping from an engine's error to a turn's ending. A
// typed stop is matched before the default, so a new [session.TurnStop] that is
// not given a case here reads as a failure — and TestEveryTypedTurnStopHasAReason
// fails until it is given one.
func turnStopOf(err error) turnStop {
	switch {
	case err == nil:
		return turnStop{turnStopFailed, turnFailedWords}
	case errors.Is(err, session.ErrSpendRail):
		return turnStop{turnStopBudget, turnStopWords(err)}
	case errors.Is(err, session.ErrLaunchBudget):
		return turnStop{turnStopLimit, turnStopWords(err)}
	case errors.Is(err, context.Canceled):
		return turnStop{turnStopCancelled, "Stopped"}
	}
	return turnStop{turnStopFailed, turnStopWords(err)}
}

func turnStopWords(err error) string {
	return "Stopped · " + redact.Secrets(err.Error())
}

func (s turnStop) failed() bool { return s.reason == turnStopFailed }

// budgetCard is the waiting budget question a spending stop is drawn with — the
// one the Inbox already holds, so its raise is answered through the same door.
// It is read outside the registry's lock: measuring spending reads the records.
func (a *AgentRegistry) budgetCard(agent string, stop *turnStop) *Ask {
	if stop == nil || stop.reason != turnStopBudget || a.budgetAsk == nil {
		return nil
	}
	return a.budgetAsk(agent)
}

// agentBudgetAsk measures now rather than waiting on the watch's next tick: the
// cap is often crossed by the very call before the one that was refused. An
// agent's own cap is named before the day's, as the engine checks them.
func (s *Server) agentBudgetAsk(agent string) *Ask {
	if err := s.RefreshSpending(context.Background()); err != nil {
		return nil
	}
	rows, err := s.spendingAskRows()
	if err != nil {
		return nil
	}
	var day *Ask
	for i := range rows {
		switch {
		case rows[i].Scope == "agent" && rows[i].Owner == agent:
			return &rows[i].Ask
		case rows[i].Scope == "day":
			day = &rows[i].Ask
		}
	}
	return day
}
