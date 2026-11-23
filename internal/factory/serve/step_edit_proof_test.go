package serve_test

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The public door must carry the bounded refusal without writing a new recipe
// or losing the proof and version the person is about to approve.
func TestJobScreensProofStartedEditRefusesWithoutWriting(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateRunning, Assignee: "sam", Stages: []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "prove it", Until: factory.UntilProven, On: true},
	}, Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
		{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone},
		{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone},
	}}, Proof: []factory.Claim{{Text: "tests pass", OK: true, Evidence: "exit 0"}}})
	before, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "step-edits", `{"add":[{"step":{"name":"tidy","kind":"chat","ask":"tidy it"},"after":"proof"}]}`, 409)
	after, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("refused HTTP edit changed stored item: before=%+v after=%+v", before, after)
	}
}

func TestJobScreensLandedWithoutProofCannotOfferStepEdits(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateLanded, Assignee: "sam", Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true}}})
	if j := f.read(t, it); j.Controls.EditSteps {
		t.Fatal("landed job offers step edits without a new proof")
	}
	before, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "step-edits", `{"add":[{"step":{"name":"tidy","kind":"chat","ask":"tidy it"}}]}`, 409)
	after, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("landed HTTP refusal changed stored item")
	}
}
