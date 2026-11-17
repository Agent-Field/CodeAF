package worker

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestDelayedManagerDecisionCannotAnswerTheNextQuestion(t *testing.T) {
	for _, mode := range []string{"current", "next question", "next round"} {
		t.Run(mode, func(t *testing.T) {
			mirror, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			it, err := mirror.Create(factory.Item{State: factory.StateNeedsYou, Question: "plan asks: Merge both?", QKind: "step", Asking: &factory.Asked{Stage: "plan", Question: "Merge both?", With: factory.AskedYou, At: time.Now()}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseWaiting, Round: 1}}}})
			if err != nil {
				t.Fatal(err)
			}
			questionID := factory.PendingQuestionID(it)
			if mode != "current" {
				_ = mirror.Update(it.ID, func(cur *factory.Item) error {
					if mode == "next question" {
						cur.Question = "Choose the public API"
						cur.Asking.Question = cur.Question
						cur.Asking.At = cur.Asking.At.Add(time.Second)
					} else {
						cur.Stream.Phases[0].Round++
					}
					return nil
				})
			}
			j := &job{id: it.ID, o: Options{Mirror: mirror, Runner: run.New(run.Options{Store: mirror})}}
			if !j.signals(context.Background(), []api.WorkerSignal{{Verb: lease.SignalAnswer, ID: questionID, Words: "Merge both"}}) {
				t.Fatal("signal stopped the worker")
			}
			got, _ := mirror.Get(it.ID)
			if mode == "current" {
				if got.Question != "" || len(got.Notes) != 1 {
					t.Fatal("matching answer did not reach its saved question")
				}
			} else if got.Question == "" || len(got.Notes) != 0 {
				t.Fatal("stale answer signal consumed a later decision")
			}
		})
	}
}
