package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// Contract 5: expiry while the pump is stuck must cover later stuck producers
// too, and must still close and flush the outbox.
func TestAppFinishSharesOneDeadline(t *testing.T) {
	for _, lane := range []string{"lanes", "asks", "titles"} {
		t.Run(lane, func(t *testing.T) {
			rec := newAppRecorder()
			pump, blocked := make(chan struct{}), make(chan struct{})
			released := false
			live := &appLive{
				conn: &appConn{Release: func() { released = true }},
				box:  appwire.NewOutbox(rec), pumpDone: pump,
			}
			switch lane {
			case "lanes":
				live.lanesDone = blocked
			case "asks":
				live.asksDone = blocked
			case "titles":
				live.titlesDone = blocked
			}
			// This also makes the pre-fix regression finish after its assertion.
			defer close(blocked)
			defer close(pump)
			expired := make(chan struct{})
			b := &appBackend{after: func(_ time.Duration, fn func()) func() {
				timer := time.AfterFunc(80*time.Millisecond, func() {
					fn()
					close(expired)
				})
				return func() { timer.Stop() }
			}}
			live.box.Delta("agent", 1, 0, appwire.DeltaText, "last words")
			done := make(chan struct{})
			deadline := time.After(120 * time.Millisecond)
			go func() { b.finish(live); close(done) }()
			select {
			case <-expired:
				t.Log("drain timer callback completed with every producer still open")
			case <-deadline:
				t.Fatal("release drain timer did not fire through the shortened timer hook")
			}
			select {
			case <-done:
			case <-deadline:
				t.Fatal("release waited beyond its one drain deadline")
			}
			if !released {
				t.Fatal("conversation was not released")
			}
			rec.mu.Lock()
			before := len(rec.events)
			rec.mu.Unlock()
			if before != 1 {
				t.Fatalf("outbox lost its final delta: %d events", before)
			}
			live.box.Event("after-close", nil)
			// Close waits for delivery; an already closed outbox ignores the event.
			live.box.Close()
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.events) != before {
				t.Fatal("finish returned without closing its outbox")
			}
		})
	}
}

// Contract 5: a clean release waits for every producer, flushes every event,
// and returns promptly instead of waiting out the unused grace period.
func TestAppFinishDrainsCleanProducersPromptly(t *testing.T) {
	rec := newAppRecorder()
	released := make(chan struct{})
	live := &appLive{
		conn:     &appConn{Release: func() { close(released) }},
		box:      appwire.NewOutbox(rec),
		pumpDone: make(chan struct{}), lanesDone: make(chan struct{}),
		asksDone: make(chan struct{}), titlesDone: make(chan struct{}),
	}
	b := &appBackend{after: func(_ time.Duration, fn func()) func() {
		timer := time.AfterFunc(time.Second, fn)
		return func() { timer.Stop() }
	}}
	done := make(chan struct{})
	go func() { b.finish(live); close(done) }()
	<-released
	for i, producer := range []chan struct{}{live.pumpDone, live.lanesDone, live.asksDone, live.titlesDone} {
		// Give finish time to return incorrectly while this producer is open.
		select {
		case <-done:
			t.Fatalf("finish returned before producer %d completed", i)
		case <-time.After(25 * time.Millisecond):
		}
		live.box.Event(appwire.EventAgentChanged, appwire.AgentChanged{Agent: appwire.Agent{ID: "final"}})
		close(producer)
	}
	select {
	case <-done:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("clean release waited out its grace period")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 4 {
		t.Fatalf("clean release lost events: %v", rec.events)
	}
}
