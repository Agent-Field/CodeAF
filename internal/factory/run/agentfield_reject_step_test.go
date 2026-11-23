package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

// A STEP WHOSE RUN ENDED ON THE PERSON'S NO IS REJECTED HERE, NOT FAILED. Job
// 74 of the third live run: the person rejected the AgentField plan, the
// control plane cancelled the run ("approval_rejected: <their words>"), and
// the step's executor returned that cancel as an error. The runner stamped the
// build step `tool_error` and logged `step_failed tool_error · build ·
// stopped` before the factory ended the job Rejected, so the desk's rail read
// "failed" where it should read "rejected here". The step now ends as a native
// Reject leaves its step: failed, with no failure on it and none on the job,
// and no step_failed event.
func TestAnAgentFieldStepEndedByARejectCarriesNoFailure(t *testing.T) {
	var g *rig
	exec := ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
		// The step's follower writes the run's end as the control plane
		// reports it, then answers how it ended.
		_ = g.st.Update(job.Item.ID, func(it *factory.Item) error {
			it.AgentFieldSteps = map[string]*factory.AgentFieldRun{"build": {Target: "planner.build", ExecutionID: "exec-root", RunID: "run-1",
				Status: "cancelled", Error: factory.AgentFieldRejectCancel + ": Not this release", Ended: time.Now()}}
			return nil
		})
		return factory.StageResult{}, errors.New("the AgentField run cancelled: " + factory.AgentFieldRejectCancel + ": Not this release")
	})
	var history *record.Record
	g = newRig(t, map[factory.StageKind]Executor{factory.StageAgentField: exec}, func(o *Options) {
		var err error
		history, err = record.Open(o.Store, record.Options{})
		if err != nil {
			t.Fatal(err)
		}
		o.Record = history
	})
	id := g.add("add Credits", factory.Stage{Name: "build", Kind: factory.StageAgentField, Target: "planner.build"})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "ended", func(it factory.Item) bool {
		return it.Stream != nil && len(it.Stream.Phases) == 1 && it.Stream.Phases[0].State == factory.PhaseFailed
	})
	if f := it.Stream.Phases[0].Failure; f != nil {
		t.Fatalf("the rejected step carries a failure: %+v", f)
	}
	if it.Failure != nil {
		t.Fatalf("the job carries a failure for the person's no: %+v", it.Failure)
	}
	events, err := history.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Job == id && e.Kind == record.StepFailed {
			t.Fatalf("the person's no was logged as a failed step: %+v", e)
		}
	}
}
