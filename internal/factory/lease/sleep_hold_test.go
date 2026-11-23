package lease_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestAsleepLeaseRemainsALiveHoldUntilWakeDeadline(t *testing.T) {
	f := setup(t)
	_, held := f.running(t)
	if err := f.desk.Sleep(held.Machine, held.Handle, true); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(30 * time.Minute)
	if live, err := f.desk.HasLiveHold(held.Job); err != nil || !live {
		t.Fatalf("sleep lost hold: %v %v", live, err)
	}
	if err := f.desk.Sleep(held.Machine, held.Handle, false); err != nil {
		t.Fatal(err)
	}
	if live, err := f.desk.HasLiveHold(held.Job); err != nil || !live {
		t.Fatalf("wake did not refresh hold: %v %v", live, err)
	}
	f.now = f.now.Add(time.Hour)
	if live, err := f.desk.HasLiveHold(held.Job); err != nil || live {
		t.Fatalf("awake expired hold stayed live: %v %v", live, err)
	}
}

// A computer that sleeps holding a job keeps it through a night, not forever:
// past the asleep ceiling the hold no longer counts and the job is free.
func TestAsleepLeaseEndsAtItsCeiling(t *testing.T) {
	f := setup(t)
	_, held := f.running(t)
	if err := f.desk.Sleep(held.Machine, held.Handle, true); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(lease.AsleepCeiling - time.Minute)
	if live, err := f.desk.HasLiveHold(held.Job); err != nil || !live {
		t.Fatalf("a night's sleep lost the hold: %v %v", live, err)
	}
	f.now = f.now.Add(2 * time.Minute)
	if live, err := f.desk.HasLiveHold(held.Job); err != nil || live {
		t.Fatalf("a computer that never woke still holds the job: %v %v", live, err)
	}
}
