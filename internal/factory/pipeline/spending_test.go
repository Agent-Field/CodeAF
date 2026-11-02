package pipeline

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE FACTORY'S SPENDING FILLS WHAT A REPOSITORY LEAVES UNSAID. Its default
// budget stands in for the built-in one, never for a repository's own; its cap
// on starting alone stands in for the built-in one and tightens a repository's
// looser one, never loosens a stricter one.
func TestTheFactorysSpendingFillsWhatTheRepositoryLeavesUnsaid(t *testing.T) {
	budget, most := 9.0, 2.0
	s := factory.Spending{DefaultBudgetUSD: &budget, AutoStartMaxUSD: &most}
	f := WithSpending(Defaults(), s)
	if f.DefaultBudget != 9 || f.Start.MaxCost != 2 {
		t.Fatalf("defaults read budget %v, cap %v", f.DefaultBudget, f.Start.MaxCost)
	}
	own, err := Parse("default_budget: 3\nstart:\n  max_cost: 7\n")
	if err != nil {
		t.Fatal(err)
	}
	if f = WithSpending(own, s); f.DefaultBudget != 3 || f.Start.MaxCost != 2 {
		t.Fatalf("a repository's own figures read budget %v, cap %v", f.DefaultBudget, f.Start.MaxCost)
	}
	strict, _ := Parse("start:\n  max_cost: 1\n")
	if f = WithSpending(strict, s); f.Start.MaxCost != 1 {
		t.Fatalf("a stricter repository cap read %v", f.Start.MaxCost)
	}
	if f = WithSpending(Defaults(), factory.Spending{}); f.DefaultBudget != factory.DefaultJobBudget || f.Start.MaxCost != DefaultMaxCost {
		t.Fatalf("nothing set read budget %v, cap %v", f.DefaultBudget, f.Start.MaxCost)
	}
}
