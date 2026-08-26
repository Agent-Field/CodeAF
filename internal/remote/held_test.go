package remote

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestASettledConnectQuestionLeavesTheWaitingRoom(t *testing.T) {
	held := newHeldSet()
	held.raise(EventWire{Event: session.Event{Kind: session.EventConnectAsk, ConnectID: "connect-1"}}, 7, true)
	if got := len(held.waiting()); got != 1 {
		t.Fatalf("waiting questions = %d, want 1", got)
	}
	held.settleConnect(nil)
	if got := len(held.waiting()); got != 0 {
		t.Fatalf("waiting questions after settle = %d, want 0", got)
	}
}

func TestALiveConnectQuestionStaysInTheWaitingRoom(t *testing.T) {
	held := newHeldSet()
	held.raise(EventWire{Event: session.Event{Kind: session.EventConnectAsk, ConnectID: "connect-1"}}, 7, true)
	held.settleConnect([]string{"connect-1"})
	if got := len(held.waiting()); got != 1 {
		t.Fatalf("waiting questions = %d, want 1", got)
	}
}
