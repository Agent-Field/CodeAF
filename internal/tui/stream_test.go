package tui

import "testing"

// TestStreamEventMatchingSessionAppliesNormally is the control: an event
// carrying the model's own session (or the zero value every pre-key test in
// this package still constructs) must apply exactly as it did before the key
// existed. This is the byte-identical half of the guarantee.
func TestStreamEventMatchingSessionAppliesNormally(t *testing.T) {
	matching := New(&fakeBackend{}, "room-a")
	matching.applyStreamEvent(StreamEvent{Kind: StreamStarted, Session: "room-a"})
	matching.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"hi"}`, Session: "room-a"})
	if matching.streamMode != streamReal || matching.streamTarget != "hi" {
		t.Fatalf("a same-session event was not applied: mode=%v target=%q", matching.streamMode, matching.streamTarget)
	}

	unkeyed := New(&fakeBackend{}, "room-a")
	unkeyed.applyStreamEvent(StreamEvent{Kind: StreamStarted})
	unkeyed.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"hi"}`})
	if unkeyed.streamMode != streamReal || unkeyed.streamTarget != "hi" {
		t.Fatalf("a zero-value-session event was not applied: mode=%v target=%q", unkeyed.streamMode, unkeyed.streamTarget)
	}
}

// TestStreamEventOtherSessionIsDropped is the defensive half: today only one
// room is ever listening, so this can't happen in practice, but the old TUI
// path must not paint another room's deltas into this one if it ever does.
// The event is silently dropped rather than crashing or partially applying.
func TestStreamEventOtherSessionIsDropped(t *testing.T) {
	model := New(&fakeBackend{}, "room-a")
	model.applyStreamEvent(StreamEvent{Kind: StreamStarted, Session: "room-b"})
	if model.streamMode != streamNone {
		t.Fatalf("a StreamStarted for a different room was applied: mode=%v", model.streamMode)
	}

	// Prime a real stream for this model's own room, then confirm an
	// interleaved other-room delta is dropped without disturbing it.
	model.applyStreamEvent(StreamEvent{Kind: StreamStarted, Session: "room-a"})
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: `{"reply":"mine`, Session: "room-a"})
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: ` only"}`, Session: "room-b"})
	if model.streamTarget != "mine" {
		t.Fatalf("an other-room delta leaked into this room's stream: target=%q", model.streamTarget)
	}
	model.applyStreamEvent(StreamEvent{Kind: StreamDelta, Delta: ` only"}`, Session: "room-a"})
	if model.streamTarget != "mine only" {
		t.Fatalf("this room's own delta after the dropped one was not applied: target=%q", model.streamTarget)
	}
}
