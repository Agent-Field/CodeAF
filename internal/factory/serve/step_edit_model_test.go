package serve_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE JOB'S PERSON PICKS ONE STEP'S MODEL THROUGH THE STEP-EDIT ROUTE, and
// every read of the job's steps says the pick as steps[].use_model, beside
// steps[].model, which stays the model the step ran on. Someone else's pick is
// refused; "" gives the step back to the job's model.
func TestJobScreensAPersonPicksAStepsModel(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Stages: []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "test", Kind: factory.StageCheck, Ask: "go test ./...", On: true},
		{Name: "review", Kind: factory.StageChat, Ask: "read it", On: true},
	}})
	f.person = "mei"
	f.post(t, it, "step-edits", `{"model":{"review":"anthropic/claude-opus-5.5"}}`, 403)
	f.person = "sam"
	f.post(t, it, "step-edits", `{"model":{"test":"anthropic/claude-opus-5.5"}}`, 409)
	f.post(t, it, "step-edits", `{"model":{"review":"anthropic/claude-opus-5.5"}}`, 200)
	w := verbRequest(f.h, "GET", fmt.Sprintf("/v1/jobs/%d/step-edits", it.ID), "")
	var read api.StepEdits
	if err := json.Unmarshal(w.Body.Bytes(), &read); err != nil {
		t.Fatal(err)
	}
	picked := map[string]string{}
	for _, s := range read.Steps {
		picked[s.Name] = s.UseModel
	}
	if picked["review"] != "anthropic/claude-opus-5.5" || picked["write"] != "" {
		t.Fatalf("the steps read %+v", read.Steps)
	}
	f.post(t, it, "step-edits", `{"model":{"review":""}}`, 200)
	if cur, _ := f.st.Get(it.ID); cur.Stages[2].Model != "" {
		t.Fatalf("the pick was not given back: %+v", cur.Stages[2])
	}
}
