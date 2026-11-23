package worker

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// A pulled job carrying a merge signal is stood up for the runner's Merge
// door, never launched: its steps already ran and its pull request is out.
func TestAMergeSignalStandsTheJobForMergeNotForItsSteps(t *testing.T) {
	it := factory.Item{State: factory.StateRunning, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "code", State: factory.PhaseDone, Round: 1}}}}
	door := stand(&it, []api.WorkerSignal{{Verb: lease.SignalMerge, Words: "abc123"}})
	if door == nil || it.State != factory.StateShipped {
		t.Fatalf("stood up as %s", it.State)
	}
}
