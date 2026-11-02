package run_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/run"
)

// S8 preserves the worker's six-action bound unless its caller explicitly
// supplies task controls. Both cases use the real worker and its trajectory.
func TestBashWorkerWithoutTaskControlsKeepsItsDefaultLimit(t *testing.T) {
	for _, chosen := range []bool{false, true} {
		name, want := "ordinary", 6
		if chosen {
			name, want = "task controls", 2
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
			store := runOpenStore(t)
			seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolReply(`{"command":"printf 'same answer'"}`), nil
			}}
			var options []run.WorkerOption
			if chosen {
				options = append(options, run.WithTaskControls(effort.None, want))
			}
			worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat, options...)
			report, err := worker.Run(run.WithStepsPerTask(runContext(t), 60), *store.Task(store.RootID()))
			if err == nil || report.Steps != want {
				t.Fatalf("worker ended after %d actions, want %d: %v", report.Steps, want, err)
			}
		})
	}
}
