package workgraph

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// BuildTimeout bounds one snapshot build, whoever is waiting for it.
const BuildTimeout = 10 * time.Second

// Cache keeps built snapshots keyed by (scope, revision). THE REVISION IS THE
// PRODUCER'S CLAIM ABOUT ITS SOURCES — stat stamps of the files a snapshot reads,
// computed before any of them is read — so an unchanged source is one map
// lookup, and a streaming token that touches no task source never rebuilds a
// graph. Three bounds hold it: the entry count, the total nodes held, and one
// build in flight per key.
//
// IO happens only inside the build function, which runs with no lock of this
// cache held, so a slow disk never blocks a lookup of another scope.
type Cache struct {
	mu         sync.Mutex
	maxEntries int
	maxNodes   int
	nodes      int
	lru        *list.List
	byKey      map[string]*list.Element
	flights    map[string]*flight

	builds atomic.Int64
	hits   atomic.Int64
}

type entry struct {
	key   string
	scope string
	snap  *Snapshot
}

type flight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	snap    *Snapshot
	err     error
}

// NewCache is a cache of at most maxEntries snapshots holding at most maxNodes
// nodes between them. A single snapshot larger than maxNodes is still served,
// and is the only one kept.
func NewCache(maxEntries, maxNodes int) *Cache {
	return &Cache{
		maxEntries: max(maxEntries, 1), maxNodes: max(maxNodes, 1),
		lru: list.New(), byKey: map[string]*list.Element{}, flights: map[string]*flight{},
	}
}

// Stats is how many snapshots were built and how many lookups were served from
// memory, for tests and for the app's own diagnostics.
func (c *Cache) Stats() (builds, hits int64) { return c.builds.Load(), c.hits.Load() }

// Builder makes the [Input] for one scope. It runs without any cache lock and
// must honour ctx.
type Builder func(ctx context.Context) (Input, error)

// Get is the snapshot for scope at revision: the cached one, the one another
// caller is building right now, or a fresh one built by build. A caller whose
// own ctx ends stops waiting; the build is abandoned only when nobody waits.
func (c *Cache) Get(ctx context.Context, scope GraphScope, revision string, build Builder) (*Snapshot, error) {
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
		f = c.start(ctx, key, scope, revision, build)
	}
	f.waiters++
	c.mu.Unlock()
	return c.wait(ctx, key, f)
}

// start begins a build in its own goroutine. The build's context is NOT the
// first caller's: a caller that goes away must not fail the callers that joined
// it. Called with c.mu held.
func (c *Cache) start(ctx context.Context, key string, scope GraphScope, revision string, build Builder) *flight {
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), BuildTimeout)
	f := &flight{done: make(chan struct{}), cancel: cancel}
	c.flights[key] = f
	go func() {
		defer cancel()
		in, err := build(bctx)
		var snap *Snapshot
		if err == nil {
			in.Scope, in.Revision = scope, revision
			snap, err = Build(bctx, in)
		}
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
// the same scope) and then the least recently used until the bounds hold.
// Called with c.mu held.
func (c *Cache) store(key, scope string, snap *Snapshot) {
	for el := c.lru.Back(); el != nil; {
		prev := el.Prev()
		if e := el.Value.(*entry); e.scope == scope {
			c.drop(el)
		}
		el = prev
	}
	c.byKey[key] = c.lru.PushFront(&entry{key: key, scope: scope, snap: snap})
	c.nodes += snap.Len()
	for c.lru.Len() > 1 && (c.lru.Len() > c.maxEntries || c.nodes > c.maxNodes) {
		c.drop(c.lru.Back())
	}
}

func (c *Cache) drop(el *list.Element) {
	e := c.lru.Remove(el).(*entry)
	delete(c.byKey, e.key)
	c.nodes -= e.snap.Len()
}

// Held is how many snapshots and nodes the cache holds right now.
func (c *Cache) Held() (entries, nodes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len(), c.nodes
}
