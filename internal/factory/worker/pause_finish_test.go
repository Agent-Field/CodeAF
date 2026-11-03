package worker_test

import (
 "sync/atomic"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestAPauseIsReportedWhenTheCancelledDriveFinishesFirst(t *testing.T) {
 var delay atomic.Bool
 g := newRigWith(t, nil, func(o *run.Options) {
  o.Clock = func() time.Time {
   if delay.CompareAndSwap(true, false) {
    // Let the cancelled drive finish before the pause's store write.
    time.Sleep(200 * time.Millisecond)
   }
   return time.Now()
  }
 })
 id := g.add(t, check("sleep 30"), factory.Stage{Name: "again", Kind: factory.StageCheck, Ask: "true", On: true})
 if err := g.floor.Launch(id); err != nil { t.Fatal(err) }
 for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
  it, _ := g.st.Get(id)
  if logHas(it, "check: sleep 30") > 0 { break }
  if time.Now().After(deadline) { t.Fatal("the worker never started the check") }
 }
 delay.Store(true)
 if err := g.floor.Hold(id, true); err != nil { t.Fatal(err) }
 for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
  it, _ := g.st.Get(id)
  if !g.run.Running(id) && logHas(it, "paused") == 1 {
   if logHas(it, "check: true") != 0 { t.Fatal("the second step started while held") }
   break
  }
  if time.Now().After(deadline) {
   local, _ := g.mirror.Get(id)
   t.Fatalf("pause lost as drive finished: factory=%+v worker=%+v running=%t reports=%q", it.Stream, local.Stream, g.run.Running(id), g.seen.all())
  }
 }
 if err := g.floor.Hold(id, false); err != nil { t.Fatal(err) }
 // The resumed check should finish promptly without waiting thirty seconds.
 if err := g.floor.Stop(id); err != nil { t.Fatal(err) }
}
