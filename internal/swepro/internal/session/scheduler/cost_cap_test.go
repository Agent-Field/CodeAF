package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/loopguard"
	"github.com/Agent-Field/swe-pro-go/internal/session/sizeband"
)

type errorLedgerStepLoop struct{ result LeafRunResult }

func (step errorLedgerStepLoop) RunLeaf(context.Context, LeafRunRequest) (LeafRunResult, error) {
	return step.result, errors.New("stream failed")
}

func TestLeafCostEnvironmentCapTripsOnThreadedCallCost(t *testing.T) {
	// Round-2 contract: CODEAF_LEAF_MAX_COST_USD is parsed into the guard used
	// by inspectLeafAttempt, and a per-call cost attached to a tool stops it.
	t.Setenv("CODEAF_LEAF_MAX_COST_USD", "0.005")
	cost := 0.01
	current := LeafRunResult{Parts: []LeafPart{{
		Type: "tool", Tool: "bash", ArgsKey: `{"command":"true"}`, CostUSD: &cost,
	}}}
	inspection := inspectLeafAttempt(
		sizeband.BandS, []LeafRunResult{current}, current, 0,
		loopguard.CreateLoopGuard(leafLoopGuardOptions()),
	)
	if !strings.Contains(inspection.LoopStopReason, "cost budget reached") {
		t.Fatalf("loop stop reason = %q", inspection.LoopStopReason)
	}
}

func TestLeafCostCapCountsExpensiveTerminalCallAfterCheapToolTurn(t *testing.T) {
	// Round-2 full-call-cost contract: the engine ledger charges both a cheap
	// tool-bearing turn and an expensive terminal text turn. The latter has no
	// LeafPart cost but must still fail the leaf cap without adding an action.
	maximum := 0.01
	cheap := 0.001
	current := LeafRunResult{
		CallCosts: []float64{cheap, 0.02}, CostUSD: 0.021,
		Parts: []LeafPart{
			{Type: "tool", Tool: "read", ArgsKey: `{"filePath":"x"}`, CostUSD: &cheap},
			{Type: "text", Text: "done"},
		},
	}
	guard := loopguard.CreateLoopGuard(loopguard.LoopGuardOptions{MaxCostUsd: &maximum})
	inspection := inspectLeafAttempt(sizeband.BandS, []LeafRunResult{current}, current, 0, guard)
	if !strings.Contains(inspection.LoopStopReason, "cost budget reached (0.021/0.01 USD)") {
		t.Fatalf("loop stop reason = %q", inspection.LoopStopReason)
	}
	if got := guard.Snapshot().ActionCount; got != 1 {
		t.Fatalf("terminal cost charging changed action count to %v", got)
	}
}

func TestErroredLeafPreservesPaidCallLedgerForCostCap(t *testing.T) {
	// Finding 2 contract: synthesizing a scheduler error keeps the paid call
	// ledger, so a cap below the spend still fires.
	paid := 0.01
	s := &Scheduler{stepLoop: errorLedgerStepLoop{result: LeafRunResult{
		CostUSD: paid, CallCosts: []float64{paid}, Parts: []LeafPart{{
			Type: "tool", Tool: "bash", ArgsKey: `{"command":"paid"}`,
		}},
	}}}
	got := s.callLeaf(context.Background(), LeafRunRequest{}, "leaf", "coder")
	if len(got.CallCosts) != 1 || got.CallCosts[0] != paid || got.CostUSD != paid {
		t.Fatalf("errored ledger = total %v calls %v", got.CostUSD, got.CallCosts)
	}
	maximum := 0.005
	inspection := inspectLeafAttempt(
		sizeband.BandS, []LeafRunResult{got}, got, 0,
		loopguard.CreateLoopGuard(loopguard.LoopGuardOptions{MaxCostUsd: &maximum}),
	)
	if !strings.Contains(inspection.LoopStopReason, "cost budget reached") {
		t.Fatalf("loop stop reason = %q", inspection.LoopStopReason)
	}
}

func TestLeafTurnMonitorStopsCostBeforeNextProviderTurn(t *testing.T) {
	// Finding 1 cost contract: the full-call leaf cost ceiling is evaluated at
	// the turn boundary, before the engine can issue another provider call.
	maximum := 0.005
	monitor := newLeafTurnMonitor(loopguard.CreateLoopGuard(loopguard.LoopGuardOptions{
		MaxCostUsd: &maximum,
	}))
	err := monitor.Observe(context.Background(), LeafTurnObservation{CostUSD: 0.01})
	if err == nil || !strings.Contains(err.Error(), "cost budget reached") {
		t.Fatalf("live cost guard error = %v", err)
	}
}

func TestLeafTurnMonitorStopsEmptyToolCallProtocolLoop(t *testing.T) {
	// Finding 1 contract: repeated tool-call finishes with no calls are bounded
	// even though the TS loop has no in-flight guard for this provider defect.
	monitor := newLeafTurnMonitor(nil)
	turn := LeafTurnObservation{Finish: "tool-calls"}
	for index := 0; index < maxConsecutiveEmptyToolTurns-1; index++ {
		if err := monitor.Observe(context.Background(), turn); err != nil {
			t.Fatalf("early stop at turn %d: %v", index+1, err)
		}
	}
	err := monitor.Observe(context.Background(), turn)
	if err == nil || !strings.Contains(err.Error(), "3 consecutive") {
		t.Fatalf("protocol guard error = %v", err)
	}
}
