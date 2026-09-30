package session

// A PASS THAT FOUND NOTHING SAYS NOTHING.
//
// [EventCompacting] is sent only once a pass has really edited the transcript,
// and [EventCompacted] follows it at once. A pass that stubbed nothing and folded
// nothing therefore opens no row, and so has no row to settle: it returns
// [ErrNothingToCompact] to its caller and leaves the hub alone. An automatic
// attempt is silent that way, and `/compact` says "nothing to compact" from the
// error rather than from an event.
//
// [Event.Unchanged] is still read by a surface, because a peer built before this
// announced every pass and settled a refused one with that field set
// (internal/tui3's compact_refused_test.go). No pass in this build sends it.

import (
	"context"
	"errors"
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

func TestAPassThatCompactedNothingSendsNoEvent(t *testing.T) {
	agent, _ := newTestAgent(t, &refusingCompleter{t: t}, func(config *Config) {
		config.ContextWindow = 2_000_000
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, textMessage("user", "one short question"))
	agent.mu.Unlock()

	hub := newEventHub()
	if _, err := agent.compact(context.Background(), hub); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("compact = %v, want ErrNothingToCompact", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, event := range hub.backlog {
		if event.Kind == EventCompacting || event.Kind == EventCompacted {
			t.Fatalf("a pass that stubbed nothing and folded nothing announced itself: %+v", event)
		}
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
