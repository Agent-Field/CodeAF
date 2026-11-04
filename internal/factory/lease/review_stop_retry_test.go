package lease_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestReviewStopAllowsImmediateRunAgain(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam", State: factory.StateLanded, Proof: []factory.Claim{{Text: "works", OK: true}}})
	history, _ := record.Open(f.store, record.Options{})
	if _, err := history.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "sam"}); err != nil {
		t.Fatal(err)
	}
	doors := lease.NewDoors(f.desk)
	if _, err := doors.SignOff(it.ID, false); err != nil {
		t.Fatal(err)
	}
	held, err := f.desk.Pull(worker("publisher", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.desk.Drain(held.ID, held.Worker); err != nil {
		t.Fatal(err)
	}
	if err := doors.Stop(it.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Update(it.ID, func(it *factory.Item) error { factory.ClearPersonStop(it); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := doors.Launch(it.ID); err != nil {
		t.Fatalf("Run again after Stop blocked: %v", err)
	}
	replacement, err := f.desk.Pull(worker("replacement", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.desk.CheckPublish(held.ID, held.Worker); err == nil {
		t.Fatal("stopped publisher authorized after Run again")
	}
	if _, err := f.desk.Renew(held.ID, held.Worker); err != nil {
		t.Fatalf("receipt heartbeat lost: %v", err)
	}
	signals, err := f.desk.DrainLive(held.ID, held.Worker)
	if err != nil || len(signals) != 1 || signals[0].Verb != lease.SignalStop {
		t.Fatalf("old publisher did not receive Stop: %+v %v", signals, err)
	}
	before, _ := f.store.Get(it.ID)
	report := factory.RunReport{PR: &factory.PullRequest{Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen}, Publishing: &factory.Publishing{State: "opened", Attempt: 1}}
	if err := f.desk.Apply(held.ID, held.Worker, false, func(it *factory.Item) error { return run.ApplyReport(it, report, f.now) }); err != nil {
		t.Fatalf("late PR receipt lost: %v", err)
	}
	saved, _ := f.store.Get(it.ID)
	if saved.PullRequest == nil || saved.State != before.State || saved.Publishing != before.Publishing {
		t.Fatalf("late PR took over new run: before=%s after=%+v", before.State, saved)
	}
	if err := f.desk.Apply(held.ID, held.Worker, true, func(it *factory.Item) error { it.State = factory.StateShipped; return nil }); err != nil {
		t.Fatal(err)
	}
	saved, _ = f.store.Get(it.ID)
	if saved.State != before.State {
		t.Fatal("old publisher completion took over new run")
	}
	if _, err := f.desk.Renew(replacement.ID, replacement.Worker); err != nil {
		t.Fatalf("new lease lost: %v", err)
	}
}
