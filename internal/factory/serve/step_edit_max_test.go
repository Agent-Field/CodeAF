package serve_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE JOB'S PERSON SETS ONE STEP'S TRIES THROUGH THE STEP-EDIT ROUTE, and the
// job's steps read it as steps[].max. Someone else's is refused, and so is a
// fourth try.
func TestJobScreensAPersonSetsAStepsTries(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Stages: []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "review", Kind: factory.StageChat, Ask: "read it", Until: factory.UntilClean, Max: 2, On: true},
	}})
	f.person = "mei"
	f.post(t, it, "step-edits", `{"max":{"review":3}}`, 403)
	f.person = "sam"
	f.post(t, it, "step-edits", `{"max":{"review":4}}`, 409)
	f.post(t, it, "step-edits", `{"max":{"review":3}}`, 200)
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d/step-edits", it.ID), "")
	var read api.StepEdits
	if err := json.Unmarshal(w.Body.Bytes(), &read); err != nil {
		t.Fatal(err)
	}
	if len(read.Steps) != 2 || read.Steps[1].Max != 3 {
		t.Fatalf("the steps read %+v", read.Steps)
	}
}
