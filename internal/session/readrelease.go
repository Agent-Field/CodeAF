package session

import (
	"runtime"
	"sync"
	"time"
)

// releaseCooldown keeps sustained journal paging from repeatedly paying for a
// process-wide collection while the runtime scavenger returns the freed heap.
const releaseCooldown = 5 * time.Second

// freeJournalReadMemory is replaceable under journalReadRelease.mu so tests can
// hold a collection in progress. GC lowers the heap goal after a large read,
// letting the runtime's paced background scavenger return memory without the
// process-wide forced scavenge that debug.FreeOSMemory would add on every run.
var freeJournalReadMemory = runtime.GC

// The clock and scheduler are replaced under journalReadRelease.mu so cooldown
// tests can advance time without waiting for real collections or timers.
var journalReadNow = time.Now
var journalReadAfterFunc = func(delay time.Duration, run func()) { time.AfterFunc(delay, run) }

var journalReadRelease struct {
	mu        sync.Mutex
	running   bool
	pending   bool
	scheduled bool
	lastStart time.Time
}

// releaseAfterLargeRead never waits for collection. Requests share one trailing
// collection at the cooldown's end, even while an earlier collection is running.
func releaseAfterLargeRead(size int64) {
	if size < recordCacheFileBytes {
		return
	}
	journalReadRelease.mu.Lock()
	defer journalReadRelease.mu.Unlock()
	journalReadRelease.pending = true
	scheduleJournalReadReleaseLocked()
}

func scheduleJournalReadReleaseLocked() {
	if journalReadRelease.running || journalReadRelease.scheduled || !journalReadRelease.pending {
		return
	}
	now := journalReadNow()
	if delay := journalReadRelease.lastStart.Add(releaseCooldown).Sub(now); !journalReadRelease.lastStart.IsZero() && delay > 0 {
		journalReadRelease.scheduled = true
		journalReadAfterFunc(delay, func() {
			journalReadRelease.mu.Lock()
			defer journalReadRelease.mu.Unlock()
			journalReadRelease.scheduled = false
			scheduleJournalReadReleaseLocked()
		})
		return
	}
	journalReadRelease.pending = false
	journalReadRelease.running = true
	free := freeJournalReadMemory
	go func() {
		journalReadRelease.mu.Lock()
		journalReadRelease.lastStart = journalReadNow()
		journalReadRelease.mu.Unlock()
		free()
		journalReadRelease.mu.Lock()
		defer journalReadRelease.mu.Unlock()
		journalReadRelease.running = false
		// A request during GC keeps the deadline measured from its start. If GC
		// outlasts that deadline, the pending run can start as soon as it finishes.
		scheduleJournalReadReleaseLocked()
	}()
}
