// Package directorytest is the conformance suite every directory.Client
// implementation must pass, plus the fake clock it runs on.
package directorytest

import (
	"sync"
	"time"
)

// Clock is the directory's time as a suite may observe and wait on it. The
// fake clock moves when a case waits; a live relay's clock moves by itself and
// waiting is real.
type Clock interface {
	Now() time.Time
	// Wait returns once the directory's clock has moved on by d.
	Wait(d time.Duration)
}

// FakeClock is a clock that moves only when a test says so, and keeps the
// timers its After hands out: they run when the clock moves past them.
type FakeClock struct {
	mu     sync.Mutex
	t      time.Time
	timers []*fakeTimer
}

// fakeTimer is one pending After; it runs once, when the clock reaches due.
type fakeTimer struct {
	due  time.Time
	fn   func()
	done bool
}

// NewFakeClock starts at a fixed instant so runs are repeatable.
func NewFakeClock() *FakeClock { return &FakeClock{t: time.UnixMilli(1_700_000_000_000)} }

// Now is the value handed to the directory as its clock.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward by d and then runs every timer it passed, the
// earliest first. A timer may set another; one that falls due inside d runs too.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
	for fn := c.popDue(); fn != nil; fn = c.popDue() {
		fn()
	}
}

// After is the directory's timers on this clock: fn runs once the clock has
// moved d, and the stop it answers reports whether it beat the run.
func (c *FakeClock) After(d time.Duration, fn func()) (stop func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{due: c.t.Add(d), fn: fn}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.done
		t.done = true
		return was
	}
}

// popDue answers the earliest timer that is due and marks it run, or nil.
func (c *FakeClock) popDue() func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	var first *fakeTimer
	for _, t := range c.timers {
		if !t.done && !t.due.After(c.t) && (first == nil || t.due.Before(first.due)) {
			first = t
		}
	}
	if first == nil {
		return nil
	}
	first.done = true
	return first.fn
}

// Wait moves the clock forward by d, so it is Advance under the Clock name.
func (c *FakeClock) Wait(d time.Duration) { c.Advance(d) }
