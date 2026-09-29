// Package directorytest is the conformance suite every directory.Client
// implementation must pass, plus the fake clock it runs on.
package directorytest

import (
	"sync"
	"time"
)

// FakeClock is a clock that moves only when a test says so.
type FakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// NewFakeClock starts at a fixed instant so runs are repeatable.
func NewFakeClock() *FakeClock { return &FakeClock{t: time.UnixMilli(1_700_000_000_000)} }

// Now is the value handed to the directory as its clock.
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
