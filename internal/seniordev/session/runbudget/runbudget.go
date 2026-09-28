//go:build !windows

// Package runbudget resolves the optional run-level cost and wall-clock
// budget from flags and environment and tracks spend against it.
package runbudget

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// RunBudget is an explicitly supplied run-level budget. A nil field means
// that dimension is unbounded.
type RunBudget struct {
	MaxCostUSD *float64 `json:"maxCostUsd,omitempty"`
	MaxWallMS  *float64 `json:"maxWallMs,omitempty"`
}

// RunBudgetFlags mirrors the two CLI flag values.
type RunBudgetFlags struct {
	MaxCost  *float64 `json:"maxCost"`
	MaxHours *float64 `json:"maxHours"`
}

// BudgetExhaustion is the result of a tracker check.
type BudgetExhaustion struct {
	Yes    bool    `json:"yes"`
	Reason *string `json:"reason,omitempty"`
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func positiveNumber(n float64) (float64, bool) {
	return n, finite(n) && n > 0
}

func positiveString(raw string, present bool) (float64, bool) {
	if !present || strings.TrimSpace(raw) == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	return positiveNumber(parsed)
}

// ResolveRunBudget applies flag-over-environment precedence. A non-nil env map
// is used exactly (including an explicitly empty map); nil reads process env.
func ResolveRunBudget(flags *RunBudgetFlags, env map[string]string) RunBudget {
	lookup := func(name string) (string, bool) {
		if env != nil {
			value, ok := env[name]
			return value, ok
		}
		return os.LookupEnv(name)
	}

	var maxCost float64
	var hasMaxCost bool
	if flags != nil && flags.MaxCost != nil {
		maxCost, hasMaxCost = positiveNumber(*flags.MaxCost)
	}
	if !hasMaxCost {
		raw, present := lookup("SENIOR_DEV_MAX_COST_USD")
		maxCost, hasMaxCost = positiveString(raw, present)
	}

	var maxHours float64
	var hasMaxHours bool
	if flags != nil && flags.MaxHours != nil {
		maxHours, hasMaxHours = positiveNumber(*flags.MaxHours)
	}
	if !hasMaxHours {
		raw, present := lookup("SENIOR_DEV_MAX_WALL_H")
		maxHours, hasMaxHours = positiveString(raw, present)
	}

	budget := RunBudget{}
	if hasMaxCost {
		budget.MaxCostUSD = &maxCost
	}
	if hasMaxHours {
		maxWall := maxHours * 3_600_000
		budget.MaxWallMS = &maxWall
	}
	return budget
}

// IsBounded reports whether either property is present.
func IsBounded(budget RunBudget) bool {
	return budget.MaxCostUSD != nil || budget.MaxWallMS != nil
}

// BudgetTracker is the mutable accumulator returned by MakeBudgetTracker.
type BudgetTracker struct {
	Budget  RunBudget
	startTS float64
	cost    float64
	now     func() float64
}

// MakeBudgetTracker creates a tracker. The optional prior cost defaults to 0.
func MakeBudgetTracker(budget RunBudget, startTS float64, priorCostUSD ...float64) *BudgetTracker {
	prior := float64(0)
	if len(priorCostUSD) > 0 && finite(priorCostUSD[0]) && priorCostUSD[0] > 0 {
		prior = priorCostUSD[0]
	}
	return &BudgetTracker{
		Budget:  budget,
		startTS: startTS,
		cost:    prior,
		now:     func() float64 { return float64(time.Now().UnixMilli()) },
	}
}

// AddCost accumulates a positive finite provider-cost delta.
func (t *BudgetTracker) AddCost(usd float64) {
	if finite(usd) && usd > 0 {
		t.cost += usd
	}
}

// CostUSD returns the accumulated provider cost.
func (t *BudgetTracker) CostUSD() float64 { return t.cost }

// Exhausted checks cost first and wall time second. Omit nowMS to use the
// ambient wall clock.
func (t *BudgetTracker) Exhausted(nowMS ...float64) BudgetExhaustion {
	now := t.now()
	if len(nowMS) > 0 {
		now = nowMS[0]
	}
	if t.Budget.MaxCostUSD != nil && t.cost >= *t.Budget.MaxCostUSD {
		reason := "cost $" + strconv.FormatFloat(t.cost, 'f', 4, 64) +
			" >= budget $" + strconv.FormatFloat(*t.Budget.MaxCostUSD, 'f', 4, 64)
		return BudgetExhaustion{Yes: true, Reason: &reason}
	}
	if t.Budget.MaxWallMS != nil {
		elapsed := now - t.startTS
		if elapsed >= *t.Budget.MaxWallMS {
			reason := "wall " + strconv.FormatFloat(math.Round(elapsed/1000), 'f', -1, 64) +
				"s >= budget " + strconv.FormatFloat(math.Round(*t.Budget.MaxWallMS/1000), 'f', -1, 64) + "s"
			return BudgetExhaustion{Yes: true, Reason: &reason}
		}
	}
	return BudgetExhaustion{Yes: false}
}
