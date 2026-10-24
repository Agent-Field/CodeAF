package session

import (
	"errors"
	"fmt"
	"time"
)

// ErrLaunchBudget identifies a refused turn at the launch's dollar or time
// limit. Goal ownership and spending authority are independent: choosing a
// person as principal must not discard a limit supplied at the door.
var ErrLaunchBudget = errors.New("session: launch limit reached")

const conversationTimeLimitWord = "conversation time limit reached · start a new conversation or relaunch with a larger --max-hours"

func (a *Agent) interactiveBudget() Budget {
	if !a.config.Interactive || a.config.InTask || a.config.Errand {
		return Budget{}
	}
	return a.config.Budget
}

// launchBudgetBlockLocked runs at the existing turn-admission boundary with
// a.mu held. This check does not reserve spending across tasks. The independent
// conversation wall reader cancels owned work when the launch time expires.
func (a *Agent) launchBudgetBlockLocked() error {
	budget := a.interactiveBudget()
	if budget.Wall > 0 && !a.startedAt.IsZero() && time.Since(a.startedAt) >= budget.Wall {
		return launchBudgetReached{said: conversationTimeLimitWord}
	}
	if budget.USD > 0 && a.usage.CostUSD >= budget.USD {
		return launchBudgetReached{said: fmt.Sprintf("conversation launch limit reached · %s spent of %s · relaunch with a larger --max-cost",
			railMoney(a.usage.CostUSD), railMoney(budget.USD))}
	}
	return nil
}

type launchBudgetReached struct{ said string }

func (e launchBudgetReached) Error() string { return e.said }
func (e launchBudgetReached) Unwrap() error { return ErrLaunchBudget }
