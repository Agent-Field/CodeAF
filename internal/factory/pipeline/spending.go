package pipeline

import "github.com/Agent-Field/codeaf/internal/factory"

// WithSpending fills what one repository's read leaves unsaid with the
// factory's spending defaults ([factory.Spending]). The default budget stands
// in for the built-in one and never for a figure the read already has (the
// repository's file, or a personal file that lowered it). The cap on starting
// alone stands in for the built-in one too, and where the read has its own the
// stricter of the two holds, so the factory can hold more work back but never
// let a repository's stricter cap go.
//
// The file is copied first, so a cached read is never changed in place.
func WithSpending(f File, s factory.Spending) File {
	out := cloneFile(f)
	if v := s.DefaultBudgetUSD; v != nil && budgetUnsaid(f) {
		out.DefaultBudget = *v
	}
	if v := s.AutoStartMaxUSD; v != nil && (maxCostUnsaid(f) || *v < out.Start.MaxCost) {
		out.Start.MaxCost = *v
	}
	return out
}

// budgetUnsaid says the read's default budget is the built-in one: neither
// the repository's file nor a personal file named one.
func budgetUnsaid(f File) bool {
	return !f.set.defaultBudget && f.DefaultBudget == factory.DefaultJobBudget
}

// maxCostUnsaid says the read's start cap is the built-in one.
func maxCostUnsaid(f File) bool {
	return !f.set.maxCost && f.Start.MaxCost == DefaultMaxCost
}
