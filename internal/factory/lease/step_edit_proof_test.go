package lease_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
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

// A STEP EDIT TO A JOB THAT NEVER RAN LANDS ON ITS REPOSITORY'S STEPS. The job
// arrives on the built-in list; the edit once made that list the job's own, so
// the job ran the built-in steps with the edit and never the repository's.
func TestEditStepsBeforeStartKeepsTheRepositorysPipeline(t *testing.T) {
	f := setup(t)
	file, err := pipeline.Parse("pipeline:\n  issue: [write, audbcopy, review, proof]\nsteps:\n  audbcopy:\n    prompt: copy the docs\n    until: done\n    max: 1\n")
	if err != nil || len(file.Problems) > 0 {
		t.Fatalf("parse: %v %v", err, file.Problems)
	}
	it := f.add(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Stages: factory.DefaultRecipe().For(factory.KindIssue)})
	tidy := factory.Stage{Name: "tidy", Kind: factory.StageChat, Ask: "tidy up", Until: "done", On: true}
	edit := factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: tidy, After: "review"}}}
	got, _, err := lease.NewDoors(f.desk).EditSteps(it.ID, edit, file.Recipe())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got.Stages {
		names = append(names, s.Name)
	}
	if want := []string{"write", "audbcopy", "review", "tidy", "proof"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("steps after the edit = %v, want %v", names, want)
	}
}
