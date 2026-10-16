package workgraph

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// BuildTimeout bounds one snapshot build, whoever is waiting for it.
const BuildTimeout = 10 * time.Second

// ErrBusy is returned when the cache already has as many distinct builds
// pending as it admits. It is an answer, not a failure of the graph: the same
// ask a moment later may be served.
var ErrBusy = errors.New("workgraph: busy")

// CacheLimits are the hard bounds on a [Cache]. Every field is a ceiling on
// something the cache can be made to hold or to run, and a zero is raised to the
// smallest working value, never to "unbounded".
type CacheLimits struct {
	// Bytes bounds the snapshots HELD, by their [Snapshot.Bytes] estimate. A
	// snapshot larger than this is served to the caller that built it and to the
	// callers waiting on it, and then forgotten: nothing is exempt, and nothing
	// is ever truncated to fit.
	Bytes int64
	// Entries bounds how many snapshots are held, whatever their size.
	Entries int
	// Pending bounds the distinct builds queued or running at once. A caller
	// that would start one more is refused with [ErrBusy]. Callers asking for a
	// build already pending join it and cost nothing, so this bounds goroutines
	// by builds, never by requests.
	Pending int
	// BuildBytes bounds the weight of the builds RUNNING at once. A build
	// brings its own weight (an estimate of the memory its input costs); one
	// heavier than this runs alone rather than never.
	BuildBytes int64
}

// Cache keeps built snapshots keyed by (scope, revision). THE REVISION IS THE
// PRODUCER'S CLAIM ABOUT ITS SOURCES — stat stamps of the files a snapshot reads,
// computed before any of them is read — so an unchanged source is one map
// lookup, and a streaming token that touches no task source never rebuilds a
// graph. Four bounds hold it: bytes held, entries held, distinct builds pending,
// and the weight of builds running; plus one build in flight per key.
//
// IO happens only inside the build function, which runs with no lock of this
// cache held, so a slow disk never blocks a lookup of another scope.
type Cache struct {
	mu      sync.Mutex
	limits  CacheLimits
	bytes   int64
	lru     *list.List
	byKey   map[string]*list.Element
	flights map[string]*flight
	gate    *Gate

	builds   atomic.Int64
	hits     atomic.Int64
	busy     atomic.Int64
	unstored atomic.Int64
}

type entry struct {
	key   string
	scope string
	snap  *Snapshot
	bytes int64
}

type flight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	snap    *Snapshot
	err     error
}

// NewCache is a cache held to limits.
func NewCache(l CacheLimits) *Cache {
	l.Bytes, l.Entries = max(l.Bytes, 1), max(l.Entries, 1)
	l.Pending, l.BuildBytes = max(l.Pending, 1), max(l.BuildBytes, 1)
	return &Cache{
		limits: l, lru: list.New(), byKey: map[string]*list.Element{}, flights: map[string]*flight{},
		gate: NewGate(l.BuildBytes),
	}
}

// Stats is how many snapshots were built, how many lookups were served from
// memory, how many callers were refused as busy and how many built snapshots were
// too large to keep, for tests and for the app's own diagnostics.
func (c *Cache) Stats() (builds, hits int64) { return c.builds.Load(), c.hits.Load() }

// Refusals is how many asks were refused with [ErrBusy] and how many snapshots
// were served without being kept.
func (c *Cache) Refusals() (busy, unstored int64) { return c.busy.Load(), c.unstored.Load() }

// Builder makes the [Input] for one scope. It runs without any cache lock and
// must honour ctx.
type Builder func(ctx context.Context) (Input, error)

// Get is [Cache.GetWeighted] for a build of unknown weight, taken as light.
func (c *Cache) Get(ctx context.Context, scope GraphScope, revision string, build Builder) (*Snapshot, error) {
	return c.GetWeighted(ctx, scope, revision, 1, build)
}

// GetWeighted is the snapshot for scope at revision: the cached one, the one
// another caller is building right now, or a fresh one built by build, which is
// admitted to run when weight bytes of build capacity are free. A caller whose
// own ctx ends stops waiting; the build is abandoned only when nobody waits. A
// caller that would start a build beyond the pending limit gets [ErrBusy].
func (c *Cache) GetWeighted(ctx context.Context, scope GraphScope, revision string, weight int64, build Builder) (*Snapshot, error) {
	key := scopeKey(scope) + "\x00" + revision
	c.mu.Lock()
	if el, ok := c.byKey[key]; ok {
		c.lru.MoveToFront(el)
		snap := el.Value.(*entry).snap
		c.mu.Unlock()
		c.hits.Add(1)
		return snap, nil
	}
	f, ok := c.flights[key]
	if !ok {
		if len(c.flights) >= c.limits.Pending {
			c.mu.Unlock()
			c.busy.Add(1)
			return nil, ErrBusy
		}
		f = c.start(ctx, key, scope, revision, weight, build)
	}
	f.waiters++
	c.mu.Unlock()
	return c.wait(ctx, key, f)
}

// start begins a build in its own goroutine, which first waits its turn at the
// gate. The build's context is NOT the first caller's: a caller that goes away
// must not fail the callers that joined it. Called with c.mu held.
func (c *Cache) start(ctx context.Context, key string, scope GraphScope, revision string, weight int64, build Builder) *flight {
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), BuildTimeout)
	f := &flight{done: make(chan struct{}), cancel: cancel}
	c.flights[key] = f
	go func() {
		defer cancel()
		snap, err := c.run(bctx, scope, revision, weight, build)
		c.builds.Add(1)
		c.mu.Lock()
		f.snap, f.err = snap, err
		delete(c.flights, key)
		if err == nil {
			c.store(key, scopeKey(scope), snap)
		}
		c.mu.Unlock()
		close(f.done)
	}()
	return f
}

// run is one build, admitted by weight. The gate is released as soon as the
// snapshot exists, because the input it weighed is garbage by then.
func (c *Cache) run(ctx context.Context, scope GraphScope, revision string, weight int64, build Builder) (*Snapshot, error) {
	held, err := c.gate.Acquire(ctx, weight)
	if err != nil {
		return nil, err
	}
	defer c.gate.Release(held)
	in, err := build(ctx)
	if err != nil {
		return nil, err
	}
	in.Scope, in.Revision = scope, revision
	return Build(ctx, in)
}

// wait blocks for a flight or for the caller's own ctx, and when the last
// waiter leaves a build nobody wants any more, cancels it.
func (c *Cache) wait(ctx context.Context, key string, f *flight) (*Snapshot, error) {
	select {
	case <-f.done:
		return f.snap, f.err
	case <-ctx.Done():
		c.mu.Lock()
		f.waiters--
		if f.waiters == 0 && c.flights[key] == f {
			f.cancel()
		}
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// store keeps a snapshot, first dropping what it replaces (an older revision of
// the same scope) and then the least recently used until the bounds hold. A
// snapshot that cannot fit even alone is not kept. Called with c.mu held.
func (c *Cache) store(key, scope string, snap *Snapshot) {
	size := snap.Bytes()
	for el := c.lru.Back(); el != nil; {
		prev := el.Prev()
		if e := el.Value.(*entry); e.scope == scope {
			c.drop(el)
		}
		el = prev
	}
	if size > c.limits.Bytes {
		c.unstored.Add(1)
		return
	}
	c.byKey[key] = c.lru.PushFront(&entry{key: key, scope: scope, snap: snap, bytes: size})
	c.bytes += size
	for c.lru.Len() > c.limits.Entries || c.bytes > c.limits.Bytes {
		c.drop(c.lru.Back())
	}
}

func (c *Cache) drop(el *list.Element) {
	e := c.lru.Remove(el).(*entry)
	delete(c.byKey, e.key)
	c.bytes -= e.bytes
}

// Held is how many snapshots and estimated bytes the cache holds right now.
func (c *Cache) Held() (entries int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len(), c.bytes
}

// Pending is how many distinct builds are queued or running right now.
func (c *Cache) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.flights)
}
