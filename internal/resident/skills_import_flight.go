package resident

import (
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/store"
)

// importFlight coalesces import passes: every launch in a process asks for one,
// and N concurrent asks for the same shelf and the same two directories need
// two passes, not N — the one already running, and a single trailing one for
// everybody who arrived while it ran.
//
// THE TRAILING PASS IS THE CORRECTNESS RULE. A caller that arrives while a
// pass is mid-scan cannot adopt its result, because the disk may have changed
// after that scan read it, and the whole point of the launch import is that a
// skill edited before the open is seen before the first message. So a caller
// remembers how many passes had STARTED when it arrived and returns only once
// a pass that started later has finished. Waiters share that one pass.
//
// Entries exist only while a caller is inside them, so the map is bounded by
// the number of concurrent launches, never by the number of paths seen.
type importFlight struct {
	mu      sync.Mutex
	entries map[importKey]*importEntry
	shelves map[*store.Store]*shelfGate
}

// shelfGate serialises a complete scan and publication on one store. It is
// refcounted like the entries, so the map holds shelves with a publisher in or
// waiting at the door and nothing else.
type shelfGate struct {
	mu      sync.Mutex
	holders int // guarded by importFlight.mu
}

type importKey struct {
	shelf   *store.Store
	project string
	home    string
}

type importEntry struct {
	run       sync.Mutex
	started   atomic.Uint64
	completed uint64 // guarded by run; a panicked pass never advances it
	holders   int    // guarded by importFlight.mu
}

var importPasses = newImportFlight()

func newImportFlight() *importFlight {
	return &importFlight{entries: make(map[importKey]*importEntry), shelves: make(map[*store.Store]*shelfGate)}
}

// publish runs a complete scan and publication alone on one shelf. Other
// shelves never wait here. The private gate is released after the pass settles.
func (f *importFlight) publish(shelf *store.Store, write func()) {
	gate := f.holdShelf(shelf)
	defer f.releaseShelf(shelf, gate)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	write()
}

// do runs pass at least once, started after do was called, sharing it with any
// concurrent caller on the same key.
func (f *importFlight) do(key importKey, pass func()) {
	entry, arrived := f.holdEntry(key)
	defer f.releaseEntry(key, entry)

	entry.run.Lock()
	defer entry.run.Unlock()
	if entry.completed > arrived {
		return
	}
	number := entry.started.Add(1)
	pass()
	entry.completed = number
}

func (f *importFlight) holdShelf(shelf *store.Store) *shelfGate {
	f.mu.Lock()
	defer f.mu.Unlock()
	gate := f.shelves[shelf]
	if gate == nil {
		gate = &shelfGate{}
		f.shelves[shelf] = gate
	}
	gate.holders++
	return gate
}

func (f *importFlight) releaseShelf(shelf *store.Store, gate *shelfGate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if gate.holders--; gate.holders == 0 {
		delete(f.shelves, shelf)
	}
}

func (f *importFlight) holdEntry(key importKey) (*importEntry, uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := f.entries[key]
	if entry == nil {
		entry = &importEntry{}
		f.entries[key] = entry
	}
	entry.holders++
	// Capture arrival with its holder registration, before any pass can be
	// adopted. A caller arriving during a pass still requires a later one.
	return entry, entry.started.Load()
}

func (f *importFlight) releaseEntry(key importKey, entry *importEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry.holders--; entry.holders == 0 {
		delete(f.entries, key)
	}
}

func (f *importFlight) idle() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries) + len(f.shelves)
}

// waiting is how many callers hold the key, the running one included.
func (f *importFlight) waiting(key importKey) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry := f.entries[key]; entry != nil {
		return entry.holders
	}
	return 0
}
