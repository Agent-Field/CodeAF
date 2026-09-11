package remote

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// slowSender is an engine whose Submit takes as long as a real one's preamble:
// the client rebind, the standing orders, the system prompt, the journal write
// and the naming errand all happen before [session.Agent.Submit] returns, and
// on a cold disk that is not free. Held here on purpose, at two seconds, so the
// question is unambiguous.
type slowSender struct {
	*fakeAgent
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	typed   chan struct{}
	typedOn sync.Once
}

func (s *slowSender) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.fakeAgent.Submit(ctx, text)
}

func (s *slowSender) Typing() { s.typedOn.Do(func() { close(s.typed) }) }

// TestASendDoesNotHoldTheSocketShut is the measured defect of this wave, on the
// real protocol.
//
// EVERY FRAME ON THE CONNECTION USED TO WAIT FOR THE SEND. [MethodSubmit] was
// classed ordered, ordered stayed on the reader, and the reader does not read
// the next line until the call it is running returns — so a person who pressed
// Enter and then Esc was queued behind their own message, and so was every
// getter their surface asks from its update loop.
func TestASendDoesNotHoldTheSocketShut(t *testing.T) {
	far := &slowSender{
		fakeAgent: &fakeAgent{model: "m"},
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
		typed:     make(chan struct{}),
	}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	released := make(chan struct{})
	var releaseOnce sync.Once
	letGo := func() { releaseOnce.Do(func() { close(far.release); close(released) }) }
	t.Cleanup(func() { letGo(); _ = loop.Close() })

	sent := make(chan error, 1)
	go func() {
		_, err := loop.Client.Agent().Submit(context.Background(), "hello")
		sent <- err
	}()
	select {
	case <-far.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the send never reached the engine")
	}
	// The preamble is held for two seconds from here, exactly as the brief for
	// this lane stated it, and let go early only if the assertions below fail.
	timer := time.AfterFunc(2*time.Second, letGo)
	t.Cleanup(func() { timer.Stop() })

	began := time.Now()
	if _, err := loop.Client.Ping(); err != nil {
		t.Fatalf("a beat on the same socket was refused while a send was in flight: %v", err)
	}
	took := time.Since(began)
	select {
	case <-released:
		t.Fatal("the beat only came back after the send let go: the reader is still holding the socket")
	default:
	}
	// TEN MILLISECONDS IS THE BAR AND THE MEASUREMENT IS FAR UNDER IT — a
	// loopback round trip is tens of microseconds — so the generous figure here
	// is about a shared box under load rather than about the mechanism.
	if took > 250*time.Millisecond {
		t.Fatalf("a beat behind an in-flight send took %v", took)
	}

	letGo()
	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("the send failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the send never finished")
	}
}

// TestTwoSendsKeepTheOrderTheSurfaceSentThem is the other half: a turn left the
// reader and must not have left the queue. The lane is one goroutine draining
// one channel, so two sends run in the order their frames arrived — which is
// the guarantee classOrdered was keeping and the only one a turn owes.
func TestTwoSendsKeepTheOrderTheSurfaceSentThem(t *testing.T) {
	far := &orderedSender{fakeAgent: &fakeAgent{model: "m"}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	agent := loop.Client.Agent()
	for _, word := range []string{"first", "second", "third"} {
		if _, err := agent.Submit(context.Background(), word); err != nil {
			t.Fatalf("%s: %v", word, err)
		}
	}
	far.mu.Lock()
	defer far.mu.Unlock()
	want := []string{"first", "second", "third"}
	if len(far.heard) != len(want) {
		t.Fatalf("the engine heard %v", far.heard)
	}
	for i, word := range want {
		if far.heard[i] != word {
			t.Fatalf("the engine heard %v, not %v", far.heard, want)
		}
	}
}

type orderedSender struct {
	*fakeAgent
	mu    sync.Mutex
	heard []string
}

func (o *orderedSender) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	o.mu.Lock()
	o.heard = append(o.heard, text)
	o.mu.Unlock()
	return o.fakeAgent.Submit(ctx, text)
}

// TestAKeystrokeReachesTheEngineOnTheDefaultRoad is the typing door end to end:
// the surface says somebody is writing, the frame crosses, and the engine's own
// door is called — which is what buys the probe, and with it a connection that
// is already open when the turn goes out (typing.go).
func TestAKeystrokeReachesTheEngineOnTheDefaultRoad(t *testing.T) {
	far := &slowSender{
		fakeAgent: &fakeAgent{model: "m"},
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
		typed:     make(chan struct{}),
	}
	close(far.release)
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	agent := loop.Client.Agent()
	// THE SURFACE CALLS THIS ON EVERY CHARACTER (internal/tui3's app.key), so
	// the door is driven the same way here.
	for range 40 {
		agent.Typing()
	}
	select {
	case <-far.typed:
	case <-time.After(2 * time.Second):
		t.Fatal("the engine was never told somebody was writing")
	}
}

// TestTypingIsHushedToOneFrameAWireIsWorth refuses a frame per character. The
// engine's prober is the budget that matters — one pair every twenty seconds
// per model — and this is only about what goes onto the pipe.
func TestTypingIsHushedToOneFrameAWireIsWorth(t *testing.T) {
	var beat typingBeat
	at := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	if !beat.due(at) {
		t.Fatal("THE FIRST KEYSTROKE MUST GO AT ONCE: the whole value of the signal is how early it arrives")
	}
	for step := time.Millisecond; step < typingHush; step *= 4 {
		if beat.due(at.Add(step)) {
			t.Fatalf("a second frame went out %v after the first", step)
		}
	}
	if !beat.due(at.Add(typingHush)) {
		t.Fatal("the hush never lifts")
	}
}

// TestATypingFrameIsNeverWaitedFor states the one property that makes the door
// safe to call from a surface's update loop: it is a hint with nobody holding
// the other end, so it registers no call and cannot be made to wait out a
// deadline ([Client.notify]).
func TestATypingFrameIsNeverWaitedFor(t *testing.T) {
	far := &fakeAgent{model: "m"}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	loop.Client.Agent().Typing()
	loop.Client.mu.Lock()
	waiting := len(loop.Client.calls)
	loop.Client.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("a hint left %d calls waiting for an answer", waiting)
	}
}
