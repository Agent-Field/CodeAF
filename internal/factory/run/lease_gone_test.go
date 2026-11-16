package run

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

// A STOP THE WORKER HEARS THROUGH ITS LEASE IS NOT A FAILED STEP. The factory
// revokes the lease when a person presses Stop, and the worker's step can end
// on that refusal before the stop cancels the runner's own loop. On a busy box
// that gap was wide enough that the runner stamped a failure and paid the
// job's manager to explain it (TestMemoryPausedJobLifecycle/stop_tail, under
// load on 2026-11-16). The job left this runner; it writes nothing about it.
func TestALeaseTakenBackIsNotAStepFailure(t *testing.T) {
	for name, gone := range map[string]error{
		"stopped by a person": client.ErrLeaseStopped,
		"withdrawn":           client.ErrLeaseWithdrawn,
		"gone, wrapped":       fmt.Errorf("stage report: %w", client.ErrLeaseGone),
	} {
		t.Run(name, func(t *testing.T) {
			var ran, asked atomic.Int32
			g, _ := managed(t, map[factory.StageKind]Executor{
				factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
					ran.Add(1)
					return factory.StageResult{}, gone
				}),
			}, newFakeTalk(), time.Hour)
			g.r.opts.FailureSummary = func(context.Context, factory.Item, factory.Failure) (string, error) {
				asked.Add(1)
				return "a failure nobody had", nil
			}
			id := g.add("fix it", chat("write"))
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for ran.Load() == 0 || g.r.Running(id) {
				if time.Now().After(deadline) {
					t.Fatalf("the runner kept the job after its lease went: ran=%d", ran.Load())
				}
				time.Sleep(3 * time.Millisecond)
			}
			it, err := g.st.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if it.Failure != nil {
				t.Fatalf("a lease taken back was stamped as a failure: %+v", it.Failure)
			}
			if asked.Load() != 0 {
				t.Fatalf("the manager was asked to explain a failure %d times", asked.Load())
			}
		})
	}
}
