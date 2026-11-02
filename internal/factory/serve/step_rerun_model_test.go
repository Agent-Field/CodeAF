package serve_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// RUN AGAIN FROM HERE WITH ANOTHER MODEL is the step rerun route with
// `model`: the step reruns on it, and the job's steps read it as use_model. A
// stop takes no model.
func TestJobScreensARerunCanNameTheStepsModel(t *testing.T) {
	f := newV2Jobs(t)
	run := recordedRun()
	it := f.create(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Stream: run,
		Stages: []factory.Stage{{Name: run.Phases[0].Name, Kind: factory.StageChat, Ask: "plan it", On: true}, {Name: run.Phases[1].Name, Kind: factory.StageChat, Ask: "write it", On: true}}})
	if _, err := lease.Open(f.st, f.cfg.LeaseOptions); err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "steps/write/stop", `{"model":"vendor/new"}`, 422)
	f.post(t, it, "steps/write/rerun", `{"model":"two words"}`, 409)
	f.post(t, it, "steps/write/rerun", `{"model":"vendor/new"}`, 200)
	cur, _ := f.st.Get(it.ID)
	if cur.Stages[1].Model != "vendor/new" || cur.Stream.Phases[1].State != factory.PhasePending {
		t.Fatalf("rerun with a model: %+v %+v", cur.Stages[1], cur.Stream.Phases[1])
	}
	if j := f.read(t, it); j.Steps[1].UseModel != "vendor/new" {
		t.Fatalf("the job reads %+v", j.Steps[1])
	}
}
