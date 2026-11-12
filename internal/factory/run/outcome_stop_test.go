package run

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// outcomesAfter is the job outcomes recorded after cursor, and the cursor now.
func outcomesAfter(t *testing.T, g *rig, cursor int64) ([]live.Outcome, int64) {
	t.Helper()
	got, err := live.ReadOutcomes(g.st.Root(), &cursor, 100)
	if err != nil || got.Reset {
		t.Fatalf("outcomes after %d: %+v %v", cursor, got, err)
	}
	return got.Events, got.Cursor
}

// A STEP'S OWN FAILURE IS STILL A FAILED JOB: the failure is written, then the
// loop's stop ends the run with that same failure ([floorLoop.stop]), and the
// two writes record it once.
func TestLoopStepFailureRecordsOneFailedOutcome(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{}, errors.New("the worktree is gone")
		}),
	}, nil)
	id := g.add("a failing step", chat("write"), chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "failed", func(it factory.Item) bool { return it.State == factory.StateNew && it.Failure != nil })
	got, _ := outcomesAfter(t, g, 0)
	if len(got) != 1 || got[0].Kind != "failed" || got[0].Code != factory.CodeToolError || got[0].Step != "write" || !got[0].At.Equal(it.Failure.At) {
		t.Fatalf("a step's failure recorded %+v, failure %+v", got, it.Failure)
	}
}

// A PERSON'S STOP IS NO JOB OUTCOME, on the local runner's roads too: Stop at
// an approve gate and in a running step, and a no to the stop after a step.
// Each puts the person's stop on the job as a failure coded stopped; the job
// waits in Backlog, and nothing was recorded for the desktop app to take as a
// failed job.
func TestLoopPersonStopsRecordNoOutcome(t *testing.T) {
	stopped := func(it factory.Item) bool {
		return it.State == factory.StateNew && it.Failure != nil && it.Failure.Code == factory.CodeStopped
	}
	t.Run("Stop at the approve gate", func(t *testing.T) {
		g := newRig(t, map[factory.StageKind]Executor{
			factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { return done("planned"), nil }),
		}, nil)
		id := g.add("stop at the gate", chat("plan"), approveStep(), chat("write"))
		if err := g.r.Launch(id); err != nil {
			t.Fatal(err)
		}
		g.asked(id, "plan is ready · continue, or send it back?")
		waited, cursor := outcomesAfter(t, g, 0)
		if len(waited) != 1 || waited[0].Kind != "needs_you" || waited[0].Need != factory.NeedsApprovePlan {
			t.Fatalf("the wait at the gate: %+v", waited)
		}
		if err := g.r.Stop(id); err != nil {
			t.Fatal(err)
		}
		it := g.wait(id, "stopped at the gate", stopped)
		if it.Failure.Step != factory.ApproveName || factory.JobState(it) != "backlog" {
			t.Fatalf("stop at the gate: %+v, %s", it.Failure, factory.JobState(it))
		}
		if after, _ := outcomesAfter(t, g, cursor); len(after) != 0 {
			t.Fatalf("the stop recorded outcomes: %+v", after)
		}
	})
	t.Run("Stop in a running step", func(t *testing.T) {
		g := newRig(t, map[factory.StageKind]Executor{
			factory.StageChat: ExecutorFunc(func(ctx context.Context, _ Job) (factory.StageResult, error) {
				<-ctx.Done()
				return factory.StageResult{}, ctx.Err()
			}),
		}, nil)
		id := g.add("stop mid-step", chat("plan"), chat("write"))
		if err := g.r.Launch(id); err != nil {
			t.Fatal(err)
		}
		g.wait(id, "in its round", func(it factory.Item) bool {
			return it.Stream != nil && len(it.Stream.Phases) > 0 && it.Stream.Phases[0].State == factory.PhaseRunning
		})
		if err := g.r.Stop(id); err != nil {
			t.Fatal(err)
		}
		if it := g.wait(id, "stopped mid-step", stopped); it.Failure.Step != "plan" {
			t.Fatalf("stop mid-step: %+v", it.Failure)
		}
		if after, _ := outcomesAfter(t, g, 0); len(after) != 0 {
			t.Fatalf("the stop recorded outcomes: %+v", after)
		}
	})
	t.Run("no to the stop after a step", func(t *testing.T) {
		g := newRig(t, map[factory.StageKind]Executor{
			factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { return done("step completed"), nil }),
		}, nil)
		id := g.add("stop after a step", chat("plan"), chat("write"))
		if err := g.st.Update(id, func(it *factory.Item) error { it.StopAfterStep = true; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := g.r.Launch(id); err != nil {
			t.Fatal(err)
		}
		g.wait(id, "holding after the step", func(it factory.Item) bool {
			return it.State == factory.StateNeedsYou && it.QKind == factory.QKindSpendStopped
		})
		_, cursor := outcomesAfter(t, g, 0)
		if err := g.r.Answer(id, false, ""); err != nil {
			t.Fatal(err)
		}
		if it := g.wait(id, "stopped after the step", stopped); factory.JobState(it) != "backlog" {
			t.Fatalf("stop after a step: %s", factory.JobState(it))
		}
		if after, _ := outcomesAfter(t, g, cursor); len(after) != 0 {
			t.Fatalf("the stop recorded outcomes: %+v", after)
		}
	})
}
