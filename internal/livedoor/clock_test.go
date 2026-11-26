package livedoor

import (
	"testing"
	"time"
)

// TestFakeClockFiresTimersInDeadlineOrderAndStopPreventsFiring verifies contract
// (clock): FakeClock fires due timers in deadline order on Advance and Stop
// prevents a timer from firing.
func TestFakeClockFiresTimersInDeadlineOrderAndStopPreventsFiring(t *testing.T) {
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	fc := NewFakeClock(start)

	t30 := fc.NewTimer(30 * time.Millisecond)
	t10 := fc.NewTimer(10 * time.Millisecond)
	t20Stopped := fc.NewTimer(20 * time.Millisecond)
	t25 := fc.NewTimer(25 * time.Millisecond)

	if got := fc.BlockedTimers(); got != 4 {
		t.Fatalf("BlockedTimers() = %d, want 4", got)
	}
	t20Stopped.Stop()
	if got := fc.BlockedTimers(); got != 3 {
		t.Fatalf("BlockedTimers() after Stop = %d, want 3", got)
	}

	// THE ORDER IS PROVED BY STEPPING, NOT BY GOROUTINES RACING TO RECORD IT.
	// Each Advance releases only the timers that are due, so a timer that fires
	// early shows up as a value on a channel that should still be empty, with no
	// scheduler in the way.
	fired := func(tm Timer) (time.Time, bool) {
		select {
		case at := <-tm.C():
			return at, true
		default:
			return time.Time{}, false
		}
	}
	fc.Advance(10 * time.Millisecond)
	if at, ok := fired(t10); !ok || !at.Equal(start.Add(10*time.Millisecond)) {
		t.Fatalf("after 10ms the 10ms timer = (%v, %v), want it fired at its own deadline", at, ok)
	}
	if _, ok := fired(t25); ok {
		t.Fatal("the 25ms timer fired at 10ms")
	}
	if _, ok := fired(t30); ok {
		t.Fatal("the 30ms timer fired at 10ms")
	}
	fc.Advance(15 * time.Millisecond)
	if at, ok := fired(t25); !ok || !at.Equal(start.Add(25*time.Millisecond)) {
		t.Fatalf("after 25ms the 25ms timer = (%v, %v), want it fired at its own deadline", at, ok)
	}
	if _, ok := fired(t30); ok {
		t.Fatal("the 30ms timer fired at 25ms")
	}
	fc.Advance(15 * time.Millisecond)
	if at, ok := fired(t30); !ok || !at.Equal(start.Add(30*time.Millisecond)) {
		t.Fatalf("after 40ms the 30ms timer = (%v, %v), want it fired at its own deadline", at, ok)
	}
	if _, ok := fired(t20Stopped); ok {
		t.Fatal("a stopped timer fired on Advance")
	}

	if !fc.Now().Equal(start.Add(40 * time.Millisecond)) {
		t.Fatalf("Now() = %v, want %v", fc.Now(), start.Add(40*time.Millisecond))
	}
}
