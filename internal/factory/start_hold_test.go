package factory

import "testing"

// A ROUTING RULE'S START HOLD IS NOT AUTO STOPPING. A routed job held to ask first in Auto
// read "Auto stopped: Approval needed: start · routing rule …" on the desk,
// which prefixes this reason so; nothing had run. The hold's own question
// says what it waits on, and the reason is empty.
func TestAStartHoldGivesNoReasonAutoStopped(t *testing.T) {
	it := Item{Autonomy: AutonomyAuto, State: StateNeedsYou, QKind: QKindAskFirst, Cap: 5, Route: "ops",
		AskedFirstReason: "routing rule ops", Question: "Approval needed: start · routing rule ops"}
	if why := AutonomyBlockReason(it); why != "" {
		t.Fatalf("a start hold says Auto stopped because %q", why)
	}
	// A label hold on a GitHub issue no rule routed reads as it always did.
	it.Route, it.Question = "", "Approval needed: start · ask first · touches auth"
	if why := AutonomyBlockReason(it); why != it.Question {
		t.Fatalf("a label hold's reason changed to %q", why)
	}
}
