package session

import "testing"

// This mirrors a kept task whose requested implementation never reached the
// deliverable. The original failing suite also failed before work began.
func TestBenchmarkKeptBranchCannotCompleteAnUnchangedDeliverable(t *testing.T) {
	decision := budgetLeft(t).Decide(Remains{
		Acceptance: "Implement target_connector and add_sqlsink in the main workspace and run pytest -q",
		Delivery:   deliveryContract{Kind: "workspace", Quote: "in the main workspace"},
		Landings: []Landing{{ID: 1, Title: "shared connector", State: TaskDone,
			Merged: false, Checked: false, Files: []string{"singer_sdk/target_base.py"},
			Report: "its branch task/shared-connector was kept: your checkout is on main, which tasks do not merge into automatically"}},
		BaselineRead: true,
		WasFailing:   []string{"pytest -q"},
		Checks:       []CheckRun{{Command: "pytest -q", Ran: true, Passed: false}},
	})
	if decision.Verb == DecideDone {
		t.Fatalf("unchanged requested workspace was called done: %+v", decision)
	}
}
