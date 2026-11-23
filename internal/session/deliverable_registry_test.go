package session

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// resetTrackers evicts every tracker and proves the process-wide update count
// went back to exactly zero: never negative (a double give-back), never left
// over (a tracker forgotten without giving back).
func resetTrackers(t *testing.T) {
	t.Helper()
	r := deliverRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, tracker := range r.live {
		if tracker.leases != 0 {
			t.Fatalf("a tracker is still leased (%d) between tests", tracker.leases)
		}
		r.evictLocked(tracker)
	}
	if got := deliverTracked.Load(); got != 0 {
		t.Fatalf("after evicting every tracker the process still counts %d updates", got)
	}
}

// withIndexMax runs a test with a registry of at most n trackers, starting
// and ending empty.
func withIndexMax(t *testing.T, n int) {
	t.Helper()
	resetTrackers(t)
	old := deliverIndexMax
	deliverIndexMax = n
	t.Cleanup(func() { deliverIndexMax = old; resetTrackers(t) })
}

// registryState is how many trackers live, how many are idle, and whether the
// process count is exactly the sum of what live trackers hold.
func registryState(t *testing.T) (live, idle int) {
	t.Helper()
	r := deliverRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	var sum int64
	for _, tracker := range r.live {
		tracker.mu.RLock()
		sum += int64(tracker.count)
		tracker.mu.RUnlock()
	}
	if got := deliverTracked.Load(); got != sum {
		t.Fatalf("the process counts %d updates but its trackers hold %d", got, sum)
	}
	return len(r.live), r.idle.Len()
}

// indexWith writes an index of n deliverables, the first `removed` of them
// removed by an update.
func indexWith(t *testing.T, n, removed int) (string, []string) {
	t.Helper()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, index, n)
	for _, id := range ids[:removed] {
		row, err := FindDeliverableAfter(t, index, id)
		if err != nil {
			t.Fatal(err)
		}
		AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.Removed = true })
	}
	return index, ids
}

// Thousands of indexes, read once each, leave at most the bound behind, every
// one idle, and the update count exactly what those few still hold.
func TestThousandsOfIndexesLeaveOnlyTheBoundBehind(t *testing.T) {
	withIndexMax(t, 16)
	dir := t.TempDir()
	for i := 0; i < 3000; i++ {
		index := filepath.Join(dir, strconv.Itoa(i)+".jsonl")
		RecordArtifact(index, Artifact{Path: "/work/a.md", Session: "agent", Title: "a.md", Kind: DeliverableMarkdown, Created: time.Unix(1, 0), Deliver: &DeliverableFact{ID: NewDeliverableID()}})
		if page, err := ListDeliverables(index, DeliverableQuery{}, "", 10, time.Time{}, 0, nil); err != nil || len(page.Rows) != 1 {
			t.Fatalf("index %d: %v %d rows", i, err, len(page.Rows))
		}
	}
	if live, idle := registryState(t); live != 16 || idle != live {
		t.Fatalf("%d trackers live, %d idle, after 3000 indexes under a bound of 16", live, idle)
	}
	for i := 0; i < 40; i++ {
		index, _ := indexWith(t, 3, 2)
		if rows := listDeliverables(t, index, DeliverableQuery{}, 10); len(rows) != 1 {
			t.Fatalf("index %d: %d rows, want 1", i, len(rows))
		}
	}
	if live, _ := registryState(t); live > 16 {
		t.Fatalf("%d trackers live under a bound of 16", live)
	}
}

// An evicted index is rebuilt from the index itself on its next read: the
// removed row stays removed, the kept one stays kept, and the count it gave
// back is counted again exactly once.
func TestAnEvictedIndexIsRebuiltTruthfully(t *testing.T) {
	withIndexMax(t, 1)
	first, ids := indexWith(t, 3, 1)
	row, _ := FindDeliverableAfter(t, first, ids[1])
	AppendDeliverableUpdate(first, row, func(f *DeliverableFact) { f.State = DeliverKept })
	check := func(when string) {
		rows := listDeliverables(t, first, DeliverableQuery{}, 10)
		if len(rows) != 2 {
			t.Fatalf("%s: %d rows, want 2", when, len(rows))
		}
		for _, r := range rows {
			if r.ID == ids[0] {
				t.Fatalf("%s: the removed row came back", when)
			}
			if r.ID == ids[1] && r.Fact.State != DeliverKept {
				t.Fatalf("%s: the kept row lost its state: %+v", when, r.Fact)
			}
		}
	}
	check("first read")
	if got := deliverTracked.Load(); got != 2 {
		t.Fatalf("first index counts %d updates, want 2", got)
	}
	second, _ := indexWith(t, 2, 0)
	listDeliverables(t, second, DeliverableQuery{}, 10)
	if peekTracker(first) != nil || deliverTracked.Load() != 0 {
		t.Fatalf("the idle first index was not evicted, or its count not given back: %d", deliverTracked.Load())
	}
	check("after eviction")
	if live, _ := registryState(t); live != 1 || deliverTracked.Load() != 2 {
		t.Fatalf("%d live, %d counted after the rebuild", live, deliverTracked.Load())
	}
}

// A tracker in use is never evicted: with every slot leased, another index
// waits only until its deadline and answers busy, the leased tracker is the
// same one throughout, and once it is released the other index is read.
func TestEveryTrackerInUseIsBusyWithinTheDeadline(t *testing.T) {
	withIndexMax(t, 2)
	a, _ := indexWith(t, 2, 1)
	b, _ := indexWith(t, 2, 0)
	c, _ := indexWith(t, 2, 0)
	ta, releaseA, err := leaseTracker(a, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	_, releaseB, err := leaseTracker(b, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := ListDeliverables(c, DeliverableQuery{}, "", 10, time.Now().Add(50*time.Millisecond), 0, nil); !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("every tracker in use: %v", err)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Fatalf("busy took %v", took)
	}
	old := deliverLeaseWaitMax
	deliverLeaseWaitMax = 50 * time.Millisecond
	_, err = FindDeliverable(c, "agent", "dlv_"+strings.Repeat("0", 32), time.Time{})
	deliverLeaseWaitMax = old
	if !errors.Is(err, ErrMediaBusy) {
		t.Fatalf("a caller with no deadline waited past the bound: %v", err)
	}
	if peekTracker(a) != ta {
		t.Fatal("a leased tracker was evicted")
	}
	// A waiter wakes when a lease ends rather than at its deadline.
	done := make(chan error, 1)
	go func() {
		_, err := ListDeliverables(c, DeliverableQuery{}, "", 10, time.Now().Add(5*time.Second), 0, nil)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	releaseB()
	releaseB() // a second release changes nothing
	if err := <-done; err != nil {
		t.Fatalf("the waiter after a release: %v", err)
	}
	releaseA()
	if rows := listDeliverables(t, a, DeliverableQuery{}, 10); len(rows) != 1 {
		t.Fatalf("index a: %d rows", len(rows))
	}
	r := deliverRegistry
	r.mu.Lock()
	leases := ta.leases
	r.mu.Unlock()
	if leases != 0 {
		t.Fatalf("leases after release %d", leases)
	}
	registryState(t)
}

// Many readers of one index share one tracker at once, under a bound of one,
// and none of them is refused for room.
func TestReadersOfOneIndexShareItsTracker(t *testing.T) {
	withIndexMax(t, 1)
	index, _ := indexWith(t, 6, 3)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				page, err := ListDeliverables(index, DeliverableQuery{}, "", 10, time.Now().Add(5*time.Second), 0, nil)
				if errors.Is(err, ErrMediaBusy) {
					continue
				}
				if err == nil && len(page.Rows) != 3 {
					err = errors.New("wrong rows")
				}
				errs <- err
				return
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if live, idle := registryState(t); live != 1 || idle != 1 || deliverTracked.Load() != 3 {
		t.Fatalf("live %d idle %d counted %d", live, idle, deliverTracked.Load())
	}
}

// An idle tracker holds no line head: a row half written when a reader left is
// read again from its start, whole, once it ends.
func TestAnIdleTrackerHoldsNoLineHead(t *testing.T) {
	withIndexMax(t, 4)
	index, ids := indexWith(t, 2, 0)
	row, _ := FindDeliverableAfter(t, index, ids[0])
	scratch := filepath.Join(t.TempDir(), "artifacts.jsonl")
	RecordArtifact(scratch, Artifact{Path: row.Artifact.Path, Session: row.Artifact.Session, Title: row.Artifact.Title, Kind: row.Artifact.Kind, Created: row.Artifact.Created, Deliver: &DeliverableFact{ID: row.ID, Update: true, Removed: true}})
	line, _ := os.ReadFile(scratch)
	f, err := os.OpenFile(index, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write(line[:len(line)/2])
	listDeliverables(t, index, DeliverableQuery{}, 10)
	tracker := peekTracker(index)
	tracker.mu.RLock()
	carry := tracker.scan.carry
	tracker.mu.RUnlock()
	if carry != nil {
		t.Fatalf("an idle tracker holds %d bytes of a line head", len(carry))
	}
	f.Write(line[len(line)/2:])
	for _, r := range listDeliverables(t, index, DeliverableQuery{}, 10) {
		if r.ID == ids[0] {
			t.Fatal("the removal finished after the reader left was lost")
		}
	}
}

// A path longer than any tracker is kept under is refused, and nothing is
// registered for it.
func TestAnIndexPathPastTheKeyBoundIsRefused(t *testing.T) {
	withIndexMax(t, 4)
	long := "/" + strings.Repeat("p", deliverIndexKeyMax)
	if _, _, err := leaseTracker(long, time.Time{}); !errors.Is(err, ErrDeliverIndexPath) {
		t.Fatalf("long key: %v", err)
	}
	if live, _ := registryState(t); live != 0 {
		t.Fatalf("%d trackers registered", live)
	}
}
