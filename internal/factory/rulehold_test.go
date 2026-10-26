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
