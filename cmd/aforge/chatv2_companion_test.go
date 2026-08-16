package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/tui2/chat"
)

// THE ONE PLACE THE TWO VOCABULARIES CAN BE CHECKED AGAINST EACH OTHER.
//
// internal/head mints the companion stream keys and internal/tui2/chat decides
// what to do with them, and neither imports the other — deliberately, for the
// reason engine.go states about the surface owning its own event type. That
// leaves exactly one package that sees both, and this is it.
//
// Without this pin, renaming a suffix on the head's side is a silent regression
// of precisely the shape this wave was built to fix: the deltas keep crossing
// the bridge, the surface stops recognising the key, and the answer goes back to
// appearing whole with nothing in the build to say so.
func TestCompanionStreamKeysMatchTheHead(t *testing.T) {
	const room = "chat-20260813-101500.000000"

	for _, one := range []struct {
		what    string
		key     string
		drawn   bool
		because string
	}{
		{
			what:    "the delivery answer",
			key:     head.AbsorbStreamSession(room),
			drawn:   true,
			because: "it posts an ordinary agent row into the room, so it streams there",
		},
		{
			what:    "the receipt wake",
			key:     head.ReceiptStreamSession(room),
			drawn:   true,
			because: "it posts an ordinary agent row into the room, so it streams there",
		},
		{
			what:    "the ephemeral aside",
			key:     head.AsideStreamSession(room),
			drawn:   false,
			because: "its answer journals as a collapsed stub, and folds never stream",
		},
	} {
		got, suffix, drawn := chat.StreamRoomOf(one.key)
		if got != room {
			t.Errorf("%s: the surface read %q out of key %q as the room, want %q — "+
				"a key it cannot attribute is a key it will drop",
				one.what, got, one.key, room)
		}
		if suffix == "" {
			t.Errorf("%s: the surface read key %q as the room's OWN turn — "+
				"a companion that is mistaken for the main turn interleaves with it",
				one.what, one.key)
		}
		if drawn != one.drawn {
			t.Errorf("%s: the surface draws it = %v, want %v — %s",
				one.what, drawn, one.drawn, one.because)
		}
	}
}

// The room's own turn is keyed by the bare session (internal/head/head.go stamps
// fold.message.SessionID), and it must keep reading as the room's own.
func TestTheRoomsOwnStreamKeyIsNotReadAsACompanion(t *testing.T) {
	const room = "chat-20260813-101500.000000"
	got, suffix, drawn := chat.StreamRoomOf(room)
	if got != room || suffix != "" || !drawn {
		t.Fatalf("chat.StreamRoomOf(%q) = (%q, %q, %v), want (%q, %q, true)",
			room, got, suffix, drawn, room, "")
	}
}
