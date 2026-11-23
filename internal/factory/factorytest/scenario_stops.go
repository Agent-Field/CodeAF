package factorytest

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// THE MAIN AGENT'S TURN ON A STOP, SCRIPTED. A stop the main agent may decide
// is handed to it first (factory's handoff.go), and the service gives it one
// turn through serve's Config.Stopped. A scenario scripts that turn with
// [Scenario.StopAgent]: the script reads the stop and presses the service's
// manager_decide door ([Scenario.Decide]), as the agent's tool does, or
// returns without pressing anything, which hands the stop to the person.
//
// With no turn scripted the scenario's factory asks nobody, as an app whose
// runner is off asks nobody (serve.ErrNotAsked): every stop goes straight on
// to the person, which is how the scenarios written before handoffs read.

// StopAgent scripts the main agent's one turn on each stop handed to it. turn
// answers what the agent said, which is its reason when it settles nothing.
func (s *Scenario) StopAgent(turn func(it factory.Item, wait factory.Wait) (said string, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopAgent = turn
	s.asking.Store(turn != nil)
}

// asksMainAgent says the scenario has a main agent's turn scripted, which is
// what asks a stop's main agent on its factory.
func (s *Scenario) asksMainAgent(factory.Item) bool { return s.asking.Load() }

// stopped is serve's Config.Stopped on the scenario's factory. A turn the
// service gives up on (its deadline, the service closing) is left to end by
// itself, as a conversation that is slow to stop is.
func (s *Scenario) stopped(ctx context.Context, it factory.Item, wait factory.Wait) (string, error) {
	s.mu.Lock()
	turn := s.stopAgent
	s.mu.Unlock()
	if turn == nil {
		return "", serve.ErrNotAsked
	}
	// THE JOB IS NOT AT REST WHILE THE SERVICE WAITS ON THIS TURN
	// ([Scenario.Settle]). A decision moves the job in one write and says so
	// on its log and its record in later ones, and the script's own line
	// after the press runs later still. A scenario that took the job as
	// settled at the first of those read a turn that had not ended: its press
	// not yet answered, its log line not yet written.
	s.deciding.Add(1)
	defer s.deciding.Add(-1)
	type answer struct {
		said string
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		said, err := turn(it, wait)
		done <- answer{said, err}
	}()
	select {
	case got := <-done:
		return got.said, got.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Decide presses the main agent's door with one decision on the stop the job
// is on, and answers what the door said.
func (s *Scenario) Decide(decision, step, words string) Pressed {
	body := map[string]any{"decision": decision}
	if step != "" {
		body["step"] = step
	}
	if words != "" {
		body["words"] = words
	}
	return s.Post(serve.ManagerDecideVerb, body)
}

// stopHanded has the service look at its jobs when a write hands a stop to
// the main agent, as the app's host does from its own observer: the write
// came from the runner's report or the poller, and no request follows it.
func (s *Scenario) stopHanded(before, after factory.Item) {
	if _, held := factory.HandedOver(after); !held {
		return
	}
	if _, was := factory.HandedOver(before); was {
		return
	}
	s.mu.Lock()
	handler := s.handler
	s.mu.Unlock()
	if handler != nil {
		handler.Wake()
	}
}

// stillDeciding is a main agent that holds its turn until the service
// closes: the main agent of a copy of the factory ([Scenario.copyFactory]).
func stillDeciding(ctx context.Context, _ factory.Item, _ factory.Wait) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
