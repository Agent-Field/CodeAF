package session

import (
	"context"
	"slices"
	"testing"
	"time"
)

// The actual handoff must carry a previously declared verifier onto the node,
// while stale declarations and a handoff of only part of the ask stay scoped.
func TestHandoffCarriesOnlyCurrentWholeRequestChecks(t *testing.T) {
	for _, mode := range []string{"whole request", "existing route declaration", "stale route declaration", "different person request", "different turn obligation", "independent remainder", "work still held", "no declaration"} {
		t.Run(mode, func(t *testing.T) {
			completer := &routeCompleter{handoff: custodyDraft}
			a, _, _ := routeAgent(t, completer)
			a.personAsk = routeAsk
			steward := NewSteward(routeAsk, Budget{Wall: time.Hour}, nil)
			declared := []string{"sh ./verify.sh"}
			if mode == "no declaration" {
				declared = nil
			}
			if !steward.setAcceptanceContract(routeAsk, "the requested repository changes are complete", declared) {
				t.Fatal("could not set the existing check declaration")
			}
			a.principal = steward
			read := checkpointRead{}
			verdict := routeVerdict{Work: true, Goal: routeAsk}
			want := declared
			switch mode {
			case "existing route declaration":
				verdict.Checks, verdict.checksRequest = []string{"sh ./route-check.sh"}, routeAsk
				want = verdict.Checks
			case "stale route declaration":
				verdict.Checks, verdict.checksRequest = []string{"sh ./stale-check.sh"}, "an earlier request"
				want = nil
			case "different person request":
				a.personAsk = "implement the unrelated repository change and report its result"
				want = nil
			case "different turn obligation":
				a.owedAsks = []owedAsk{{from: owedByResult, text: "report the earlier task's outcome"}}
				want = nil
			case "work still held":
				read.held = []heldPiece{{id: 99, title: "separate ongoing investigation"}}
				want = nil
			case "independent remainder":
				read.ownRemainder = custodyHeldPart
				want = nil
			}
			taken := Decision{Verb: DecideCarryOn, Brief: "finish the repository changes"}
			over := a.handOverRunningTurn(context.Background(), newEventHub(), &Usage{}, time.Now(), a.model,
				checkpointSplitNote, checkpointSeamMark, 0, nil, verdict, read, &taken)
			if !over.moved || over.taskID == 0 {
				t.Fatalf("actual handoff did not create work: %+v", over)
			}
			node := a.graph().node(over.taskID)
			if got := node.repeatableChecks(); !slices.Equal(got, want) {
				t.Fatalf("admitted checks = %q, want %q", got, want)
			}
			if !slices.Equal(steward.declaredChecks(), declared) {
				t.Fatal("handoff changed the session's declaration")
			}
			if mode == "whole request" {
				ground := t.TempDir()
				writeCheckFile(t, ground, "verify.sh", "#!/bin/sh\nprintf VERIFIED", 0o755)
				door := auditDoorFor(node, ground)
				if text, refused := typedAtTheChecker(t, checkerBash(t, door, ground), "sh ./verify.sh"); refused || text == "" {
					t.Fatalf("carried declaration did not reach the real checker: %q", text)
				}
			}
		})
	}
}
