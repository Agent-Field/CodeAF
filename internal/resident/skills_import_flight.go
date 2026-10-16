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
	run     sync.Mutex
	started atomic.Uint64
	holders int // guarded by importFlight.mu
}

var importPasses = newImportFlight()

func newImportFlight() *importFlight {
	return &importFlight{entries: make(map[importKey]*importEntry), shelves: make(map[*store.Store]*shelfGate)}
}

// publish runs a complete scan and publication alone on one shelf. Other
// shelves never wait here. The private gate is released after the pass settles.
func (f *importFlight) publish(shelf *store.Store, write func()) {
	f.mu.Lock()
	gate := f.shelves[shelf]
	if gate == nil {
		gate = &shelfGate{}
		f.shelves[shelf] = gate
	}
	gate.holders++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		if gate.holders--; gate.holders == 0 {
			delete(f.shelves, shelf)
		}
		f.mu.Unlock()
	}()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	write()
}

// do runs pass at least once, started after do was called, sharing it with any
// concurrent caller on the same key.
func (f *importFlight) do(key importKey, pass func()) {
	f.mu.Lock()
	entry := f.entries[key]
	if entry == nil {
		entry = &importEntry{}
		f.entries[key] = entry
	}
	entry.holders++
	// Read under the same hold that counts the caller in, so "holders" and
	// "arrived" cannot disagree about who was here before a pass started.
	arrived := entry.started.Load()
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		if entry.holders--; entry.holders == 0 {
			delete(f.entries, key)
		}
		f.mu.Unlock()
	}()

	entry.run.Lock()
	defer entry.run.Unlock()
	if entry.started.Load() > arrived {
		return
	}
	entry.started.Add(1)
	pass()
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
