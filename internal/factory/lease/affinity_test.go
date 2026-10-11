package lease_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func machineWorker(id, machine string) lease.Worker {
	return lease.Worker{ID: id, Machine: machine, Handle: "sam", TakesUnassigned: map[string]bool{lease.EveryRepo: true}}
}

func TestAResumedJobWaitsForItsWorktreesMachine(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{Assignee: "sam"})
	held, err := f.desk.Pull(machineWorker("first-process", "laptop"))
	if err != nil || held.Machine != "laptop" {
		t.Fatalf("first lease = %+v, %v", held, err)
	}
	// No worker is online after this deterministic expiry. Reopening the
	// desk must still refuse the online machine that has none of the files.
	f.now = held.ExpiresAt
	desk := f.open(t)
	if _, err := desk.Pull(machineWorker("other-process", "desktop")); !errors.Is(err, lease.ErrNoJob) {
		t.Fatalf("the other machine took resumed work: %v", err)
	}
	waiting, _ := f.store.Get(it.ID)
	if waiting.State != factory.StateQueued || waiting.WorkState() != "waiting for laptop" {
		t.Fatalf("offline work = %+v, phrase %q", waiting, waiting.WorkState())
	}
	// Even much later, offline affinity never turns into permission to
	// start over on another disk; unrelated work remains available there.
	f.now = f.now.Add(10 * lease.DefaultTTL)
	other := f.add(t, factory.Item{Assignee: "sam"})
	if got, err := desk.Pull(machineWorker("desktop-slot", "desktop")); err != nil || got.Job != other.ID {
		t.Fatalf("unrelated job = %+v, %v", got, err)
	}
	resumed, err := desk.Pull(machineWorker("new-slot", "laptop"))
	if err != nil || resumed.Job != it.ID || resumed.Machine != held.Machine {
		t.Fatalf("same machine's new slot = %+v, %v", resumed, err)
	}
}

func TestPublishingKeepsAffinityAndApprovalAcrossDisconnection(t *testing.T) {
	for _, finishing := range []bool{false, true} {
		for _, expire := range []bool{false, true} {
			t.Run(testName(finishing, expire), func(t *testing.T) {
				f := setup(t)
				it := f.add(t, factory.Item{Assignee: "sam"})
				held, err := f.desk.Pull(machineWorker("runner", "laptop"))
				if err != nil {
					t.Fatal(err)
				}
				if err := f.desk.Apply(held.ID, held.Worker, !finishing, func(it *factory.Item) error {
					it.State = factory.StateLanded
					it.Proof = []factory.Claim{{Text: "build passes", OK: true}}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				history, _ := record.Open(f.store, record.Options{})
				event, err := history.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "sam"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := lease.NewDoors(f.desk).SignOff(it.ID, false); err != nil {
					t.Fatal(err)
				}
				if finishing {
					if signals, err := f.desk.DrainLive(held.ID, held.Worker); err != nil || len(signals) != 0 {
						t.Fatalf("finishing heartbeat consumed publishing: %+v, %v", signals, err)
					}
					// Approval arrives before the original worker releases its
					// landed run; losing that worker must still queue publishing.
					f.now = held.ExpiresAt
					if _, err := f.desk.Expire(); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := f.open(t).Pull(machineWorker("wrong-machine", "desktop")); !errors.Is(err, lease.ErrNoJob) {
					t.Fatalf("publish went to the other machine: %v", err)
				}
				waiting, _ := f.store.Get(it.ID)
				if !waiting.Publishing || waiting.WorkState() != "publishing · waiting for laptop" {
					t.Fatalf("pending publish says %q", waiting.WorkState())
				}
				publisher, err := f.open(t).Pull(machineWorker("publisher", "laptop"))
				if err != nil {
					t.Fatal(err)
				}
				want := "Factory-Approval: " + event.ID + " @sam"
				assertPublish := func(held lease.Lease) {
					t.Helper()
					signals, err := f.desk.Drain(held.ID, held.Worker)
					if err != nil || len(signals) != 1 || signals[0].Verb != lease.SignalPublish || signals[0].Words != want {
						t.Fatalf("approval = %+v, %v", signals, err)
					}
				}
				assertPublish(publisher)
				if expire {
					f.now = publisher.ExpiresAt
					if _, err := f.desk.Expire(); err != nil {
						t.Fatal(err)
					}
				} else if err := f.desk.Requeue(it.ID, publisher.Worker); err != nil {
					t.Fatal(err)
				}
				if _, err := f.open(t).Pull(machineWorker("still-wrong", "desktop")); !errors.Is(err, lease.ErrNoJob) {
					t.Fatalf("disconnected publish lost its machine: %v", err)
				}
				publisher, err = f.open(t).Pull(machineWorker("replacement", "laptop"))
				if err != nil {
					t.Fatal(err)
				}
				assertPublish(publisher)
				if err := f.desk.Apply(publisher.ID, publisher.Worker, true, func(it *factory.Item) error {
					it.State = factory.StateShipped
					it.PullRequest = &factory.PullRequest{Number: 7, URL: "https://example.com/pull/7"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				shipped, _ := f.store.Get(it.ID)
				if shipped.Publishing || shipped.WorkState() != "" {
					t.Fatalf("completed publish still waits: %+v", shipped)
				}
				if _, err := f.desk.Pull(machineWorker("after", "laptop")); !errors.Is(err, lease.ErrNoJob) {
					t.Fatalf("completed approval was offered again: %v", err)
				}
			})
		}
	}
}

func testName(finishing, expire bool) string {
	name := "released"
	if finishing {
		name = "finishing"
	}
	if expire {
		return name + "/expiry"
	}
	return name + "/requeue"
}
