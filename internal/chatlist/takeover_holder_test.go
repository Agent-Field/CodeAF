package chatlist

import (
	"testing"
	"time"
)

// Contract F2 keeps the relay's holder visible even when that computer is offline.
func TestTakeoverCardNamesTheOfflineHolderAndSavedTurn(t *testing.T) {
	r := Row{Device: "other computer", Status: Off, DurableAgo: 37 * time.Second}
	if got, want := TakeoverLine(r), "other computer offline; last saved turn 37s ago"; got != want {
		t.Fatalf("card=%q; want %q", got, want)
	}
}
