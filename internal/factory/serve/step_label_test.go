package serve_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAddedStepLabelReachesTheSavedJob(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true}}})
	f.post(t, it, "step-edits", `{"add":[{"step":{"name":"securityrevi","label":"Security review","ask":"review security","until":"clean"},"after":"write"}]}`, 200)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Stages) != 2 || saved.Stages[1].Name != "securityrevi" || saved.Stages[1].Label != "Security review" {
		t.Fatalf("added step lost label or id: %+v", saved.Stages)
	}
}
