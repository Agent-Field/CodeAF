//go:build !windows

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// A budget stop is a truthful, checkpointable terminal — exit 0, status
// budget-exhausted — however deeply the errRunBudget sentinel is wrapped.
// Any other error still crashes.
func TestClassifyRunErrorMapsBudgetSentinelFromAnyPhase(t *testing.T) {
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)

	wrapped := fmt.Errorf("landing turn: %w", fmt.Errorf(
		"%w: cost $0.6172 >= budget $0.6000", errRunBudget,
	))
	result, err := classifyRunError(context.Background(), runner, pipelineResult{Status: "crashed"}, wrapped)
	if err != nil {
		t.Fatalf("budget sentinel returned an error (would exit 1): %v", err)
	}
	if result.Status != "budget-exhausted" || !strings.Contains(result.Reason, "cost $0.6172") {
		t.Fatalf("result = %#v", result)
	}

	infrastructure := errors.New("provider wiring exploded")
	result, err = classifyRunError(context.Background(), runner, pipelineResult{Status: "crashed"}, infrastructure)
	if !errors.Is(err, infrastructure) || result.Status != "crashed" ||
		result.Reason != "provider wiring exploded" {
		t.Fatalf("infrastructure error result = %#v err = %v", result, err)
	}

	passResult := pipelineResult{Status: "pass"}
	result, err = classifyRunError(context.Background(), runner, passResult, nil)
	if err != nil || result.Status != "pass" {
		t.Fatalf("nil error result = %#v err = %v", result, err)
	}
}

// codeaf stops a run with SIGTERM, which ends its context. That is a stop, not
// a program that broke: the run's error is the context's own, and the ending
// says the work did not finish, carrying the run's own account of how far it
// got. A ceiling the run had already crossed is still the ceiling.
func TestAStopFromOutsideIsNotACrash(t *testing.T) {
	runner := newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	stopped, stop := context.WithCancel(context.Background())
	stop()

	result, err := classifyRunError(stopped, runner, pipelineResult{
		Status:   "crashed",
		Terminal: map[string]any{"reason": "no submission: the run stopped without calling submit"},
	}, context.Canceled)
	if err != nil {
		t.Fatalf("a stop returned an error, which the ending would call a crash: %v", err)
	}
	if result.Status != "fail" || !strings.HasPrefix(result.Reason, "stopped before it finished") ||
		!strings.Contains(result.Reason, "without calling submit") {
		t.Fatalf("stopped result = %#v", result)
	}

	spent := 1.0
	budgeted := newPipeline(cliArgs{MaxCost: &spent}, t.TempDir(), pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(budgeted.runtime.Close)
	budgeted.runtime.addCost(2)
	result, err = classifyRunError(stopped, budgeted, pipelineResult{Status: "crashed"}, context.Canceled)
	if err != nil || result.Status != "budget-exhausted" {
		t.Fatalf("a stop past the ceiling = %#v err = %v, want budget-exhausted", result, err)
	}
}
