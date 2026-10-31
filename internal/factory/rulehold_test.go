package factory

import (
	"strings"
	"testing"
)

// AUTO NEVER PASSES AN APPROVAL A RULE HOLDS: the reason names the rule and
// the file, and without a hold the same approval stays routine.
func TestAutoStopsAtAnApprovalARuleHolds(t *testing.T) {
	it := Item{Autonomy: AutonomyAuto, Assignee: "sam", Cap: 5, State: StateNeedsYou, QKind: QKindApprove, RuleHold: "ask first · touch auth/ · auth/login.go"}
	if why := AutonomyBlockReason(it); !strings.Contains(why, "touch auth/ · auth/login.go") {
		t.Fatalf("Auto passed a held approval: %q", why)
	}
	it.RuleHold = ""
	if why := AutonomyBlockReason(it); why != "" {
		t.Fatalf("an unheld approval was blocked: %q", why)
	}
}

// A PLAN THAT LISTED NO FILES IS SAID AS WHAT HAPPENED: the factory could not
// check it, against the paths in force, and the person may approve it or send
// it back. A hold saved before the paths were kept still reads as a sentence.
func TestAutoSaysAPlanWithNoFileListCouldNotBeChecked(t *testing.T) {
	it := Item{Autonomy: AutonomyAuto, Assignee: "assignee-1", Cap: 5, State: StateNeedsYou, QKind: QKindApprove, RuleHold: "protected · auth/ · " + PlanNoFiles}
	want := "The plan didn't list its files, so the factory couldn't check them against auth/. Approve, or send it back to list them."
	if why := AutonomyBlockReason(it); why != want {
		t.Fatalf("Auto says %q, want %q", why, want)
	}
	it.RuleHold = "protected · auth/, infra/ · " + PlanNoFiles
	if why := AutonomyBlockReason(it); !strings.Contains(why, "couldn't check them against auth/, infra/.") {
		t.Fatalf("two paths said as %q", why)
	}
	it.RuleHold = "protected · " + PlanNoFiles
	if why := AutonomyBlockReason(it); why != "The plan didn't list its files, so the factory couldn't check them against its protected paths. Approve, or send it back to list them." {
		t.Fatalf("an older hold said as %q", why)
	}
	it.RuleHold = "protected · auth/ · auth/login.go"
	if why := AutonomyBlockReason(it); why != "A protected path: auth/ · auth/login.go. Review the plan, then press Approve." {
		t.Fatalf("a matched file said as %q", why)
	}
}
