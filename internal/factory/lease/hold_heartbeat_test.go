package lease_test

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestHeartbeatRepeatsWantedHoldAndResumeSupersedesIt(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	doors := lease.NewDoors(f.desk)
	if err := doors.Hold(it.ID, true); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		signals, err := f.desk.DrainLive(held.ID, held.Worker)
		if err != nil || len(signals) != 1 || signals[0].Verb != lease.SignalHold || !signals[0].Yes {
			t.Fatalf("heartbeat %d lost wanted hold: %+v, %v", n, signals, err)
		}
	}
	if err := doors.Hold(it.ID, false); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		signals, err := f.desk.DrainLive(held.ID, held.Worker)
		if err != nil || len(signals) != 0 {
			t.Fatalf("resumed heartbeat replayed hold: %+v, %v", signals, err)
		}
	}
	// Resume must supersede even a hold that was never collected.
	if err := doors.Hold(it.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := doors.Hold(it.ID, false); err != nil {
		t.Fatal(err)
	}
	signals, err := f.desk.DrainLive(held.ID, held.Worker)
	if err != nil || len(signals) != 0 {
		t.Fatalf("resume kept an older hold: %+v, %v", signals, err)
	}
}

func TestStoppedJobNeverReceivesRepeatedHold(t *testing.T) {
	for _, live := range []bool{false, true} {
		name := "drain"
		if live {
			name = "heartbeat"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			it, held := f.running(t)
			doors := lease.NewDoors(f.desk)
			if err := doors.Hold(it.ID, true); err != nil {
				t.Fatal(err)
			}
			// Preserve the old lease and pause to model a Stop record observed before revocation.
			if err := f.store.Update(it.ID, func(cur *factory.Item) error { factory.MarkPersonStopped(cur); return nil }); err != nil {
				t.Fatal(err)
			}
			if err := doors.Hold(it.ID, true); !errors.Is(err, factory.ErrPersonStopped) {
				t.Fatalf("repeated hold accepted Stop: %v", err)
			}
			for n := 0; n < 3; n++ {
				drain := f.desk.Drain
				if live {
					drain = func(id, worker string) ([]lease.Signal, error) { return f.desk.DrainLive(id, worker) }
				}
				signals, err := drain(held.ID, held.Worker)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range signals {
					if s.Verb == lease.SignalHold {
						t.Fatalf("drain %d delivered hold after Stop: %+v", n, signals)
					}
				}
			}
		})
	}
}
