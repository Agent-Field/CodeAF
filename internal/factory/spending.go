package factory

import (
	"fmt"
	"math"
)

// ── THE FACTORY'S OWN SPENDING DEFAULTS ────────────────────────────────────
//
// Spending is what a factory says about money and models for EVERY repository
// it watches, said once instead of in each repository's `.codeaf/factory.yaml`.
// It is kept by the factory that serves the floor, beside its rules, so on a
// team it is the team's and lives on the shared server.
//
// EACH VALUE ONLY FILLS WHAT A REPOSITORY LEAVES UNSAID, with one tightening:
//
//   - DefaultBudgetUSD is the budget a job gets when it has none and its
//     repository's file names no `default_budget`; the built-in [DefaultJobBudget]
//     otherwise. A repository's own figure stands.
//   - AutoStartMaxUSD is the most a job may be budgeted and still start on its
//     own (`start.max_cost`); above it the job asks first. It replaces the
//     built-in figure where a repository says none, and where a repository says
//     one the stricter of the two holds. Zero makes every such job ask first.
//   - DefaultModel is the model a step runs on when neither the step nor its
//     job names one. It travels with the job ([Item.FactoryModel]). A runner on
//     one model for every task keeps that model.
//
// Nil figures and an empty model are not set: the built-in behaviour holds.
type Spending struct {
	DefaultBudgetUSD *float64 `json:"default_budget_usd,omitempty"`
	AutoStartMaxUSD  *float64 `json:"auto_start_max_usd,omitempty"`
	DefaultModel     string   `json:"default_model,omitempty"`
}

// BudgetMostUSD is the largest dollar figure a person may give a factory budget
// or start cap: a figure past it is a typo, not a budget.
const BudgetMostUSD = 10000.0

// Check refuses a value the factory would not keep: a default budget that is
// not above zero, a start cap below zero, either past [BudgetMostUSD], and a
// model that is not one model id ([ModelID]).
func (s Spending) Check() error {
	if v := s.DefaultBudgetUSD; v != nil && (!finite(*v) || *v <= 0 || *v > BudgetMostUSD) {
		return fmt.Errorf("the default budget is a number of dollars above zero and at most %g", BudgetMostUSD)
	}
	if v := s.AutoStartMaxUSD; v != nil && (!finite(*v) || *v < 0 || *v > BudgetMostUSD) {
		return fmt.Errorf("the most a job may cost to start on its own is a number of dollars, zero or more and at most %g", BudgetMostUSD)
	}
	if !ModelID(s.DefaultModel) {
		return ErrModelID
	}
	return nil
}

// Same says two readings keep the same values.
func (s Spending) Same(o Spending) bool {
	return sameFigure(s.DefaultBudgetUSD, o.DefaultBudgetUSD) && sameFigure(s.AutoStartMaxUSD, o.AutoStartMaxUSD) && s.DefaultModel == o.DefaultModel
}

func sameFigure(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
