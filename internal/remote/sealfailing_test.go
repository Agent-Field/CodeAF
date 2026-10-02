package remote

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// sealingAgent is an engine whose seat's last seal can fail, as the far
// machine's seal watch does.
type sealingAgent struct {
	*fakeAgent
	failing atomic.Bool
}

func (a *sealingAgent) SealFailing() bool { return a.failing.Load() }

func sealingLoop(t *testing.T, far *sealingAgent) *Loop {
	t.Helper()
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { loop.Close() })
	return loop
}

// THE SEAL WATCH LIVES IN THE ENGINE'S PROCESS, so the `not sealed` segment
// reads the fact the engine stated. A failure that began before the surface
// connected is in its welcome.
func TestASealFailingBeforeTheSurfaceConnectedIsKnownAtTheDoor(t *testing.T) {
	far := &sealingAgent{fakeAgent: &fakeAgent{model: "m"}}
	far.failing.Store(true)
	var agent interface{ SealFailing() bool } = sealingLoop(t, far).Client.Agent()
	if !agent.SealFailing() {
		t.Fatal("the welcome hid a seal that was already failing")
	}
}

// A seal that fails, and holds again, during a turn moves the segment with it
// and never asks the wire.
func TestTheFarSealFailingAndRecoveringMovesTheSegmentWithoutAskingTheWire(t *testing.T) {
	far := &sealingAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := sealingLoop(t, far)
	handle := loop.Client.Agent()
	if handle.SealFailing() {
		t.Fatal("a conversation whose seals hold drew a failure")
	}
	events, err := handle.Submit(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	stream := waitForStream(t, far.fakeAgent)
	for _, failing := range []bool{true, false} {
		far.failing.Store(failing)
		stream <- session.Event{Kind: session.EventToolEnd, ID: 1}
		<-events
		before := loop.CallsMade()
		if handle.SealFailing() != failing {
			t.Fatalf("segment = %v, want %v", handle.SealFailing(), failing)
		}
		if loop.CallsMade() != before {
			t.Fatal("reading the seal state crossed the wire")
		}
	}
	far.finish(stream)
}

// An engine that cannot say draws nothing: unknown is not a failure.
func TestAnEngineWithNoSealStateDrawsNoFailure(t *testing.T) {
	far := &fakeAgent{model: "m"}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if loop.Client.Agent().SealFailing() {
		t.Fatal("an engine that said nothing drew a failure")
	}
}
