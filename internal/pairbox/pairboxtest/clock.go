package pairboxtest

import (
	"sync"
	"time"
)

// Clock is the mailbox's time as a suite may wait on it. A Memory rig has a
// fake clock whose Wait moves it; a live relay's clock moves by itself, so a
// rig for one sleeps until the relay says the time has passed.
type Clock interface {
	// Wait returns once the mailbox's clock has moved on by d.
	Wait(d time.Duration)
}

// FakeClock is a clock that moves only when a test says so. Hand its Now to
// pairbox.NewMemory and its Wait to the rig, and expiry costs no real time.
type FakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// NewFakeClock starts at a fixed instant so runs are repeatable.
func NewFakeClock() *FakeClock { return &FakeClock{t: time.UnixMilli(1_700_000_000_000)} }

// Now is the value handed to the mailbox as its clock.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Wait moves the clock forward by d, so it is Advance under the Clock name.
func (c *FakeClock) Wait(d time.Duration) { c.Advance(d) }
