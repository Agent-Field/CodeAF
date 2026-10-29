package session

import (
	"runtime/debug"
	"sync"
)

// freeJournalReadMemory is replaceable under journalReadRelease.mu so tests can
// hold a collection in progress. A forced collection is paid only for journals
// too big to keep: otherwise a desktop can leave 300 MB per open resident until
// the runtime's next idle collection, even after the reading has been let go of.
var freeJournalReadMemory = debug.FreeOSMemory

var journalReadRelease struct {
	mu      sync.Mutex
	running bool
	pending bool
}

// releaseAfterLargeRead never waits for collection. Requests during one run
// share a single follow-up, so overlapping display and resume reads can both
// let go of their garbage without starting competing forced collections.
func releaseAfterLargeRead(size int64) {
	if size < recordCacheFileBytes {
		return
	}
	journalReadRelease.mu.Lock()
	defer journalReadRelease.mu.Unlock()
	if journalReadRelease.running {
		journalReadRelease.pending = true
		return
	}
	journalReadRelease.running = true
	go releaseJournalReadMemory()
}

func releaseJournalReadMemory() {
	journalReadRelease.mu.Lock()
	defer journalReadRelease.mu.Unlock()
	for {
		free := freeJournalReadMemory
		journalReadRelease.mu.Unlock()
		free()
		journalReadRelease.mu.Lock()
		if !journalReadRelease.pending {
			journalReadRelease.running = false
			return
		}
		journalReadRelease.pending = false
	}
}
