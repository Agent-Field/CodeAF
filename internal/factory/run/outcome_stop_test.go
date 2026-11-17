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

// AN ANSWER THAT STOPS THE JOB IS THE PERSON'S STOP, on the runner's own
// copy of the job as at the Stop door: a cancel to a step out of rounds and a
// no to the budget end the run, and the job waits in Backlog asking nothing,
// with no outcome. The job here was started by a person (a Start takes any
// earlier stop down, factory.ClearPersonStop), which is the copy a runner
// pulls from a Factory service; the loop's stop left it reading as a failed
// job waiting on that person ("Stopped by you" in Needs you).
func TestLoopAnswersThatStopLeaveTheJobInBacklog(t *testing.T) {
	stopped := func(it factory.Item) bool {
		return it.State == factory.StateNew && it.Stream != nil && !it.Stream.Ended.IsZero()
	}
	started := func(t *testing.T, g *rig, id int) {
		t.Helper()
		if err := g.st.Update(id, func(it *factory.Item) error { factory.ClearPersonStop(it); return nil }); err != nil {
			t.Fatal(err)
		}
		if err := g.r.Launch(id); err != nil {
			t.Fatal(err)
		}
	}
	check := func(t *testing.T, g *rig, id int, cursor int64, step, need string) {
		t.Helper()
		it := g.wait(id, "stopped by the answer", stopped)
		cut := it.Stream.Phases[it.Stream.Cur]
		if cut.Name != step || cut.Failure == nil || cut.Failure.Code != factory.CodeStopped {
			t.Fatalf("the answer cut no step: %+v", cut)
		}
		if factory.JobState(it) != "backlog" || !factory.StoppedByPerson(it) {
			t.Errorf("the stopped job reads %s (failure %+v, person stopped %v), want backlog", factory.JobState(it), it.Failure, factory.StoppedByPerson(it))
		}
		// The stop records nothing. (The runner's copy of a loop's wait has
		// no clock of its own, so a write before the answer may record that
		// same wait again; this copy's outcomes are never served.)
		after, _ := outcomesAfter(t, g, cursor)
		for _, e := range after {
			if e.Kind != "needs_you" || e.Need != need || !e.At.Before(cut.Failure.At) {
				t.Errorf("the answer's stop recorded %+v", e)
			}
		}
	}
	t.Run("cancel a step out of rounds", func(t *testing.T) {
		g := newRig(t, map[factory.StageKind]Executor{
			factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
				return factory.StageResult{Done: true, Findings: 2}, nil
			}),
		}, nil)
		review := chat("review")
		review.Until, review.Max = "clean", 1
		id := g.add("cancel when stuck", review, chat("proof"))
		started(t, g, id)
		g.wait(id, "out of rounds", func(it factory.Item) bool {
			return it.State == factory.StateNeedsYou && it.QKind == factory.QKindOutOfAttempts
		})
		_, cursor := outcomesAfter(t, g, 0)
		if err := g.r.Answer(id, false, factory.ChoiceCancel); err != nil {
			t.Fatal(err)
		}
		check(t, g, id, cursor, "review", factory.NeedsOutOfAttempts)
	})
	t.Run("no to the budget", func(t *testing.T) {
		g := newRig(t, map[factory.StageKind]Executor{
			factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
				res := done("")
				res.Spent = 1.5
				return res, nil
			}),
		}, nil)
		id := g.add("no more money", chat("write"), chat("proof"))
		if err := g.st.Update(id, func(it *factory.Item) error { it.Cap = 1; return nil }); err != nil {
			t.Fatal(err)
		}
		started(t, g, id)
		g.wait(id, "at its budget", func(it factory.Item) bool { return it.State == factory.StateNeedsYou && it.QKind == "cap" })
		_, cursor := outcomesAfter(t, g, 0)
		if err := g.r.Answer(id, false, ""); err != nil {
			t.Fatal(err)
		}
		check(t, g, id, cursor, "write", factory.NeedsSpendingLimit)
	})
}
