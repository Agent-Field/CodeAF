package remote

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// Hosted chats must receive the same response boundary as a local chat, or
// their answer would remain compact until the later whole-turn finish.
func TestAssistantConfirmationCrossesTheEventWire(t *testing.T) {
	payload, err := json.Marshal(WireEvent(session.Event{Kind: session.EventAssistantDone}))
	if err != nil {
		t.Fatal(err)
	}
	var wire EventWire
	if err = json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	if got := wire.Unwire(); got.Kind != session.EventAssistantDone {
		t.Fatalf("confirmation became %v across the wire", got.Kind)
	}
}
