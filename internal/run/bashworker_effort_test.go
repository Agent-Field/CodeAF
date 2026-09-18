package run_test

// THE EFFORT HALF OF THE RUN WORKER, read at the seam the seat's provider
// stands on. A run worker is a bash-belt worker, and its belt answers one
// action per response — so the seat is the work seat (internal/session's
// NewBeltWorker sets effort.RoleWork) whose floor is low, and the rung the
// ladder resolves is stamped onto the request context (internal/session's
// loop stamps provider.WithConfiguredEffortRung per attempt) before it reaches
// the provider. The fake completer is the provider here, so the context it is
// handed is the whole of what the seat asked for.

import (
	"context"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/run"
)

// TestBashWorkerAsksTheProviderAtTheWorkSeat pins the wire half of the seat:
// the first call a run worker makes reaches the provider asking for low
// reasoning, because nothing above the role spoke — no task rung, no
// conversation rung and no dialled level — and the work seat's floor is low.
func TestBashWorkerAsksTheProviderAtTheWorkSeat(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)

	var (
		mu      sync.Mutex
		efforts []provider.Effort
	)
	seat := &seat{script: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			mu.Lock()
			efforts = append(efforts, provider.ReasoningEffortFrom(ctx))
			mu.Unlock()
			return textReply("the work is done"), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(efforts) == 0 {
		t.Fatal("the worker made no call to the provider")
	}
	if efforts[0] != provider.EffortLow {
		t.Fatalf("the worker's first call asked the provider for %q, want low at the work seat", efforts[0])
	}
}
