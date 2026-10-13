package session

import (
	"fmt"
	"math"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// SetAppBudget binds app agent accounting without turning its daily allowance
// into a lifetime conversation limit. A TUI host can accept the same binding.
func (a *Agent) SetAppBudget(usd float64) error {
	a.mu.Lock()
	id := a.appAgentIDLocked()
	a.mu.Unlock()
	if err := config.BindAppAgentBudget(a.config.ProfileDir, id, usd); err != nil {
		return err
	}
	a.appSpending.Store(true)
	a.appCapPending.Store(false)
	return nil
}

func (a *Agent) appAgentIDLocked() string {
	if a.config.rootSession != "" {
		return a.config.rootSession
	}
	return a.sessionID()
}

func (a *Agent) appCapsEnabled() bool {
	return a.appSpending.Load() || a.config.AppSpending
}

// AppSpendingBlock checks measured day/agent/task receipts at the same door as
// ordinary model calls. Child receipts join their root agent's daily allowance.
// It is also safe for an external producer to use before dispatching work.
func AppSpendingBlock(profile, ledger, agent, task string, now time.Time) error {
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		return err
	}
	day := now.Format(usageDayLayout)
	var daySpent, agentSpent, taskSpent float64
	for _, row := range lines {
		if row.At.After(now) || row.USD <= 0 || math.IsNaN(row.USD) || math.IsInf(row.USD, 0) {
			continue
		}
		owner := row.Root
		if owner == "" {
			owner = row.Session
		}
		if row.At.In(now.Location()).Format(usageDayLayout) == day {
			daySpent += row.USD
			if owner == agent {
				agentSpent += row.USD
			}
		}
		if task != "" && row.Task == task && owner == agent {
			taskSpent += row.USD
		}
	}
	checks := []struct {
		scope, id   string
		spent, base float64
	}{
		{"day", "", daySpent, config.AppDefaultDayCap},
		{"agent", agent, agentSpent, config.AppDefaultAgentBudgetAt(profile)},
	}
	if task != "" {
		checks = append(checks, struct {
			scope, id   string
			spent, base float64
		}{"task", config.AppTaskCapID(agent, task), taskSpent, config.AppDefaultTaskCap})
	}
	for _, check := range checks {
		cap, err := config.AppCapAt(profile, check.scope, check.id, check.base, now)
		if err != nil {
			return err
		}
		if cap > 0 && check.spent >= cap {
			return spendRailReached{said: fmt.Sprintf("%s cap reached · $%.2f spent of $%.2f · raise the cap before continuing", check.scope, check.spent, cap)}
		}
	}
	return nil
}

func (a *Agent) appSpendingBlockLocked() error {
	if !a.appCapsEnabled() {
		return nil
	}
	// The writer owns no agent lock. Flush makes the previous completed call
	// visible before the next call can consume another allowance.
	if !FlushUsage() {
		return fmt.Errorf("could not read spending; try again before continuing")
	}
	ledger := a.config.usageLedger
	if ledger == "" {
		ledger = filepath.Join(config.ProfilePath(a.config.ProfileDir, ""), "v3", UsageLedgerName)
	}
	err := AppSpendingBlock(a.config.ProfileDir, ledger, a.appAgentIDLocked(), a.usageNode(), time.Now())
	a.appCapPending.Store(err != nil)
	return err
}

func (a *Agent) appSpendingBlock() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.appSpendingBlockLocked()
}

func (a *Agent) appCapNeedsPerson() bool {
	if !a.appCapPending.Load() {
		return false
	}
	return a.appSpendingBlock() != nil
}

// Live task guards retain their reservations while reading raised limits afresh.
func (a *Agent) bindAppGuard(guard *SpendGuard, task string) {
	if guard == nil || !a.appCapsEnabled() {
		return
	}
	a.mu.Lock()
	owner := a.appAgentIDLocked()
	a.mu.Unlock()
	guard.Limits = func() (float64, float64, error) {
		now := time.Now()
		day, err := config.AppCapAt(a.config.ProfileDir, "day", "", config.AppDefaultDayCap, now)
		if err != nil {
			return 0, 0, err
		}
		cap, err := config.AppCapAt(a.config.ProfileDir, "task", config.AppTaskCapID(owner, task), config.AppDefaultTaskCap, now)
		return day, cap, err
	}
}
