package scheduler

import (
	"context"
	"testing"
)

func TestDispatchObservesInRunTestEvidenceContract(t *testing.T) {
	// Validation contract C3: scheduler outcome logic consumes the leaf's live
	// TestPassed value rather than treating test evidence as permanently absent.
	for _, passed := range []bool{true, false} {
		h := newTerminalHarness(t, LeafRunResult{
			Parts: []LeafPart{{Type: "text", Text: "implemented"}}, TestPassed: &passed,
		})
		h.scheduler.adaptiveCuts = true
		verdict := ReviewVerdict{Verdict: VerdictPass, Raw: map[string]any{}}
		h.scheduler.gate = gateFunc(func(_ context.Context, input GateInput) (GateResult, error) {
			return GateResult{
				Status: GatePass, Verdict: &verdict,
				FinalWorktreePath: input.WorktreePath, FinalBranch: input.Branch,
			}, nil
		})
		result := h.dispatch(phase2Task("root"))
		if !result.Success {
			t.Fatalf("dispatch = %+v", result)
		}
		if h.lastOutcome == nil || h.lastOutcome.Evidence == nil || h.lastOutcome.Evidence.InRunTestsPassed != passed {
			t.Fatalf("outcome = %+v, want inRunTestsPassed=%v", h.lastOutcome, passed)
		}
	}
}
