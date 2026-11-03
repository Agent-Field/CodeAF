package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestJobScreensAddedStepConditionSurvives(t *testing.T) {
	for _, kind := range []string{"chat", "check", "gate"} {
		t.Run(kind, func(t *testing.T) {
			f := newV2Jobs(t)
			f.seam.Edit = factory.LocalSeam(f.st, now).Edit
			reopenJobs(t, f)
			it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true}}})
			f.post(t, it, "step-edits", fmt.Sprintf(`{"add":[{"step":{"name":"inspect","kind":%q,"ask":"inspect it","when":"touches auth"}}]}`, kind), 200)
			w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d/step-edits", it.ID), "")
			checkJSON(t, w, 200)
			var read api.StepEdits
			if err := json.Unmarshal(w.Body.Bytes(), &read); err != nil {
				t.Fatal(err)
			}
			if len(read.Steps) != 2 || read.Steps[1].When != "touches auth" {
				t.Fatalf("condition lost in readback: %+v", read.Steps)
			}
			bad := f.post(t, it, "step-edits", fmt.Sprintf(`{"add":[{"step":{"name":"another","kind":%q,"ask":"inspect it","when":"banana"}}]}`, kind), 409)
			if !strings.Contains(bad.Body.String(), "when is one of") {
				t.Fatalf("unclear condition refusal: %s", bad.Body)
			}
			st := factory.Stage{When: read.Steps[1].When}
			if !factory.Fits(st, it) {
				t.Fatal("conditional step skips while files and area are unknown")
			}
			it.Triage.Area = "auth"
			if !factory.Fits(st, it) {
				t.Fatal("conditional step skips an auth item")
			}

			// A known nonmatching area is still a reason to skip at the step.
			if err := f.st.Update(it.ID, func(it *factory.Item) error { it.Triage.Area = "docs"; return nil }); err != nil {
				t.Fatal(err)
			}

			runner := run.New(run.Options{Store: f.st, Exec: map[factory.StageKind]run.Executor{
				factory.StageChat: run.ExecutorFunc(func(_ context.Context, job run.Job) (factory.StageResult, error) {
					if job.Stage.Name != "write" {
						t.Errorf("conditional step executed: %s", job.Stage.Name)
					}
					return factory.StageResult{Done: true}, nil
				}),
			}})
			t.Cleanup(func() { _ = runner.Stop(it.ID) })
			if err := runner.Launch(it.ID); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				job := f.read(t, it)
				if job.Ready {
					if len(job.Steps) != 2 || job.Steps[0].State != api.StepDone || !job.Steps[1].InRun || job.Steps[1].State != api.StepDone || !strings.Contains(job.Steps[1].Note, "does not match the triage area") || job.Steps[1].Round != 0 {
						t.Fatalf("run did not skip conditional step: %+v", job.Steps)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("run never completed without conditional step: %+v", job)
				}
				time.Sleep(2 * time.Millisecond)
			}
		})
	}
}
