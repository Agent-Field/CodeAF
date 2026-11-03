package run

import (
	"context"
	"errors"
 "fmt"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestStopAfterStepReaderErrorUsesStoredChoice(t *testing.T) {
	for _, armed := range []bool{false, true} {
		name := "continues"
		if armed {
			name = "holds"
		}
		t.Run(name, func(t *testing.T) {
			g := newRig(t, map[factory.StageKind]Executor{
				factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
					return done("step completed"), nil
				}),
			}, nil)
			id := g.add("reader outage", chat("plan"), chat("write"))
			if err := g.st.Update(id, func(it *factory.Item) error {
				it.StopAfterStep = armed
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var reads atomic.Int32
			g.r.SetStopAfterStepReader(id, func(context.Context) (bool, error) {
				if reads.Add(1) == 2 {
					return false, errors.New("temporary choice read failure")
				}
				return armed, nil
			})
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			it := g.wait(id, "landed or waiting", func(it factory.Item) bool {
				return it.State == factory.StateLanded || it.State == factory.StateNeedsYou || it.Failure != nil
			})
			if armed {
				if it.State != factory.StateNeedsYou || it.QKind != factory.QKindSpendStopped || !it.StopAfterStep {
					t.Fatalf("stored hold lost after reader error: state=%s kind=%s armed=%v", it.State, it.QKind, it.StopAfterStep)
				}
				if it.Stream.Phases[0].State != factory.PhaseDone || it.Stream.Phases[1].Round != 0 {
					t.Fatalf("hold crossed step boundary: %+v", it.Stream.Phases)
				}
				return
			}
			if it.State != factory.StateLanded {
				t.Fatalf("reader error failed healthy job: state=%s", it.State)
			}
			if it.Stream.Phases[1].State != factory.PhaseDone {
				t.Fatalf("next step did not complete: %+v", it.Stream.Phases[1])
			}
			if reads.Load() < 3 {
				t.Fatal("later boundary did not retry the choice read")
			}
		})
	}
}

func TestAuthorityOutageDoesNotAdmitNextPaidStep(t *testing.T) {
 for _, transient := range []bool{true, false} {
  t.Run(fmt.Sprint(transient), func(t *testing.T) {
   var steps, reads atomic.Int32
   g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) { steps.Add(1); return done("paid"), nil })}, nil)
   id := g.add("authority outage", chat("plan"), chat("write"))
   g.r.SetStopAfterStepReader(id, func(context.Context) (bool, error) {
    n := reads.Add(1)
    if n == 1 { return false, nil }
    if transient && n > 2 { return true, nil }
    return false, errors.New("503 authority unavailable")
   })
   if err := g.r.Launch(id); err != nil { t.Fatal(err) }
   it := g.wait(id, "authority hold", func(it factory.Item) bool { return it.State == factory.StateLanded || it.State == factory.StateNeedsYou || it.Stream != nil && it.Stream.Paused })
   if steps.Load() != 1 || it.Failure != nil { t.Fatalf("authority outage admitted paid steps=%d failure=%+v", steps.Load(), it.Failure) }
   if !transient && !logHas(it, "could not confirm Stop") { t.Fatal("hold did not explain authority outage") }
  })
 }
}
