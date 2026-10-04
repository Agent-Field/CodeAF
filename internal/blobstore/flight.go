package blobstore

import (
	"context"
	"sync"
)

// Has never answers no while a put for the same identity is still being
// received. A client that gave up on a slow put asks Has to learn whether the
// put landed anyway, and a "no" given from the middle of that put makes it send
// every byte again. Any relay must keep this rule, including one that keeps an
// identity's state in a single object: a put counts from the moment its request
// arrives, and a Has that meets one waits for it, or fails, but never says no.
//
// flight counts the puts of one scope that have arrived and not yet finished.
type flight struct {
	mu      sync.Mutex
	n       int
	changed chan struct{} // closed, and replaced, each time n changes
}

func newFlight() *flight { return &flight{changed: make(chan struct{})} }

// enter records one put and answers the function that records its end.
func (f *flight) enter() (leave func()) {
	f.move(1)
	var once sync.Once
	return func() { once.Do(func() { f.move(-1) }) }
}

func (f *flight) move(delta int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n += delta
	close(f.changed)
	f.changed = make(chan struct{})
}

// busy answers a channel that closes on the next change, or nil when idle.
func (f *flight) busy() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.n == 0 {
		return nil
	}
	return f.changed
}

// waitIdle returns once every flight is idle at the same moment, or with the
// context's error. The bound comes from the caller so a put that never ends
// cannot hold a Has forever.
func waitIdle(ctx context.Context, flights ...*flight) error {
	for {
		wait := firstBusy(flights)
		if wait == nil {
			return nil
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func firstBusy(flights []*flight) <-chan struct{} {
	for _, f := range flights {
		if ch := f.busy(); ch != nil {
			return ch
		}
	}
	return nil
}
