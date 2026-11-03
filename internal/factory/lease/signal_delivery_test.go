package lease_test

import (
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestOldSignalWorkerStillCollectsOnlyOnce(t *testing.T) {
 f:=setup(t)
 it,held:=f.running(t)
 if ok,err:=f.desk.SignalIfHeld(it.ID,lease.Signal{Verb:lease.SignalSteer,Words:"once"});!ok || err!=nil{t.Fatal(ok,err)}
 first,err:=f.desk.DrainLive(held.ID,held.Worker)
 if err!=nil || len(first)!=1 || first[0].Words!="once" {t.Fatalf("legacy delivery: %+v %v",first,err)}
 second,err:=f.open(t).DrainLive(held.ID,held.Worker)
 if err!=nil || len(second)!=0 {t.Fatalf("old worker received a repeat: %+v %v",second,err)}
}
