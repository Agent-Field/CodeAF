package lease_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestBudgetAllowsApprovedMergePull(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{State: factory.StateShipped, Assignee: "sam", Cap: 1, Stream: &factory.Stream{Spent: 1}})
	if err := lease.NewDoors(f.desk).Merge(it.ID, factory.MergeRun{Commit: "abc123"}); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(worker("merger", "sam"))
	if err != nil {
		t.Fatalf("approved merge at cap refused: %v", err)
	}
	if held.Job != it.ID {
		t.Fatalf("merge job=%d, want %d", held.Job, it.ID)
	}
}

func TestBudgetKeepsApprovedPublicationLease(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam", Publishing: true, Cap: 1, Stream: &factory.Stream{Spent: .5}})
	held, err := f.desk.Pull(worker("publisher", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	err = f.desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error { it.Stream.Spent = 1; return nil })
	if err != nil {
		t.Fatalf("approved publication progress revoked lease: %v", err)
	}
	if err := f.desk.CheckPublish(held.ID, held.Worker); err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(f.desk).Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.desk.CheckPublish(held.ID, held.Worker); err == nil {
		t.Fatal("Stop did not stop publication")
	}
}

func TestBudgetStillRejectsPaidStepDuringApprovedWrite(t *testing.T) {
	for _, merging := range []bool{false, true} {
		t.Run(map[bool]string{false: "publication", true: "merge"}[merging], func(t *testing.T) {
			f := setup(t)
			it := factory.Item{Assignee: "sam", Publishing: !merging, Cap: 1,
				Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}},
				Stream: &factory.Stream{Spent: .5, Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhasePending}}}}
			if merging {
				it.Merging = &factory.MergeRun{Commit: "abc123", State: factory.MergeQueued}
			}
			it = f.add(t, it)
			held, err := f.desk.Pull(worker("writer", "sam"))
			if err != nil {
				t.Fatal(err)
			}
			report := factory.RunReport{Cost: .5, Stages: []factory.StageReport{{Name: "write", State: factory.PhaseRunning, Round: 1}}}
			err = f.desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error { return run.ApplyReport(it, report, f.now) })
			if !errors.Is(err, lease.ErrWithdrawn) {
				t.Fatalf("paid step at cap beside approved write: %v, want withdrawn", err)
			}
		})
	}
}

func TestBudgetAllowsPublishAnywayAtLifetimeCap(t *testing.T) {
	f := setup(t)
	it := factory.Item{Assignee: "sam", State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts, Cap: 1, SpentBefore: .5, HeadCommit: "abc123",
		Change: &factory.Change{Files: []factory.ChangedFile{{Path: "docs.md"}}},
		Stages: []factory.Stage{{Name: "inspect", Kind: factory.StageCheck, When: "touches auth"}, {Name: "post", Kind: factory.StagePost, Ask: "pr"}, {Name: "model", Kind: factory.StageChat}},
		Stream: &factory.Stream{Spent: .5, Cur: 1, Phases: []factory.Phase{
			{Name: "inspect", Kind: factory.StageCheck, State: factory.PhaseSkipped, SkipReason: "no auth files"},
			{Name: "post", Kind: factory.StagePost, State: factory.PhaseWaiting, Result: &factory.StageResult{Claims: []factory.Claim{{Medium: "policy", Evidence: factory.EveryCheckPolicy}}}},
			{Name: "model", Kind: factory.StageChat, State: factory.PhasePending},
		}}}
	if err := factory.DecidePublishAnyway(&it, "sam", f.now); err != nil {
		t.Fatal(err)
	}
	it = f.add(t, it)
	if err := lease.NewDoors(f.desk).Answer(it.ID, true, factory.ChoiceTryOnceMore); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(worker("publisher", "sam"))
	if err != nil {
		t.Fatalf("Publish anyway at lifetime cap refused: %v", err)
	}
	if err := f.desk.CheckPublish(held.ID, held.Worker); err != nil {
		t.Fatal(err)
	}
	if err := f.desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error {
		return run.ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: "post", State: factory.PhaseRunning, Round: 1}}}, f.now)
	}); err != nil {
		t.Fatalf("approved post at lifetime cap refused: %v", err)
	}
	if err := f.desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error {
		return run.ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: "model", State: factory.PhaseRunning, Round: 1}}}, f.now)
	}); !errors.Is(err, lease.ErrWithdrawn) {
		t.Fatalf("paid step beside Publish anyway: %v, want withdrawn", err)
	}
}
