package remote

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Attention is a conversation fact; it must precede the event that wakes its
// hidden view, and reading it must never send a request back to the engine.
type attentionAgent struct {
	*fakeAgent
	waiting atomic.Bool
}

func (a *attentionAgent) NeedsPerson() bool { return a.waiting.Load() }

func TestHiddenConversationAttentionArrivesBeforeItsQuestionAndSettlement(t *testing.T) {
	far := &attentionAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	handle := loop.Client.Agent()
	events, err := handle.Submit(context.Background(), "permission")
	if err != nil {
		t.Fatal(err)
	}
	stream := waitForStream(t, far.fakeAgent)
	for _, step := range []struct {
		kind    session.EventKind
		waiting bool
	}{
		{session.EventConsentRequest, true},
		{session.EventToolEnd, false},
		{session.EventConnectAsk, true},
		{session.EventConnectDone, false},
		{session.EventTurnDone, false},
	} {
		far.waiting.Store(step.waiting)
		stream <- session.Event{Kind: step.kind, ID: 1}
		if ev := <-events; ev.Kind != step.kind {
			t.Fatalf("event %v, want %v", ev.Kind, step.kind)
		}
		before := loop.CallsMade()
		if handle.NeedsPerson() != step.waiting {
			t.Fatalf("attention after %v = %v", step.kind, handle.NeedsPerson())
		}
		if loop.CallsMade() != before {
			t.Fatal("reading attention crossed the wire")
		}
	}
	far.finish(stream)
}

// A keeper observes the turn through its own connection-local subscription.
// Its question may arrive before the ordinary Submit pump has forwarded it.
type attentionObservedAgent struct {
	*observedAgent
	waiting atomic.Bool
}

func (a *attentionObservedAgent) NeedsPerson() bool { return a.waiting.Load() }

func TestObserverPublishesAttentionBeforeWakingTheKeeper(t *testing.T) {
	far := &attentionObservedAgent{observedAgent: &observedAgent{fakeAgent: &fakeAgent{model: "m"}}}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	handle := loop.Client.Agent()
	events, running, stop := handle.Attach()
	defer stop()
	if !running {
		t.Fatal("observer missing")
	}
	observerEvent(t, events, "before")
	far.waiting.Store(true)
	far.mu.Lock()
	far.readers[0] <- session.Event{Kind: session.EventConsentRequest, ID: 7, Text: "question"}
	far.mu.Unlock()
	observerEvent(t, events, "question")
	if !handle.NeedsPerson() {
		t.Fatal("keeper woke before attention arrived")
	}
	far.waiting.Store(false)
	far.mu.Lock()
	far.readers[0] <- session.Event{Kind: session.EventTurnDone, Text: "stopped"}
	far.mu.Unlock()
	observerEvent(t, events, "stopped")
	if handle.NeedsPerson() {
		t.Fatal("stopped turn still needs a person")
	}
}
