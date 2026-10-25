package session

// deliverable_registry.go bounds how many update trackers the process holds.
//
// A tracker is a cache of where each deliverable's updates sit in one index,
// rebuilt from that index whenever it is missing. The process once kept one
// for every index path it had ever read, forever; the update count was
// bounded, but the number of trackers, their keys and their pending line heads
// were not. THE LAW HERE: at most deliverIndexMax trackers live at once, each
// under a key of at most deliverIndexKeyMax bytes. One in use (leased) is never
// evicted; an idle one may be, least recently used first, and evicting it
// gives back its share of the process-wide update count exactly once. A
// tracker that was evicted is rebuilt from the index on its next use, so
// eviction costs a re-read and never an answer: nothing canonical lives here.

import (
	"container/list"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// deliverIndexMax is the most trackers the process holds at once. A
	// process reads one artifacts index of its own and a few more at most, so
	// this is room, not a quota. Every one in use is bounded too: an active
	// tracker holds at most one pending line head (mediaRowMax), an idle one
	// none, so listing heads are at most deliverIndexMax x 256 KiB. The
	// path index adds one active head process-wide under artifactsMu, and
	// drops it before releasing its lease.
	deliverIndexMax = 64
	// deliverLeaseWaitMax is the longest a caller with no deadline of its own
	// waits for a tracker to come free when every one is in use.
	deliverLeaseWaitMax = 2 * time.Second
)

// deliverIndexKeyMax is the longest index path a tracker is kept under.
const deliverIndexKeyMax = 4096

// ErrDeliverIndexPath is an index path longer than any tracker is kept under.
var ErrDeliverIndexPath = errors.New("the deliverables index path is too long to read")

type trackerRegistry struct {
	mu   sync.Mutex
	live map[string]*deliverTracker
	// idle holds the trackers nobody is using, most recently released first.
	idle list.List
	// freed is closed, and replaced, whenever a tracker becomes idle, so a
	// caller waiting for room wakes without a queue of its own.
	freed chan struct{}
}

var deliverRegistry = &trackerRegistry{live: map[string]*deliverTracker{}, freed: make(chan struct{})}

// leaseTracker is index's tracker, held until release is called (once; a
// second call does nothing). When every tracker the process may hold is in
// use it waits, until the deadline (or deliverLeaseWaitMax with none), for one
// to come free, and then answers [ErrMediaBusy]. It starts no goroutine.
func leaseTracker(index string, deadline time.Time) (*deliverTracker, func(), error) {
	if len(index) > deliverIndexKeyMax {
		return nil, nil, ErrDeliverIndexPath
	}
	if limit := time.Now().Add(deliverLeaseWaitMax); deadline.IsZero() || deadline.After(limit) {
		deadline = limit
	}
	r := deliverRegistry
	var timer *time.Timer
	for {
		t, freed := r.admit(index)
		if t != nil {
			if timer != nil {
				timer.Stop()
			}
			return t, r.releaser(t), nil
		}
		if timer == nil {
			timer = time.NewTimer(time.Until(deadline))
			defer timer.Stop()
		}
		select {
		case <-freed:
		case <-timer.C:
			return nil, nil, ErrMediaBusy
		}
	}
}

// admit leases index's tracker, making one (and evicting the least recently
// used idle one to make room) when it has none. When there is no room and
// nothing idle it answers the channel that says something became idle.
func (r *trackerRegistry) admit(index string) (*deliverTracker, chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t := r.live[index]; t != nil {
		r.takeLocked(t)
		return t, nil
	}
	if len(r.live) >= deliverIndexMax {
		oldest := r.idle.Back()
		if oldest == nil {
			return nil, r.freed
		}
		r.evictLocked(oldest.Value.(*deliverTracker))
	}
	t := &deliverTracker{sem: make(chan struct{}, 1), key: index}
	r.live[index] = t
	r.takeLocked(t)
	return t, nil
}

func (r *trackerRegistry) takeLocked(t *deliverTracker) {
	if t.idleAt != nil {
		r.idle.Remove(t.idleAt)
		t.idleAt = nil
	}
	t.leases++
}

// releaser is the one-shot release of a lease on t.
func (r *trackerRegistry) releaser(t *deliverTracker) func() {
	var done atomic.Bool
	return func() {
		if done.CompareAndSwap(false, true) {
			r.release(t)
		}
	}
}

func (r *trackerRegistry) release(t *deliverTracker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.leases--; t.leases > 0 {
		return
	}
	t.settle()
	t.idleAt = r.idle.PushFront(t)
	close(r.freed)
	r.freed = make(chan struct{})
}

// evictLocked forgets an idle tracker and gives back its updates' share of the
// process-wide count. It is reached only for a tracker with no lease, which no
// caller can still be using, and leaves it empty so it cannot be given back
// twice.
func (r *trackerRegistry) evictLocked(t *deliverTracker) {
	if t.idleAt != nil {
		r.idle.Remove(t.idleAt)
		t.idleAt = nil
	}
	delete(r.live, t.key)
	t.forget()
}

// settle makes an idle tracker hold no line head: a short pending line is read
// again from its start next time (at most mediaRowMax bytes), while an
// oversize line keeps its skip, which costs no bytes and is what lets a run
// longer than one budget be passed at all.
func (t *deliverTracker) settle() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.scan.carry != nil {
		t.scan.pos, t.scan.carry = t.scan.line, nil
	}
}

// forget empties a tracker and gives back what it counted, once.
func (t *deliverTracker) forget() {
	t.mu.Lock()
	defer t.mu.Unlock()
	deliverTracked.Add(-int64(t.count))
	t.updates, t.scan, t.count = nil, deliverScan{}, 0
}
