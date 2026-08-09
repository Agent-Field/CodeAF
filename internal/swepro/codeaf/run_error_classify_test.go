package codeaf

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// Contract (run T2): a budget stop is a truthful, checkpointable terminal —
// exit 0, status budget-exhausted — no matter which phase surfaced the
// errRunBudget sentinel (T2 crossed the ceiling inside the audit-fix loop,
// whose callers return the sentinel raw). Harness errors still crash.
func TestClassifyRunErrorMapsBudgetSentinelFromAnyPhase(t *testing.T) {
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)

	wrapped := fmt.Errorf("audit-fix cycle 2: %w", fmt.Errorf(
		"%w: cost $0.6172 >= budget $0.6000", errRunBudget,
	))
	result, err := classifyRunError(runner, pipelineResult{Status: "crashed"}, wrapped)
	if err != nil {
		t.Fatalf("budget sentinel returned an error (would exit 1): %v", err)
	}
	if result.Status != "budget-exhausted" || !strings.Contains(result.Reason, "cost $0.6172") {
		t.Fatalf("result = %#v", result)
	}

	harness := errors.New("provider wiring exploded")
	result, err = classifyRunError(runner, pipelineResult{Status: "crashed"}, harness)
	if !errors.Is(err, harness) || result.Status != "crashed" ||
		result.Reason != "provider wiring exploded" {
		t.Fatalf("harness error result = %#v err = %v", result, err)
	}

	passResult := pipelineResult{Status: "pass"}
	result, err = classifyRunError(runner, passResult, nil)
	if err != nil || result.Status != "pass" {
		t.Fatalf("nil error result = %#v err = %v", result, err)
	}
}
