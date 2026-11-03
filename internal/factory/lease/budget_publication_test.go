package lease_test

import (
 "testing"
 "errors"
 "github.com/Agent-Field/codeaf/internal/factory/run"
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

func TestBudgetStillRejectsPaidStepDuringApprovedWrite(t *testing.T) {
 for _, merging := range []bool{false, true} {
  t.Run(map[bool]string{false: "publication", true: "merge"}[merging], func(t *testing.T) {
   f := setup(t)
   it := factory.Item{Assignee: "sam", Publishing: !merging, Cap: 1,
    Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}},
    Stream: &factory.Stream{Spent: .5, Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhasePending}}}}
   if merging { it.Merging = &factory.MergeRun{Commit: "abc123", State: factory.MergeQueued} }
   it = f.add(t, it)
   held, err := f.desk.Pull(worker("writer", "sam"))
   if err != nil { t.Fatal(err) }
   report := factory.RunReport{Cost: .5, Stages: []factory.StageReport{{Name: "write", State: factory.PhaseRunning, Round: 1}}}
   err = f.desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error { return run.ApplyReport(it, report, f.now) })
   if !errors.Is(err, lease.ErrWithdrawn) { t.Fatalf("paid step at cap beside approved write: %v, want withdrawn", err) }
  })
 }
}
