package session

// A PASS THAT FOUND NOTHING SAYS SO ON THE EVENT ITSELF.
//
// [EventCompacted] is sent on both paths by promise: a surface opens a row on
// [EventCompacting] and has to be able to settle it whether the pass edited
// anything or not. That left one value carrying two meanings, and the failing
// one was the silent one, so a reader could not tell a transcript that had been
// replaced from one that had not been touched. [Event.Unchanged] is the
// disjoint range, and its zero value is the meaning that was always safe.
//
// THIS PINS THE SESSION HALF ONLY. internal/tui3 pins what a surface does with
// the field, and that test passes on a hand-built event whether this half exists
// or not, which is exactly why both are written down.

import (
	"context"
	"strings"
	"testing"
)

// lastCompacted is the pass announcement the hub carries, or a failure saying
// none was sent at all, which is the other half of the same promise.
func lastCompacted(t *testing.T, hub *eventHub) Event {
	t.Helper()
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for i := len(hub.backlog) - 1; i >= 0; i-- {
		if hub.backlog[i].Kind == EventCompacted {
			return hub.backlog[i]
		}
	}
	t.Fatal("no EventCompacted was sent, which is the promise EventCompacting is declared with")
	return Event{}
}

func TestAPassThatCompactedNothingSaysTheTranscriptDidNotMove(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2_000_000
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, textMessage("user", "one short question"))
	agent.mu.Unlock()

	hub := newEventHub()
	if _, err := agent.compact(context.Background(), hub); err != ErrNothingToCompact {
		t.Fatalf("compact = %v, want ErrNothingToCompact", err)
	}
	event := lastCompacted(t, hub)
	if !event.Unchanged {
		t.Fatalf("a pass that stubbed nothing and folded nothing announced itself as a pass that happened: %+v", event)
	}
	// AND THE ROW STILL SETTLES. The field separates the two meanings; it does
	// not withdraw the event, which a surface is waiting on either way.
	if strings.TrimSpace(event.Hint) == "" {
		t.Fatal("the refused pass settled the row with nothing to say")
	}
}

// AND A PASS THAT REALLY EDITED THE TRANSCRIPT SAYS NOTHING OF THE SORT, which
// is what keeps the test above from passing on a build that simply marked every
// pass unchanged.
func TestAPassThatEditedTheTranscriptIsNotAnnouncedAsUnchanged(t *testing.T) {
	heavy := strings.Repeat("package main // the whole of it, again and again.\n", 60)
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2_000_000
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, exchanges(6, map[int]string{1: heavy})...)
	agent.mu.Unlock()

	hub := newEventHub()
	changed, err := agent.compact(context.Background(), hub)
	if err != nil || !changed {
		t.Fatalf("compact = %v, %v, want a pass that edited the transcript", changed, err)
	}
	if event := lastCompacted(t, hub); event.Unchanged {
		t.Fatalf("a pass that rewrote the transcript announced itself as having changed nothing: %+v", event)
	}
}
