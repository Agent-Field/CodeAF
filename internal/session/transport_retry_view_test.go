package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A transport retry replaces the failed attempt on the live page just as it
// does in the journal. The first attempt really streams, then loses its wire;
// replaying the public events as the surface does must show only the answer
// which the session kept, without needing a later reopen to repair the page.
func TestATransportRetryReplacesTheVisiblePartialAnswer(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "the dead partial answer")
			return nil, errors.New("decode stream: read: connection reset by peer")
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "the completed answer")
			return textResponse("the completed answer"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collected := collect(t, mustSubmit(t, agent, "answer the question"))
	var visible strings.Builder
	for _, event := range collected {
		switch event.Kind {
		case EventTextDelta:
			visible.WriteString(event.Text)
		case EventRetrying:
			visible.Reset()
		case EventError:
			t.Fatalf("the retry did not recover: %v", event.Err)
		}
	}
	kept := messageText(lastMessage(agent))
	if kept != "the completed answer" {
		t.Fatalf("journal answer = %q", kept)
	}
	if visible.String() != kept {
		t.Fatalf("visible answer = %q, journal = %q; events: %v", visible.String(), kept, kinds(collected))
	}
	if got := countOfKind(collected, EventRetrying); got != 1 {
		t.Fatalf("retry announcements = %d, want exactly one", got)
	}
	if got := completer.requests(); got != 2 {
		t.Fatalf("requests = %d, want failed attempt and its replacement", got)
	}
}

// Stopping while a retry is waiting keeps the first partial answer. A discard
// announced before the wait would erase text which the stopped turn journals.
func TestStoppingATransportRetryWaitKeepsTheVisiblePartial(t *testing.T) {
	log := watchPhases(t)
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "the interrupted answer")
			return nil, errors.New("decode stream: read: connection reset by peer")
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	events := mustSubmit(t, agent, "answer the question")
	waitFor(t, "the transport retry wait", func() bool {
		for _, news := range log.all() {
			if news.Phase == provider.PhaseRetrying {
				return true
			}
		}
		return false
	})
	agent.Interrupt()
	collected := collect(t, events)
	if got := countOfKind(collected, EventRetrying); got != 0 {
		t.Fatalf("a stopped wait discarded the partial %d times", got)
	}
	if got := messageText(lastMessage(agent)); got != "the interrupted answer" {
		t.Fatalf("stopping the retry wait kept %q", got)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("stopping the retry wait still started %d requests", got)
	}
}
