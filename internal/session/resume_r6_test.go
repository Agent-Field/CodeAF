package session

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/executor"
)

func TestR6_OpeningAChatThatOwesNoTurnNeverAsksAgain(t *testing.T) {
	var checks atomic.Int32
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
		cfg.Seat = executor.Gated(cfg.Seat, func() error {
			checks.Add(1)
			return &executor.WorkRefusal{Line: "chat held elsewhere"}
		})
	})
	events, resumed := a.ResumeStoppedTurn(t.Context())
	if events != nil || resumed || checks.Load() != 0 {
		t.Fatalf("opening with no owed turn returned stream=%v resumed=%v work checks=%d", events != nil, resumed, checks.Load())
	}
}

func TestR6_OwedTurnRefusedKeepsTheTurnAndSaysItOnce(t *testing.T) {
	journal := takenOverMidThought(t, func(a *Agent) { a.InterruptFor(StopByTakeover) })
	var allowed atomic.Bool
	var calls atomic.Int32
	refusal := &executor.WorkRefusal{Line: "chat held elsewhere"}
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			calls.Add(1)
			return textResponse("the kept question is answered"), nil
		},
	}}, func(cfg *Config) {
		cfg.SessionFile = journal
		cfg.Seat = executor.Gated(cfg.Seat, func() error {
			if allowed.Load() {
				return nil
			}
			return refusal
		})
	})
	a.SettleWrites()
	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	events, resumed := a.ResumeStoppedTurn(t.Context())
	if resumed || events == nil {
		t.Fatalf("refused turn returned stream=%v resumed=%v", events != nil, resumed)
	}
	var refusals int
	for _, event := range collect(t, events) {
		if event.Kind == EventError && errors.Is(event.Err, refusal) {
			refusals++
		}
	}
	if refusals != 1 || calls.Load() != 0 {
		t.Fatalf("refusal events=%d model calls=%d", refusals, calls.Load())
	}
	after, err := os.ReadFile(journal)
	if err != nil || string(after) != string(before) {
		t.Fatalf("resume refusal changed the journal: %v", err)
	}
	allowed.Store(true)
	events, resumed = a.ResumeStoppedTurn(t.Context())
	if !resumed || events == nil {
		t.Fatal("refusal lost the owed turn")
	}
	collect(t, events)
	if events, resumed = a.ResumeStoppedTurn(t.Context()); resumed || events != nil || calls.Load() != 1 {
		t.Fatalf("owed turn did not run exactly once: resumed=%v calls=%d", resumed, calls.Load())
	}
}
