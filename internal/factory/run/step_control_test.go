package run

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestStepRerunExecutesTailAndKeepsSpend(t *testing.T) {
	for _, target := range []string{"plan", "write"} {
		t.Run(target, func(t *testing.T) {
			calls := make(chan Job, 4)
			exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
				calls <- job
				return factory.StageResult{Done: true, Output: job.Stage.Name, Spent: .5}, nil
			})
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: exec}, nil)
			id := g.add("rerun", chat("plan"), chat("write"))
			clock := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
			err := g.st.Update(id, func(it *factory.Item) error {
				it.State = factory.StateRunning
				it.Stream = &factory.Stream{Cur: 1, Spent: 1.25, Started: clock, Phases: []factory.Phase{
					{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone, Result: &factory.StageResult{Done: true, Output: "previous plan", Notes: []string{"keep this note"}}},
					{Name: "write", Kind: factory.StageChat, State: factory.PhaseFailed},
				}}
				if err := StepControl(it, target, true, "", clock); err != nil {
					return err
				}
				// A pulled worker revives any begun stream through Hold(false).
				it.State, it.Stream.Paused = factory.StateRunning, true
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.r.Hold(id, false); err != nil {
				t.Fatal(err)
			}
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for {
				select {
				case event := <-g.events:
					if event.Kind != EventLanded {
						continue
					}
					it, err := g.st.Get(id)
					if err != nil {
						t.Fatal(err)
					}
					wantCalls, wantSpend := 1, 1.75
					if target == "plan" {
						wantCalls, wantSpend = 2, 2.25
					}
					if len(calls) != wantCalls || it.Stream.Spent != wantSpend || it.State != factory.StateLanded {
						t.Fatalf("rerun %s: calls=%d spend=%f state=%s", target, len(calls), it.Stream.Spent, it.State)
					}
					first := <-calls
					if first.Stage.Name != target || (target == "write" && (len(first.Prior) == 0)) {
						t.Fatalf("wrong starting step: %+v", first)
					}
					return
				case <-deadline.C:
					t.Fatal("rerun did not land")
				}
			}
		})
	}
}
