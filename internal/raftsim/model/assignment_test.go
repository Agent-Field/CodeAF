package model

import (
	"github.com/anishathalye/porcupine"
	"testing"
)

func TestR18AssignmentOracleRejectsTwoSuccessfulOwners(t *testing.T) {
	initial := AssignmentState{Holder: "owner", Fence: 1, Status: "pending", Revision: 1, Incarnation: 1, Available: true, ParentFence: 1}
	history := []AssignmentOperation{{ID: 1, Call: 1, Return: 3, Input: AssignmentCall{Kind: "claim"}, Code: "ok"}, {ID: 2, Call: 1, Return: 3, Input: AssignmentCall{Kind: "acquire"}, Code: "ok"}}
	if CheckAssignments(initial, history) != porcupine.Illegal {
		t.Fatal("R18 oracle accepted two successful owners")
	}
	history[1].Code = "lease_held"
	if CheckAssignments(initial, history) != porcupine.Ok {
		t.Fatal("R18 oracle rejected legitimate race")
	}
	history[0].Call = 4
	history[0].Return = 5
	if CheckAssignments(initial, history) != porcupine.Illegal {
		t.Fatal("R18 real-time order was dropped")
	}
	history[0].Call = 1
	history[0].Unknown = true
	if CheckAssignments(initial, history) != porcupine.Ok {
		t.Fatal("R18 unknown commit was dropped")
	}
}
