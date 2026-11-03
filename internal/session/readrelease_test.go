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

func waitReadReleaseRunFinished(t *testing.T) {
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

type readReleaseTimer struct {
	at  time.Time
	run func()
}

type readReleaseClock struct {
	mu        sync.Mutex
	now       time.Time
	timer     *readReleaseTimer
	schedules int
}

func (c *readReleaseClock) instant() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *readReleaseClock) afterFunc(delay time.Duration, run func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		panic("more than one journal read release timer")
	}
	c.schedules++
	c.timer = &readReleaseTimer{at: c.now.Add(delay), run: run}
}

func (c *readReleaseClock) advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	for c.timer != nil && !c.timer.at.After(target) {
		timer := c.timer
		c.now, c.timer = timer.at, nil
		c.mu.Unlock()
		timer.run()
		c.mu.Lock()
	}
	c.now = target
	c.mu.Unlock()
}

func replaceReadRelease(t *testing.T, free func()) *readReleaseClock {
	t.Helper()
	waitReadReleaseRunFinished(t)
	clock := &readReleaseClock{now: time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC)}
	journalReadRelease.mu.Lock()
	priorFree, priorNow, priorAfter := freeJournalReadMemory, journalReadNow, journalReadAfterFunc
	if journalReadRelease.scheduled || journalReadRelease.pending {
		journalReadRelease.mu.Unlock()
		t.Fatal("prior journal read release still scheduled")
	}
	priorStart := journalReadRelease.lastStart
	journalReadRelease.lastStart = time.Time{}
	freeJournalReadMemory, journalReadNow, journalReadAfterFunc = free, clock.instant, clock.afterFunc
	journalReadRelease.mu.Unlock()
	t.Cleanup(func() {
		waitReadReleaseRunFinished(t)
		journalReadRelease.mu.Lock()
		journalReadRelease.pending, journalReadRelease.scheduled = false, false
		journalReadRelease.lastStart = priorStart
		freeJournalReadMemory, journalReadNow, journalReadAfterFunc = priorFree, priorNow, priorAfter
		journalReadRelease.mu.Unlock()
	})
	return clock
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
	clock := replaceReadRelease(t, func() { calls.Add(1) })
	releaseAfterLargeRead(recordCacheFileBytes - 1)
	waitReadReleaseRunFinished(t)
	if n := calls.Load(); n != 0 {
		t.Fatalf("small read caused %d collections", n)
	}
	releaseAfterLargeRead(recordCacheFileBytes)
	waitReadReleaseRunFinished(t)
	clock.advance(2 * releaseCooldown)
	waitReadReleaseRunFinished(t)
	if n := calls.Load(); n != 1 || clock.schedules != 0 {
		t.Fatalf("one large read caused %d collections and %d timers, want one prompt collection", n, clock.schedules)
	}
}

func TestLargeReadReleaseBurstHasOneTrailingCollection(t *testing.T) {
	var calls atomic.Int32
	clock := replaceReadRelease(t, func() { calls.Add(1) })
	releaseAfterLargeRead(recordCacheFileBytes)
	waitReadReleaseRunFinished(t)
	for range 64 {
		releaseAfterLargeRead(recordCacheFileBytes)
	}
	clock.advance(releaseCooldown - time.Nanosecond)
	waitReadReleaseRunFinished(t)
	if n := calls.Load(); n != 1 || clock.schedules != 1 {
		t.Fatalf("burst before deadline: %d collections, %d timers, want 1 each", n, clock.schedules)
	}
	clock.advance(time.Nanosecond)
	waitReadReleaseRunFinished(t)
	clock.advance(2 * releaseCooldown)
	waitReadReleaseRunFinished(t)
	if n := calls.Load(); n != 2 || clock.schedules != 1 {
		t.Fatalf("burst: %d collections, %d timers, want one prompt and one trailing collection", n, clock.schedules)
	}
}

func TestLargeReadReleaseSustainedRequestsStayBounded(t *testing.T) {
	var startsMu sync.Mutex
	var starts []time.Time
	clock := replaceReadRelease(t, func() {
		journalReadRelease.mu.Lock()
		at := journalReadRelease.lastStart
		journalReadRelease.mu.Unlock()
		startsMu.Lock()
		starts = append(starts, at)
		startsMu.Unlock()
	})
	start := clock.instant()
	releaseAfterLargeRead(recordCacheFileBytes)
	waitReadReleaseRunFinished(t)
	const duration, interval = 30 * time.Second, 900 * time.Millisecond
	for elapsed := interval; elapsed < duration; elapsed += interval {
		clock.advance(interval)
		waitReadReleaseRunFinished(t)
		releaseAfterLargeRead(recordCacheFileBytes)
	}
	lastRequest := clock.instant()
	clock.advance(releaseCooldown)
	waitReadReleaseRunFinished(t)
	startsMu.Lock()
	defer startsMu.Unlock()
	limit := int((duration+releaseCooldown-1)/releaseCooldown) + 1
	if len(starts) > limit || len(starts) < 2 || !starts[len(starts)-1].After(lastRequest) {
		t.Fatalf("sustained requests: %d collections, limit %d, starts %v, last request %v", len(starts), limit, starts, lastRequest)
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < releaseCooldown {
			t.Fatalf("collections %d and %d separated by %v, want at least %v", i-1, i, gap, releaseCooldown)
		}
	}
	if !starts[0].Equal(start) {
		t.Fatalf("first collection delayed until %v, want %v", starts[0], start)
	}
	t.Logf("30 s of sustained requests: %d collections (limit %d), final collection after last request", len(starts), limit)
}

func TestLargeReadReleaseCoalescesWithoutBlocking(t *testing.T) {
	for _, blocked := range []time.Duration{time.Second, 2 * releaseCooldown} {
		t.Run(blocked.String(), func(t *testing.T) {
			entered := make(chan struct{}, 4)
			resume := make(chan struct{}, 2)
			var calls atomic.Int32
			clock := replaceReadRelease(t, func() {
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
			clock.advance(blocked)
			if n := calls.Load(); n != 1 {
				t.Fatalf("collection overlapped a blocked run: %d calls", n)
			}
			resume <- struct{}{}
			if blocked < releaseCooldown {
				waitReadReleaseRunFinished(t)
				clock.advance(releaseCooldown - blocked - time.Nanosecond)
				if n := calls.Load(); n != 1 {
					t.Fatalf("follow-up started before cooldown: %d calls", n)
				}
				clock.advance(time.Nanosecond)
			}
			receiveReadRelease(t, entered)
			resume <- struct{}{}
			waitReadReleaseRunFinished(t)
			clock.advance(2 * releaseCooldown)
			waitReadReleaseRunFinished(t)
			if n := calls.Load(); n != 2 {
				t.Fatalf("concurrent requests caused %d collections, want one plus one follow-up", n)
			}
		})
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
			waitReadReleaseRunFinished(t)
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
	waitReadReleaseRunFinished(t)
	assertLiveStamps(t, a, journalStampInstants(t, a, nil))
}
