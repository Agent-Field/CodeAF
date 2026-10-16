package workgraph

import (
	"container/list"
	"context"
	"sync"
)

// Gate is a weighted, first-in-first-out admission: capacity units of running
// weight (bytes, in both its users), waiters parked on a channel each and no
// goroutine of the gate's own. A weight above capacity is clamped to it, so a
// Acquire honours its context, and a waiter that leaves hands back anything it
// was granted as it left. A weight ABOVE capacity is refused with [TooLarge]:
// there is no exemption for the heaviest job, because the capacity is the
// promise about memory and a job that does not fit it is the one the promise is
// about.
type Gate struct {
	mu         sync.Mutex
	capacity   int64
	maxWaiters int
	used       int64
	queue      list.List
}

type gateWaiter struct {
	need  int64
	ready chan struct{}
}

// NewGate is a gate of the given capacity.
func NewGate(capacity int64) *Gate { return &Gate{capacity: max(capacity, 1)} }

// Limit bounds how many may wait at once; a caller that would be one more is
// refused with [ErrBusy] and costs the gate nothing. Zero is unbounded.
func (g *Gate) Limit(waiters int) *Gate {
	g.mu.Lock()
	g.maxWaiters = waiters
	g.mu.Unlock()
	return g
}

// Acquire waits for weight units of capacity and returns what it took, which
// the caller hands back to [Gate.Release].
func (g *Gate) Acquire(ctx context.Context, weight int64) (int64, error) {
	weight = max(weight, 1)
	if weight > g.capacity {
		return 0, NewTooLarge("working set", weight, g.capacity)
	}
	g.mu.Lock()
	if g.queue.Len() == 0 && g.used+weight <= g.capacity {
		g.used += weight
		g.mu.Unlock()
		return weight, nil
	}
	if g.maxWaiters > 0 && g.queue.Len() >= g.maxWaiters {
		g.mu.Unlock()
		return 0, ErrBusy
	}
	w := &gateWaiter{need: weight, ready: make(chan struct{})}
	el := g.queue.PushBack(w)
	g.mu.Unlock()
	select {
	case <-w.ready:
		return weight, nil
	case <-ctx.Done():
		g.mu.Lock()
		select {
		case <-w.ready: // granted as we left: hand it back.
			g.used -= weight
			g.admit()
		default:
			g.queue.Remove(el)
			g.admit()
		}
		g.mu.Unlock()
		return 0, ctx.Err()
	}
}

// Release returns what Acquire took.
func (g *Gate) Release(weight int64) {
	g.mu.Lock()
	g.used -= weight
	g.admit()
	g.mu.Unlock()
}

// admit grants the head of the queue while it fits. Called with g.mu held.
func (g *Gate) admit() {
	for el := g.queue.Front(); el != nil; el = g.queue.Front() {
		w := el.Value.(*gateWaiter)
		if g.used+w.need > g.capacity {
			return
		}
		g.used += w.need
		g.queue.Remove(el)
		close(w.ready)
	}
}
