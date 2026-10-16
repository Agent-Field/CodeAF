package session

// A BYTE-BUDGETED LRU WITH COALESCED READS, for what [WorkSource] remembers.
//
// What the source keeps is a pure function of files, so forgetting it only costs
// a re-read; what it must never do is keep without limit. Two bounds hold the
// memory: a count of entries and a budget of BYTES, each entry carrying the size
// its builder estimated for it. The least recently used are dropped until both
// hold. A single value larger than half the budget is handed to the caller that
// asked and NOT retained, so one giant conversation can neither evict everything
// else nor sit in memory afterwards.
//
// Values are immutable once built, so a caller may keep reading one after it is
// evicted; eviction only drops this cache's reference.
//
// Reads are coalesced: callers asking for the same key and stamp at once share
// one read, run on a goroutine of its own under its own deadline and with no
// lock held. A caller whose context ends stops waiting, and when the last waiter
// leaves, the read is cancelled.

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// workReadTimeout bounds one coalesced read, whoever is waiting for it.
const workReadTimeout = 30 * time.Second

type lruEntry struct {
	key, stamp string
	value      any
	bytes      int64
}

type lruFlight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	value   any
	err     error
}

type byteLRU struct {
	mu         sync.Mutex
	maxBytes   int64
	maxEntries int
	bytes      int64
	lru        *list.List
	by         map[string]*list.Element
	flights    map[string]*lruFlight

	evicted    atomic.Int64
	unretained atomic.Int64
	coalesced  atomic.Int64
}

func newByteLRU(maxBytes int64, maxEntries int) *byteLRU {
	return &byteLRU{
		maxBytes: max(maxBytes, 1), maxEntries: max(maxEntries, 1),
		lru: list.New(), by: map[string]*list.Element{}, flights: map[string]*lruFlight{},
	}
}

// get is the value held for key if it was built under stamp.
func (c *byteLRU) get(key, stamp string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.by[key]
	if !ok || el.Value.(*lruEntry).stamp != stamp {
		return nil, false
	}
	c.lru.MoveToFront(el)
	return el.Value.(*lruEntry).value, true
}

// put keeps a value, replacing whatever key held, and reports whether it was
// retained: a value over half the budget is not.
func (c *byteLRU) put(key, stamp string, value any, bytes int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.by[key]; ok {
		c.dropLocked(el)
	}
	if bytes > c.maxBytes/2 {
		c.unretained.Add(1)
		return false
	}
	c.by[key] = c.lru.PushFront(&lruEntry{key: key, stamp: stamp, value: value, bytes: bytes})
	c.bytes += bytes
	for c.lru.Len() > c.maxEntries || c.bytes > c.maxBytes {
		c.dropLocked(c.lru.Back())
		c.evicted.Add(1)
	}
	return true
}

func (c *byteLRU) dropLocked(el *list.Element) {
	e := c.lru.Remove(el).(*lruEntry)
	delete(c.by, e.key)
	c.bytes -= e.bytes
}

func (c *byteLRU) forget(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.by[key]; ok {
		c.dropLocked(el)
	}
}

// held is how many entries and bytes are retained right now.
func (c *byteLRU) held() (entries int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len(), c.bytes
}

// do runs read once for everyone asking for flight at the same time.
func (c *byteLRU) do(ctx context.Context, flight string, read func(context.Context) (any, error)) (any, error) {
	c.mu.Lock()
	f, ok := c.flights[flight]
	if ok {
		c.coalesced.Add(1)
	} else {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workReadTimeout)
		f = &lruFlight{done: make(chan struct{}), cancel: cancel}
		c.flights[flight] = f
		go func() {
			defer cancel()
			value, err := read(rctx)
			c.mu.Lock()
			f.value, f.err = value, err
			if c.flights[flight] == f {
				delete(c.flights, flight)
			}
			c.mu.Unlock()
			close(f.done)
		}()
	}
	f.waiters++
	c.mu.Unlock()
	select {
	case <-f.done:
		return f.value, f.err
	case <-ctx.Done():
		c.mu.Lock()
		f.waiters--
		if f.waiters == 0 && c.flights[flight] == f {
			// NOBODY WANTS THIS READ: it is cancelled and forgotten at once, so a
			// caller arriving next starts a read of its own rather than joining
			// one that is about to answer with a cancellation.
			f.cancel()
			delete(c.flights, flight)
		}
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}
