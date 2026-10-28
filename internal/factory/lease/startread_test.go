package lease_test

import (
	"context"
	"errors"
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
	doors := lease.NewDoors(f.desk).WithStartRead(func(_ context.Context, it factory.Item) (pipeline.File, error) {
		if repo := factory.RepoOf(it); repo != "acme/api" {
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

	// A job made with steps of its own keeps them.
	own := []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}}
	made := f.add(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNew, Stages: own})
	if err := doors.Launch(made.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Get(made.ID); !reflect.DeepEqual(stageNames(got.Stages), []string{"test"}) {
		t.Fatalf("a job's own steps were replaced: %v", stageNames(got.Stages))
	}

	// The factory's rules unread: nothing starts.
	refused := lease.NewDoors(f.desk).WithStartRead(func(context.Context, factory.Item) (pipeline.File, error) {
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

// EVERY START CARRIES ITS START RULES, whichever door started it (a new job
// started at once, a hand-off, the manager's start): the repository's
// ask-first and never lines with the factory's, so the approval after the
// plan is checked. Rules a door already stamped stay; a repository file that
// could not be read marks the job so its plan waits for a person.
func TestALaunchStampsTheStartRulesEveryDoorMustCarry(t *testing.T) {
	f := setup(t)
	if _, err := f.store.ChangeHouseRules(func(h *factory.HouseRules) error {
		h.AskFirst = []string{"touch billing/"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	house, _ := f.store.HouseRules()
	file, err := pipeline.Parse("start:\n  ask_first: [touch auth/]\n  never: [infra/]\n")
	if err != nil {
		t.Fatal(err)
	}
	file = pipeline.WithHouseRules(file, house)
	read := lease.NewDoors(f.desk).WithStartRead(func(context.Context, factory.Item) (pipeline.File, error) { return file, nil })
	stages := []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}}

	it := f.add(t, factory.Item{State: factory.StateNew, Stages: stages})
	if err := read.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if got.StartRules == nil || !reflect.DeepEqual(got.StartRules.AskFirst, []string{"touch auth/", "touch billing/"}) || !reflect.DeepEqual(got.StartRules.Never, []string{"infra/"}) || got.StartRules.Unread {
		t.Fatalf("the job started under %+v", got.StartRules)
	}

	stamped := f.add(t, factory.Item{State: factory.StateNew, Stages: stages, StartRules: &factory.StartRules{AskFirst: []string{"touch docs/"}}})
	if err := read.Launch(stamped.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Get(stamped.ID); !reflect.DeepEqual(got.StartRules.AskFirst, []string{"touch docs/"}) {
		t.Fatalf("a door's own stamp was replaced: %+v", got.StartRules)
	}

	unread := lease.NewDoors(f.desk).WithStartRead(func(context.Context, factory.Item) (pipeline.File, error) {
		return pipeline.Defaults(), errors.New("factory.yaml: github is not connected")
	})
	broken := f.add(t, factory.Item{State: factory.StateNew, Stages: stages})
	if err := unread.Launch(broken.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = f.store.Get(broken.ID)
	if got.StartRules == nil || !got.StartRules.Unread || !reflect.DeepEqual(got.StartRules.AskFirst, []string{"touch billing/"}) {
		t.Fatalf("a start with the repository unread carries %+v", got.StartRules)
	}
	if hold := pipeline.PlanHold(got.StartRules, []string{"docs/readme.md"}); hold != factory.StartRulesUnreadHold {
		t.Fatalf("its plan holds %q", hold)
	}
}
