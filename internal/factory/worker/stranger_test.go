package worker_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestWorkerRefusesQueuedStrangerBeforeNextRound(t *testing.T) {
	for _, begun := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "resumed"}[begun], func(t *testing.T) {
			var calls atomic.Int32
			g := newRigWith(t, nil, func(o *run.Options) {
				o.Exec = map[factory.StageKind]run.Executor{factory.StageCheck: run.ExecutorFunc(func(context.Context, run.Job) (factory.StageResult, error) {
					calls.Add(1)
					return factory.StageResult{Done: true}, nil
				})}
			})
			phase := factory.Phase{Name: "test", Kind: factory.StageCheck, State: factory.PhasePending}
			if begun {
				phase.State, phase.Round = factory.PhaseRunning, 1
			}
			saved, err := g.st.Create(factory.Item{Title: "theirs", Repo: "api", Kind: factory.KindIssue, Tier: factory.TierStranger, State: factory.StateQueued,
				Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, On: true}}, Stream: &factory.Stream{Phases: []factory.Phase{phase}}})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				it, err := g.st.Get(saved.ID)
				if err != nil {
					t.Fatal(err)
				}
				if it.Failure != nil && strings.Contains(it.Failure.Detail, "without write access") && factory.RunEnded(it) {
					if calls.Load() != 0 || it.Stream == nil || it.Stream.Ended.IsZero() || it.Stream.Spent != 0 {
						t.Fatalf("calls=%d state=%s failure=%+v", calls.Load(), it.State, it.Failure)
					}
					wantAttempts := 0
					if begun {
						wantAttempts = 1
					}
					if it.Failure.Attempts != wantAttempts || it.Stream.Phases[0].Round != wantAttempts {
						t.Fatalf("refusal reported an attempt: failure=%+v phase=%+v", it.Failure, it.Stream.Phases[0])
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("no refusal: calls=%d state=%s failure=%+v", calls.Load(), it.State, it.Failure)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
