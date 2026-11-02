package lane

import (
	"sync/atomic"
	"time"
)

// Routing and beat membership are published together. A beat joining during
// selection must either observe that selection or receive its retained wake;
// a separate subscriber snapshot could leave the newly joined beat asleep.
type routingState struct {
	last  int64
	beats []*routingBeat
}

type routingBeat struct {
	every   time.Duration
	catchup chan struct{}
	owed    atomic.Bool
}

// RecordRouting records selection without waiting for refresh or taking a lock.
// A first selection after idle wakes each beat; the beat checks freshness before
// fetching, and fresh selections only resume its existing cadence.
func (r *Registry) RecordRouting() {
	// Zero means no selection, even when fake time has not advanced at all.
	stamp := time.Since(r.routingStart).Nanoseconds() + 1
	current := r.routing.Load()
	for stamp > current.last {
		// An overdue tick must see the retained request before resumed activity.
		for _, beat := range current.beats {
			if current.last == 0 || time.Duration(stamp-current.last) >= beat.every {
				beat.owed.Store(true)
			}
		}
		next := &routingState{last: stamp, beats: current.beats}
		admitted := r.routing.CompareAndSwap(current, next)
		for _, beat := range current.beats {
			if current.last != 0 && time.Duration(stamp-current.last) < beat.every {
				continue
			}
			// Wake even if a tick consumed the request, or a competing publication won.
			select {
			case beat.catchup <- struct{}{}:
			default:
			}
		}
		if admitted {
			return
		}
		current = r.routing.Load()
	}
}

// lastRouting preserves the origin's monotonic clock when deriving deadlines.
func (r *Registry) lastRouting() time.Time {
	stamp := r.routing.Load().last
	if stamp == 0 {
		return time.Time{}
	}
	return r.routingStart.Add(time.Duration(stamp - 1))
}

// watchRouting changes the immutable subscriber list off the routing hot path.
func (r *Registry) watchRouting(beat *routingBeat) func() {
	r.mu.Lock()
	for {
		current := r.routing.Load()
		joined := append(append([]*routingBeat(nil), current.beats...), beat)
		next := &routingState{last: current.last, beats: joined}
		if r.routing.CompareAndSwap(current, next) {
			break
		}
	}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for {
			current := r.routing.Load()
			var remaining []*routingBeat
			for _, held := range current.beats {
				if held != beat {
					remaining = append(remaining, held)
				}
			}
			next := &routingState{last: current.last, beats: remaining}
			if r.routing.CompareAndSwap(current, next) {
				return
			}
		}
	}
}
