package session

import "testing"

// The factory's usage is its jobs' ledger rows in the range, by model, and by
// job then model: calls, tokens, the priced dollars and what nobody priced.
func TestFactoryUsageByModelFoldsOnlyFactoryJobRowsInRange(t *testing.T) {
	lines := []UsageLine{
		{Day: "2026-10-26", Model: "a", FactoryJob: "o/r#1", Calls: 1, Input: 100, Output: 10, USD: 1.5, Billing: "metered"},
		{Day: "2026-10-27", Model: "a", FactoryJob: "o/r#2", Calls: 1, Input: 50, Output: 5, USD: 0.5, Billing: "metered"},
		{Day: "2026-10-27", Model: "b", FactoryJob: "o/r#1", Calls: 1, Input: 30, Output: 3},
		{Day: "2026-10-27", Model: "b", FactoryJob: "o/r#1", Calls: 1, Unbilled: true},
		{Day: "2026-10-27", Model: "a", Calls: 1, Input: 999, USD: 9},                    // a chat, not a job
		{Day: "2026-10-19", Model: "a", FactoryJob: "o/r#1", Calls: 1, Input: 7, USD: 7}, // before the range
	}
	got := FactoryUsageByModel(lines, "2026-10-26", "2026-11-01")
	want := []FactoryModelUsage{
		{Model: "a", Billing: "metered", Calls: 2, Input: 150, Output: 15, USD: 2, Priced: 2},
		{Model: "b", Calls: 2, Input: 30, Output: 3, Unpriced: 1, Unbilled: 1},
	}
	if !sameModelUsage(got.Models, want) {
		t.Fatalf("models = %+v", got.Models)
	}
	one := got.Jobs["o/r#1"]
	if !sameModelUsage(one, []FactoryModelUsage{
		{Model: "a", Billing: "metered", Calls: 1, Input: 100, Output: 10, USD: 1.5, Priced: 1},
		{Model: "b", Calls: 2, Input: 30, Output: 3, Unpriced: 1, Unbilled: 1},
	}) {
		t.Fatalf("job o/r#1 = %+v", one)
	}
	if len(got.Jobs) != 2 {
		t.Fatalf("jobs = %v", got.Jobs)
	}
}

// Two rows of one model billed different ways say no single way.
func TestFactoryUsageByModelLeavesMixedBillingUnsaid(t *testing.T) {
	got := FactoryUsageByModel([]UsageLine{
		{Day: "2026-10-27", Model: "a", FactoryJob: "o/r#1", Calls: 1, USD: 1, Billing: "metered"},
		{Day: "2026-10-27", Model: "a", FactoryJob: "o/r#1", Calls: 1, Input: 4, Billing: "included"},
	}, "2026-10-27", "2026-10-27")
	if len(got.Models) != 1 || got.Models[0].Billing != "" {
		t.Fatalf("models = %+v", got.Models)
	}
}

func sameModelUsage(a, b []FactoryModelUsage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
