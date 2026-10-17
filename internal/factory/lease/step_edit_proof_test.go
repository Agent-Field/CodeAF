package lease_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// THE LEASE DOOR REFUSES THROUGH factory.Edit: a landed job whose proof is done
// takes no new stage, and the refusal writes nothing.
func TestEditStepsAfterProofIsRefusedAndWritesNothing(t *testing.T) {
	f := setup(t)
	stage := func(name string) factory.Stage {
		return factory.Stage{Name: name, Kind: factory.StageChat, Ask: "do " + name, Until: "done", On: true}
	}
	it := f.add(t, factory.Item{
		State:  factory.StateLanded,
		Kind:   factory.KindIssue,
		Stages: []factory.Stage{stage("plan"), stage("proof")},
		Proof:  []factory.Claim{{Text: "tests pass", OK: true, Evidence: "exit 0"}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone},
			{Name: "proof", Kind: factory.StageChat, State: factory.PhaseDone},
		}},
	})
	was, err := f.store.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	edit := factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: stage("tidy"), After: "proof"}}}
	_, lines, err := lease.NewDoors(f.desk).EditSteps(it.ID, edit, factory.DefaultRecipe())
	var refusal factory.EditRefusal
	if err == nil || !errors.As(err, &refusal) || len(lines) != 0 {
		t.Fatalf("add after proof on a landed job: lines=%v err=%v", lines, err)
	}
	now, err := f.store.Get(it.ID)
	if err != nil || !reflect.DeepEqual(now, was) {
		t.Fatalf("a refused edit wrote the job:\nnow %+v\nwas %+v (%v)", now, was, err)
	}
}
