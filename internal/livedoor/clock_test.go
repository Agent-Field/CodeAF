package livedoor

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
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

	var (
		mu    sync.Mutex
		order []int
		done  = make(chan struct{}, 3)
	)
	watch := func(id int, tm Timer) {
		guard.Go("livedoor.clock.test", func() {
			<-tm.C()
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			done <- struct{}{}
		})
	}
	watch(30, t30)
	watch(10, t10)
	watch(25, t25)

	fc.WaitForTimers(3)
	fc.Advance(40 * time.Millisecond)

	for i := 0; i < 3; i++ {
		<-done
	}

	select {
	case <-t20Stopped.C():
		t.Fatal("a stopped timer fired on Advance")
	default:
	}

	mu.Lock()
	defer mu.Unlock()
	want := []int{10, 25, 30}
	if len(order) != len(want) {
		t.Fatalf("fired %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("fired order = %v, want %v", order, want)
		}
	}
	if !fc.Now().Equal(start.Add(40 * time.Millisecond)) {
		t.Fatalf("Now() = %v, want %v", fc.Now(), start.Add(40*time.Millisecond))
	}
}
