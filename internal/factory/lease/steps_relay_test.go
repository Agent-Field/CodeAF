package lease_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func phaseNames(it factory.Item) string {
	var out []string
	for _, ph := range it.Stream.Phases {
		out = append(out, ph.Name)
	}
	return strings.Join(out, " ")
}

// THE SEAM'S EDIT GOES THROUGH THE LEASE'S OWN, which lays the run's phases
// not yet begun again: a step removed from a queued job leaves its run, and a
// step moved runs in its new place.
func TestTheSeamsEditLaysARemovedOrMovedStepOutOfAQueuedRun(t *testing.T) {
	f := setup(t)
	doors := lease.NewDoors(f.desk)
	it := f.add(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	seam := factory.LocalSeam(f.store, time.Now(), factory.WithRunner(doors))
	got, _, err := seam.Edit(context.Background(), it.ID, factory.RunEdit{By: factory.ByYou, Remove: []string{"review"}})
	if err != nil || phaseNames(got) != "plan approve write test proof" {
		t.Fatalf("after the removal the run is %q (%v)", phaseNames(got), err)
	}
	got, _, err = seam.Edit(context.Background(), it.ID, factory.RunEdit{By: factory.ByYou, Move: []factory.Moved{{Stage: "test", After: "approve"}}})
	if err != nil || phaseNames(got) != "plan approve test write proof" {
		t.Fatalf("after the move the run is %q (%v)", phaseNames(got), err)
	}
	if saved, _ := f.store.Get(it.ID); phaseNames(saved) != "plan approve test write proof" {
		t.Fatalf("the store holds %q", phaseNames(saved))
	}
}

// A CHANGE TO A HELD JOB'S STEPS BY ANOTHER DOOR (the factory_item card's)
// withdraws the worker running it from its own copy, as a step edit does, and
// lays the phases not yet begun again; a change that leaves the steps alone
// leaves the worker be. A finished job's steps are refused.
func TestChangeStepsWithdrawsTheWorkerOnlyWhenTheStepsChange(t *testing.T) {
	f := setup(t)
	doors := lease.NewDoors(f.desk)
	it := f.add(t, factory.Item{State: factory.StateNew, Kind: factory.KindIssue, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(lease.Worker{ID: "w1", Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := doors.ChangeSteps(it.ID, func(it *factory.Item) error { it.Cap = 9; return nil })
	if err != nil || got.Cap != 9 {
		t.Fatalf("a budget change = %+v (%v)", got.Cap, err)
	}
	if _, err := f.desk.Drain(held.ID, "w1"); err != nil {
		t.Fatalf("a change to no step withdrew the worker: %v", err)
	}
	got, err = doors.ChangeSteps(it.ID, func(it *factory.Item) error {
		next, _, err := factory.Edit(*it, factory.RunEdit{By: factory.ByYou, Remove: []string{"review"}}, factory.DefaultRecipe(), factory.EditBeforeRun)
		*it = next
		return err
	})
	if err != nil || phaseNames(got) != "plan approve write test proof" {
		t.Fatalf("after the removal the run is %q (%v)", phaseNames(got), err)
	}
	if _, err := f.desk.Drain(held.ID, "w1"); err == nil {
		t.Fatal("the worker still holds the job whose steps changed")
	}

	done := f.add(t, factory.Item{State: factory.StateShipped, DoneHow: "merged", Kind: factory.KindIssue, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if _, err := doors.ChangeSteps(done.ID, func(it *factory.Item) error { it.Stages = it.Stages[1:]; return nil }); err == nil {
		t.Fatal("a finished job's steps changed")
	}
	if saved, _ := f.store.Get(done.ID); len(saved.Stages) != len(factory.DefaultRecipe().For(factory.KindIssue)) {
		t.Fatalf("a refused change wrote %v", saved.Stages)
	}
}
