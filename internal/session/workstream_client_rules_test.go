package session

import "testing"

// An app's workstream set carries WorkstreamWorkRules verbatim and adds its own
// lines after them, so the rules may take at most half of what a set allows: the
// desk's Afi set was refused when the rules alone were three quarters of the cap.
func TestWorkstreamRulesLeaveRoomInAClientToolSet(t *testing.T) {
	if len(WorkstreamWorkRules) > ClientToolInstructionsMax/2 {
		t.Fatalf("WorkstreamWorkRules is %d bytes; an app's set allows %d, and the rules may take at most half", len(WorkstreamWorkRules), ClientToolInstructionsMax)
	}
}
