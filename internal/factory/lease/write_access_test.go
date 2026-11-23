package lease_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestServiceWriteDoorsSavePermissionRefusal(t *testing.T) {
	for _, door := range []string{"publish", "merge"} {
		t.Run(door, func(t *testing.T) {
			f := setup(t)
			it := f.add(t, factory.Item{Tier: factory.TierStranger, State: factory.StateLanded})
			d := lease.NewDoors(f.desk)
			var err error
			if door == "publish" {
				_, err = d.SignOff(it.ID, false)
			} else {
				err = d.Merge(it.ID, factory.MergeRun{Commit: "recorded-head"})
			}
			var refusal run.WriteAccessRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("%s refusal=%v", door, err)
			}
			saved, err := f.store.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Failure == nil || saved.Failure.Detail != refusal.Error() || saved.Failure.Step != door {
				t.Fatalf("saved refusal=%+v", saved.Failure)
			}
			if saved.State != it.State || saved.Publishing || saved.Merging != nil {
				t.Fatalf("refused job changed run: %+v", saved)
			}
		})
	}
}

func TestDeskReplyRefusesWithoutStartingConversation(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Tier: factory.TierStranger, State: factory.StateNew})
	var refused run.WriteAccessRefusal
	if err := f.desk.Reply(it.ID, "sam", "Explain this job."); !errors.As(err, &refused) {
		t.Fatalf("reply refusal=%v", err)
	}
	saved, err := f.store.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Talk != "" || len(saved.Thread) != 0 || factory.JobSpent(saved) != 0 {
		t.Fatal("refused reply started work")
	}
}
