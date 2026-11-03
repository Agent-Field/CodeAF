package serve_test

import (
 "encoding/json"
 "fmt"
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
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
   if err := json.Unmarshal(w.Body.Bytes(), &read); err != nil { t.Fatal(err) }
   if len(read.Steps) != 2 || read.Steps[1].When != "touches auth" { t.Fatalf("condition lost in readback: %+v", read.Steps) }
   st := factory.Stage{When: read.Steps[1].When}
   if factory.Fits(st, it) { t.Fatal("conditional step runs on an item without auth") }
   it.Triage.Area = "auth"
   if !factory.Fits(st, it) { t.Fatal("conditional step skips an auth item") }
  })
 }
}
