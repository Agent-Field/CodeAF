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

func (a *Agent) interactiveBudget() Budget {
	if !a.config.Interactive || a.config.InTask || a.config.Errand || a.config.inHand {
		return Budget{}
	}
	return a.config.Budget
}

// launchBudgetBlockLocked runs at the existing turn-admission boundary with
// a.mu held. Like the conversation spend limit, it lets in-flight work finish;
// it is not a timer that interrupts tools or an exact reservation across tasks.
func (a *Agent) launchBudgetBlockLocked() error {
	budget := a.interactiveBudget()
	if budget.Wall > 0 && !a.startedAt.IsZero() && time.Since(a.startedAt) >= budget.Wall {
		return launchBudgetReached{said: "conversation time limit reached · start a new conversation or relaunch with a larger --max-hours"}
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
