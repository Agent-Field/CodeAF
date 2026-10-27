package lease_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func stageNames(stages []factory.Stage) []string {
	names := []string{}
	for _, st := range stages {
		names = append(names, st.Name)
	}
	return names
}

// A START RUNS THE PIPELINE THE REPOSITORY'S FILE SAYS NOW: an issue stamped
// with the defaults when it arrived runs the edited steps, whichever door
// starts it; one whose steps were changed for it keeps them.
func TestALaunchRunsTheRepositorysCurrentPipeline(t *testing.T) {
	f := setup(t)
	file, err := pipeline.Parse("pipeline:\n  issue: [plan, write, review, proof]\n")
	if err != nil {
		t.Fatal(err)
	}
	want := stageNames(file.Recipe().For(factory.KindIssue))
	if reflect.DeepEqual(want, stageNames(factory.DefaultRecipe().For(factory.KindIssue))) {
		t.Fatalf("the edited pipeline is the default one: %v", want)
	}
	doors := lease.NewDoors(f.desk).WithStartRead(func(_ context.Context, repo string) (pipeline.File, error) {
		if repo != "acme/api" {
			t.Errorf("read %q", repo)
		}
		return file, nil
	})
	defaults := factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))

	it := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: defaults})
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if names := stageNames(got.Stages); !reflect.DeepEqual(names, want) {
		t.Fatalf("the job runs %v, the file says %v", names, want)
	}
	if got.Stream == nil || len(got.Stream.Phases) != len(want) || got.Stream.Phases[1].Name != want[1] {
		t.Fatalf("the run was built from other steps: %+v", got.Stream)
	}

	edited := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: defaults, Adapted: []string{"you removed test"}})
	if err := doors.Launch(edited.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Get(edited.ID); !reflect.DeepEqual(stageNames(got.Stages), stageNames(defaults)) {
		t.Fatalf("a job's own edit was replaced: %v", stageNames(got.Stages))
	}

	// The factory's rules unread: nothing starts.
	refused := lease.NewDoors(f.desk).WithStartRead(func(context.Context, string) (pipeline.File, error) {
		return pipeline.Defaults(), pipeline.ErrHouseRulesUnread
	})
	held := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: defaults})
	if err := refused.Launch(held.ID); err == nil {
		t.Fatal("a job started with the factory's rules unread")
	}
	if got, _ := f.store.Get(held.ID); got.State != factory.StateNew {
		t.Fatalf("the refused job moved to %s", got.State)
	}
}
