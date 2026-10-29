package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitReadReleaseIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		journalReadRelease.mu.Lock()
		running := journalReadRelease.running
		journalReadRelease.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("journal read collection did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func replaceReadRelease(t *testing.T, free func()) {
	t.Helper()
	waitReadReleaseIdle(t)
	journalReadRelease.mu.Lock()
	prior := freeJournalReadMemory
	freeJournalReadMemory = free
	journalReadRelease.mu.Unlock()
	t.Cleanup(func() {
		waitReadReleaseIdle(t)
		journalReadRelease.mu.Lock()
		freeJournalReadMemory = prior
		journalReadRelease.mu.Unlock()
	})
}

func receiveReadRelease(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("journal read collection did not start or caller blocked")
	}
}

func TestLargeReadReleaseThreshold(t *testing.T) {
	var calls atomic.Int32
	replaceReadRelease(t, func() { calls.Add(1) })
	releaseAfterLargeRead(recordCacheFileBytes - 1)
	waitReadReleaseIdle(t)
	if n := calls.Load(); n != 0 {
		t.Fatalf("small read caused %d collections", n)
	}
	releaseAfterLargeRead(recordCacheFileBytes)
	waitReadReleaseIdle(t)
	if n := calls.Load(); n != 1 {
		t.Fatalf("one large read caused %d collections", n)
	}
}

func TestLargeReadReleaseCoalescesWithoutBlocking(t *testing.T) {
	entered := make(chan struct{}, 4)
	resume := make(chan struct{}, 2)
	var calls atomic.Int32
	replaceReadRelease(t, func() {
		calls.Add(1)
		entered <- struct{}{}
		<-resume
	})
	// Unblock a failed assertion before restoring the hook in the prior cleanup.
	t.Cleanup(func() { close(resume) })
	returned := make(chan struct{})
	go func() {
		releaseAfterLargeRead(recordCacheFileBytes)
		close(returned)
	}()
	receiveReadRelease(t, entered)
	receiveReadRelease(t, returned)
	var requests sync.WaitGroup
	for range 64 {
		requests.Go(func() { releaseAfterLargeRead(recordCacheFileBytes) })
	}
	done := make(chan struct{})
	go func() { requests.Wait(); close(done) }()
	receiveReadRelease(t, done)
	resume <- struct{}{}
	receiveReadRelease(t, entered)
	resume <- struct{}{}
	waitReadReleaseIdle(t)
	if n := calls.Load(); n != 2 {
		t.Fatalf("concurrent requests caused %d collections, want one plus one follow-up", n)
	}
}

// Whitespace lines grow the journal without making the fixture retain a large
// display record or depending on the scanner's limit for one very long line.
func readReleaseJournal(t *testing.T, size int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header := `{"type":"session","version":1,"id":"read-release"}
{"type":"message","role":"user","content":"kept words","timestamp":"2026-10-15T14:00:00Z"}
{"type":"compaction","folded":1,"window":1,"timestamp":"2026-10-15T14:01:00Z"}
{"type":"message","role":"user","content":"kept words","timestamp":"2026-10-15T14:01:00Z"}
`
	if _, err := f.WriteString(header); err != nil {
		t.Fatal(err)
	}
	padding := strings.Repeat(" ", (64<<10)-1) + "\n"
	for left := size - int64(len(header)); left > 0; {
		n := min(left, int64(len(padding)))
		if _, err := f.WriteString(padding[len(padding)-int(n):]); err != nil {
			t.Fatal(err)
		}
		left -= n
	}
	return path
}

func TestRecordReadReleaseFollowsCallback(t *testing.T) {
	for name, size := range map[string]int64{"below threshold": recordCacheFileBytes - 1, "at threshold": recordCacheFileBytes} {
		t.Run(name, func(t *testing.T) {
			path := readReleaseJournal(t, size)
			var calls atomic.Int32
			replaceReadRelease(t, func() { calls.Add(1) })
			// Both reads are refused retention, even the one just under the
			// threshold; only the shared large-file threshold pays for a GC.
			cache := settledCache(RecordCacheLimits{FileBytes: 1})
			if err := cache.WithRecord(path, func(Record) error {
				if calls.Load() != 0 {
					t.Fatal("collection ran before the callback returned")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			waitReadReleaseIdle(t)
			want := int32(0)
			if size >= recordCacheFileBytes {
				want = 1
			}
			if n := calls.Load(); n != want {
				t.Fatalf("collections = %d, want %d", n, want)
			}
		})
	}
}

func TestResumeReadReleaseAfterStampPrune(t *testing.T) {
	path := readReleaseJournal(t, recordCacheFileBytes)
	called := make(chan struct{}, 1)
	replaceReadRelease(t, func() { called <- struct{}{} })
	a, _ := newTestAgent(t, &refusingCompleter{t}, func(c *Config) { c.SessionFile = path })
	receiveReadRelease(t, called)
	waitReadReleaseIdle(t)
	assertLiveStamps(t, a)
}
