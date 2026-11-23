package serve

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

func TestCompletedResultRecoveryRefusesInterruptedOrUnverifiedResults(t *testing.T) {
	started := time.Date(2026, 10, 23, 14, 0, 0, 0, time.UTC)
	it := factory.Item{State: factory.StateNeedsYou, QKind: "results_not_saved", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageCheck, State: factory.PhaseRunning, Round: 3, Started: started}}}}
	for _, mode := range []string{"complete", "checkpoint", "execution blocked", "budget stopped", "verification failed"} {
		t.Run(mode, func(t *testing.T) {
			result := factory.StageResult{Done: true}
			switch mode {
			case "checkpoint":
				result.Checkpoint = true
			case "execution blocked":
				result.ExecutionBlocked = "waiting for owned checks"
			case "budget stopped":
				result.JobBudgetStopped = true
			case "verification failed":
				result.Notes = []string{verification.FailurePrefix + " focused regression did not pass"}
			}
			err := validateRetainedReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: "test", State: factory.PhaseDone, Round: 3, Started: started, Result: &result}}})
			if (err == nil) != (mode == "complete") {
				t.Fatalf("%s recovery: %v", mode, err)
			}
		})
	}
}
