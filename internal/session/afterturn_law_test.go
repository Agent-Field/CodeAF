package session

import (
	"context"
	"testing"
	"time"
)

// A READING THAT OUTLIVES ITS TURN MUST NOT OUTLIVE THE SESSION.
func TestAfterTurnRefusesOnceTheSessionIsClosing(t *testing.T) {
	after := newAfterTurn()
	ctx, done, ok := after.begin()
	if !ok || ctx == nil {
		t.Fatal("a fresh lifetime refused work")
	}
	landed := make(chan struct{})
	go func() { <-ctx.Done(); close(landed) }()
	settled := make(chan struct{})
	go func() { after.settle(time.Second); close(settled) }()
	select {
	case <-landed:
	case <-time.After(2 * time.Second):
		t.Fatal("settle did not cancel the work in flight")
	}
	done()
	select {
	case <-settled:
	case <-time.After(2 * time.Second):
		t.Fatal("settle did not return once the work unwound")
	}
	if _, _, ok := after.begin(); ok {
		t.Fatal("a closed lifetime started new work")
	}
	// A second settle is a no-op rather than a second cancel or a panic.
	after.settle(time.Millisecond)
}

// AND THE GRACE BOUNDS IT: work that ignores its cancellation cannot hold a quit.
func TestAfterTurnDoesNotWaitForeverOnWorkThatIgnoresIt(t *testing.T) {
	after := newAfterTurn()
	_, done, ok := after.begin()
	if !ok {
		t.Fatal("a fresh lifetime refused work")
	}
	began := time.Now()
	after.settle(50 * time.Millisecond)
	if waited := time.Since(began); waited > time.Second {
		t.Fatalf("settle waited %s on work that never unwound, want about its grace", waited)
	}
	done()
}

// AND A NIL LIFETIME ANSWERS, because every door here is reached on a session
// that may not have built one.
func TestANilAfterTurnAnswersWithoutPanicking(t *testing.T) {
	var after *afterTurn
	if _, _, ok := after.begin(); ok {
		t.Fatal("a nil lifetime said it had started work")
	}
	after.settle(time.Millisecond)
	_ = context.Background()
}
