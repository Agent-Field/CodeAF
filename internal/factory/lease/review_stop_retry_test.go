package lease_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
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
	if err := doors.Launch(it.ID); err != nil {
		t.Fatalf("Run again after Stop blocked: %v", err)
	}
}
