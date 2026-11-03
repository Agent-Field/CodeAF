package lease_test

import (
 "testing"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestBudgetAllowsApprovedMergePull(t *testing.T) {
 f := setup(t)
 it := f.add(t, factory.Item{State: factory.StateShipped, Assignee: "sam", Cap: 1, Stream: &factory.Stream{Spent: 1}})
 if err := lease.NewDoors(f.desk).Merge(it.ID, factory.MergeRun{Commit: "abc123"}); err != nil { t.Fatal(err) }
 held, err := f.desk.Pull(worker("merger", "sam"))
 if err != nil { t.Fatalf("approved merge at cap refused: %v", err) }
 if held.Job != it.ID { t.Fatalf("merge job=%d, want %d", held.Job, it.ID) }
}

func TestBudgetKeepsApprovedPublicationLease(t *testing.T) {
 f := setup(t)
 it := f.add(t, factory.Item{Assignee: "sam", Publishing: true, Cap: 1, Stream: &factory.Stream{Spent: .5}})
 held, err := f.desk.Pull(worker("publisher", "sam"))
 if err != nil { t.Fatal(err) }
 err = f.desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error { it.Stream.Spent = 1; return nil })
 if err != nil { t.Fatalf("approved publication progress revoked lease: %v", err) }
 if err := f.desk.CheckPublish(held.ID, held.Worker); err != nil { t.Fatal(err) }
 if err := lease.NewDoors(f.desk).Stop(it.ID); err != nil { t.Fatal(err) }
 if err := f.desk.CheckPublish(held.ID, held.Worker); err == nil { t.Fatal("Stop did not stop publication") }
}
