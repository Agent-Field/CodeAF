package lease_test

import (
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestHeartbeatRepeatsWantedHoldAndResumeSupersedesIt(t *testing.T) {
 f := setup(t)
 it, held := f.running(t)
 doors := lease.NewDoors(f.desk)
 if err := doors.Hold(it.ID, true); err != nil { t.Fatal(err) }
 for n := 0; n < 3; n++ {
  signals, err := f.desk.DrainLive(held.ID, held.Worker)
  if err != nil || len(signals) != 1 || signals[0].Verb != lease.SignalHold || !signals[0].Yes {
   t.Fatalf("heartbeat %d lost wanted hold: %+v, %v", n, signals, err)
  }
 }
 if err := doors.Hold(it.ID, false); err != nil { t.Fatal(err) }
 for n := 0; n < 3; n++ {
  signals, err := f.desk.DrainLive(held.ID, held.Worker)
  if err != nil || len(signals) != 0 { t.Fatalf("resumed heartbeat replayed hold: %+v, %v", signals, err) }
 }
 // Resume must supersede even a hold that was never collected.
 if err := doors.Hold(it.ID, true); err != nil { t.Fatal(err) }
 if err := doors.Hold(it.ID, false); err != nil { t.Fatal(err) }
 signals, err := f.desk.DrainLive(held.ID, held.Worker)
 if err != nil || len(signals) != 0 { t.Fatalf("resume kept an older hold: %+v, %v", signals, err) }
}
