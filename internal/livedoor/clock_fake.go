package livedoor

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// FakeClock is a deterministic clock driven by tests across packages.
// A test that waits for a background goroutine to park on a timer before
// calling Advance must use WaitForTimers (or inspect BlockedTimers) rather than
// calling runtime.Gosched or sleeping on real time.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	nextSeq uint64
	pending []*fakeTimer
	blocked atomic.Int32
}

type fakeTimer struct {
	clock   *FakeClock
	at      time.Time
	seq     uint64
	ch      chan time.Time
	stopped bool
}

// NewFakeClock creates a FakeClock positioned at start.
func NewFakeClock(start time.Time) *FakeClock {
	if start.IsZero() {
		start = time.Unix(1_700_000_000, 0).UTC()
	}
	return &FakeClock{now: start}
}

// Now returns the fake clock's current time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer registers a timer that fires when Advance moves the clock to or
// past Now().Add(d). A non-positive duration fires immediately.
func (c *FakeClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSeq++
	t := &fakeTimer{
		clock: c,
		at:    c.now.Add(d),
		seq:   c.nextSeq,
		ch:    make(chan time.Time, 1),
	}
	if d <= 0 {
		t.stopped = true
		t.ch <- c.now
		return t
	}
	c.pending = append(c.pending, t)
	c.blocked.Store(int32(len(c.pending)))
	return t
}

// BlockedTimers reports how many unfired, unstopped timers are currently
// waiting on the fake clock.
func (c *FakeClock) BlockedTimers() int {
	return int(c.blocked.Load())
}

// WaitForTimers spins on BlockedTimers with a bounded real-time loop of at
// most three seconds until at least n timers are pending (or zero when n <= 0),
// panicking with a clear diagnostic if the condition is not reached in time.
func (c *FakeClock) WaitForTimers(n int) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		cur := int(c.blocked.Load())
		if (n <= 0 && cur == 0) || (n > 0 && cur >= n) {
			return
		}
		if time.Now().After(deadline) {
			panic(fmt.Sprintf("livedoor.FakeClock: timed out waiting for %d blocked timer(s); have %d", n, cur))
		}
		runtime.Gosched()
	}
}

// Advance moves the clock forward by d and fires every timer whose deadline
// has passed in deadline order, yielding after each firing so goroutines
// blocked on timer.C() run before subsequent timers fire.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.now = target
	c.mu.Unlock()

	for {
		c.mu.Lock()
		bestIdx := -1
		var best *fakeTimer
		for i, t := range c.pending {
			if t.at.After(target) {
				continue
			}
			if best == nil || t.at.Before(best.at) || (t.at.Equal(best.at) && t.seq < best.seq) {
				best = t
				bestIdx = i
			}
		}
		if best == nil {
			c.mu.Unlock()
			break
		}
		c.pending = append(c.pending[:bestIdx], c.pending[bestIdx+1:]...)
		c.blocked.Store(int32(len(c.pending)))
		best.stopped = true
		best.ch <- best.at
		c.mu.Unlock()

		// Yield so a goroutine blocked on <-best.C() wakes and runs its handler
		// before the next timer in deadline order is fired.
		for i := 0; i < 64; i++ {
			runtime.Gosched()
			if len(best.ch) == 0 {
				break
			}
		}
		for i := 0; i < 16; i++ {
			runtime.Gosched()
		}
	}
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() {
	c := t.clock
	c.mu.Lock()
	if !t.stopped {
		t.stopped = true
		for i, p := range c.pending {
			if p == t {
				c.pending = append(c.pending[:i], c.pending[i+1:]...)
				break
			}
		}
		c.blocked.Store(int32(len(c.pending)))
	}
	select {
	case <-t.ch:
	default:
	}
	c.mu.Unlock()
}
