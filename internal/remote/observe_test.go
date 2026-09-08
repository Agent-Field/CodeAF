package remote

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The far side provides independent readers, as session.Agent does. The client
// must preserve that ownership across the wire instead of consuming Submit's
// channel or asking the model to execute the turn again.
type observedAgent struct {
	*fakeAgent
	mu      sync.Mutex
	readers []chan session.Event
	stops   int
}

func (a *observedAgent) Attach() (<-chan session.Event, bool, func()) {
	_, events, stop := a.AttachReplay()
	return events, true, stop
}

func (a *observedAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	events := make(chan session.Event, 4)
	events <- session.Event{Kind: session.EventTextDelta, Text: "before"}
	a.readers = append(a.readers, events)
	var once sync.Once
	return a.Transcript(), events, func() { once.Do(func() { a.mu.Lock(); a.stops++; a.mu.Unlock() }) }
}

func observerEvent(t *testing.T, events <-chan session.Event, want string) {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok || ev.Text != want {
			t.Fatalf("event = %+v, open %v; want %q", ev, ok, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observer lost its live tail")
	}
}

func TestRemoteObserversKeepIndependentBacklogsAndStopOnlyTheirReader(t *testing.T) {
	far := &observedAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	first, err := agent.Submit(context.Background(), "one execution")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range first {
		}
	}()
	watch, running, leave := agent.Attach()
	if !running {
		t.Fatal("keeper cannot observe the remote agent")
	}
	_, replay, stop := agent.AttachReplay()
	defer stop()
	observerEvent(t, watch, "before")
	observerEvent(t, replay, "before")
	leave()
	leave()
	far.mu.Lock()
	far.readers[1] <- session.Event{Kind: session.EventTextDelta, Text: "after"}
	close(far.readers[1])
	far.mu.Unlock()
	observerEvent(t, replay, "after")
	select {
	case _, ok := <-replay:
		if ok {
			t.Fatal("extra replay event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completed observation stayed open")
	}
	// Round trips establish that cancellation has crossed without relying on a
	// sleep. No observer is allowed to interrupt the conversation itself.
	far.fakeAgent.mu.Lock()
	if len(far.sent) != 1 || far.interrupts != 0 {
		t.Errorf("observing submitted %v and interrupted %d times", far.sent, far.interrupts)
	}
	far.fakeAgent.mu.Unlock()
	deadline := time.After(3 * time.Second)
	for {
		far.mu.Lock()
		stops := far.stops
		far.mu.Unlock()
		if stops == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d observer stops arrived", stops)
		default:
		}
		_, _ = loop.Client.call(nil, MethodPing, nil)
	}
}

func TestRemoteObserverEndsWhenItsConnectionCloses(t *testing.T) {
	far := &observedAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	events, running, stop := loop.Client.Agent().Attach()
	defer stop()
	if !running {
		t.Fatal("no live reader")
	}
	observerEvent(t, events, "before")
	_ = loop.Close()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected event after disconnect")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observer stranded on a dead connection")
	}
}

func TestRemoteObserverOnAnIdleEngineReturnsTheTranscriptWithoutAStream(t *testing.T) {
	far := &fakeAgent{model: "m"}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	_, events, stop := loop.Client.Agent().AttachReplay()
	stop()
	stop()
	if events != nil {
		t.Fatal("an engine with no live turn returned a stream")
	}
}
