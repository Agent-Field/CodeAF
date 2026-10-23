package lease_test

import (
	"testing"
	"time"
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
