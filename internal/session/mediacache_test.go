package session

import (
	"container/list"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func resetMediaCache() {
	mediaCache.mu.Lock()
	mediaCache.items = map[mediaCacheKey]*list.Element{}
	mediaCache.order.Init()
	mediaCache.bytes = 0
	mediaCache.mu.Unlock()
}

// listAll pages a session's whole listing and answers its ids newest first.
func listAll(t *testing.T, index, sessionID string) []string {
	t.Helper()
	var ids []string
	cursor := ""
	for {
		scan, err := ListMedia(index, sessionID, cursor, MediaPageMax, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range scan.Rows {
			ids = append(ids, row.Fact.ID)
		}
		if scan.Next == "" {
			return ids
		}
		cursor = scan.Next
	}
}

// After a listing of two hundred sources, getting each of the two hundred reads
// the index zero times: the listing left the records, and each hit is checked
// against the file with one small read.
func TestGetAfterListDoesNotRescanTheIndex(t *testing.T) {
	resetMediaCache()
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	sparseIndex(t, index, dir, 0, 0, 200)
	ids := listAll(t, index, "s1")
	if len(ids) != 200 {
		t.Fatalf("listed %d", len(ids))
	}
	before := MediaIndexScans()
	for _, id := range ids {
		row, err := FindMedia(index, "s1", id, time.Time{})
		if err != nil || row.Fact.ID != id {
			t.Fatalf("get %s = %v", id, err)
		}
	}
	if scans := MediaIndexScans() - before; scans != 0 {
		t.Fatalf("200 gets after a list ran %d index scans, want 0", scans)
	}
	// Another conversation's id is not served from the cache.
	if _, err := FindMedia(index, "s2", ids[0], time.Now().Add(5*time.Second)); err != ErrMediaNotFound {
		t.Fatalf("foreign get answered %v", err)
	}
}

func TestColdGetScansOnceThenIsWarm(t *testing.T) {
	resetMediaCache()
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 1, 50, 5)
	before := MediaIndexScans()
	if _, err := FindMedia(index, "s1", ids[0], time.Time{}); err != nil {
		t.Fatal(err)
	}
	if MediaIndexScans()-before != 1 {
		t.Fatal("a cold get is an index scan, and says so")
	}
	// The scan left every newer record of the session behind it.
	for _, id := range ids {
		if _, err := FindMedia(index, "s1", id, time.Time{}); err != nil {
			t.Fatal(err)
		}
	}
	if MediaIndexScans()-before != 1 {
		t.Fatalf("warm gets rescanned: %d scans", MediaIndexScans()-before)
	}
}

func TestMediaCacheIsBoundedAndCopiesRecords(t *testing.T) {
	resetMediaCache()
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "p.png")
	for i := 0; i < mediaCacheRecords+300; i++ {
		RecordMedia(index, Artifact{Path: path, Session: "s1", Title: "t", Kind: "image", Created: time.Now()},
			GeneratedMedia(path, []byte{byte(i)}, "m").WithReferences([]string{"a", "b"}))
	}
	listAll(t, index, "s1")
	mediaCache.mu.Lock()
	records, size := mediaCache.order.Len(), mediaCache.bytes
	mediaCache.mu.Unlock()
	if records > mediaCacheRecords || size > mediaCacheBytes {
		t.Fatalf("cache holds %d records, %d bytes", records, size)
	}
	// What a caller does to its copy is not seen by the next one.
	id := listAll(t, index, "s1")[0]
	first, _ := FindMedia(index, "s1", id, time.Time{})
	first.Fact.References[0] = "tampered"
	first.Fact.Model = "tampered"
	again, _ := FindMedia(index, "s1", id, time.Time{})
	if again.Fact.References[0] != "a" || again.Fact.Model == "tampered" && again.Fact.References[0] != "a" {
		t.Fatalf("a cached record was changed through a copy: %+v", again.Fact.References)
	}
}

func TestMediaCacheHitIsValidatedAgainstTheIndex(t *testing.T) {
	resetMediaCache()
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 0, 0, 3)
	listAll(t, index, "s1")
	// Shortened in place (same inode, fewer bytes): the entry must not survive.
	if err := os.Truncate(index, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := FindMedia(index, "s1", ids[2], time.Now().Add(5*time.Second)); err != ErrMediaNotFound {
		t.Fatalf("a record cut out of the index was still served: %v", err)
	}
	// Replaced by another file holding other rows.
	resetMediaCache()
	index2 := filepath.Join(dir, "second.jsonl")
	other := sparseIndex(t, index2, dir, 0, 0, 2)
	listAll(t, index2, "s1")
	if err := os.Remove(index2); err != nil {
		t.Fatal(err)
	}
	sparseIndex(t, index2, dir, 0, 0, 2)
	if _, err := FindMedia(index2, "s1", other[0], time.Now().Add(5*time.Second)); err != ErrMediaNotFound {
		t.Fatalf("a record of a replaced index was still served: %v", err)
	}
}

// Two hundred cold gets at once never run more than mediaScanSlots scans; the
// rest are told busy; whoever is answered is answered right.
func TestColdGetsAreBoundedAndShareAScan(t *testing.T) {
	resetMediaCache()
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	ids := sparseIndex(t, index, dir, 200, 3000, 0)
	mediaScansPeak.Store(0)
	before := MediaIndexScans()
	var wg sync.WaitGroup
	var mu sync.Mutex
	answered, busy := 0, 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			row, err := FindMedia(index, "s1", id, time.Now().Add(20*time.Second))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == ErrMediaBusy:
				busy++
			case err != nil || row.Fact.ID != id:
				t.Errorf("get %s = %v", id, err)
			default:
				answered++
			}
		}(ids[i])
	}
	wg.Wait()
	if peak := mediaScansPeak.Load(); peak > mediaScanSlots {
		t.Fatalf("%d scans ran together, bound is %d", peak, mediaScanSlots)
	}
	if answered == 0 {
		t.Fatal("nobody was answered")
	}
	t.Logf("answered %d, busy %d, scans %d", answered, busy, MediaIndexScans()-before)
	// Same record asked by many at once: one scan serves them.
	resetMediaCache()
	before = MediaIndexScans()
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := FindMedia(index, "s1", ids[0], time.Now().Add(20*time.Second)); err != nil && err != ErrMediaBusy {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if scans := MediaIndexScans() - before; scans > 3 {
		t.Fatalf("50 askers of one record ran %d scans", scans)
	}
}

// bigIndex writes n rows, one in `every` of them the session's.
func bigIndex(tb testing.TB, dir string, n, every int) (string, []string) {
	tb.Helper()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := filepath.Join(dir, "p.png")
	_ = os.WriteFile(path, []byte("x"), 0o644)
	var ids []string
	f, err := os.Create(index)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	for i := 0; i < n; i++ {
		session := fmt.Sprintf("other%04d", i%5000)
		if i%every == 0 {
			session = "s1"
		}
		fact := MediaFact{ID: NewMediaID(), Origin: MediaGenerated, Canonical: path, MIME: "image/png", Bytes: 1, ModNS: 1, Model: "m"}
		row, _ := mediaRowJSON(Artifact{Path: path, Session: session, Title: "t", Kind: "image", Created: time.Unix(int64(i), 0), Media: &fact})
		_, _ = f.Write(append(row, '\n'))
		if session == "s1" {
			ids = append(ids, fact.ID)
		}
	}
	return index, ids
}

// BenchmarkFindMedia100kIndex: a cold lookup is an honest O(index) scan of a
// 100,000-row global index; after a listing the same lookups cost one small
// validating read each.
func BenchmarkFindMedia100kIndex(b *testing.B) {
	dir := b.TempDir()
	index, ids := bigIndex(b, dir, 100000, 500) // 200 rows for the session
	oldest := ids[0]
	b.Run("cold-oldest", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resetMediaCache()
			if _, err := FindMedia(index, "s1", oldest, time.Time{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("warm-after-list", func(b *testing.B) {
		resetMediaCache()
		for cursor := ""; ; {
			scan, err := ListMedia(index, "s1", cursor, MediaPageMax, time.Time{})
			if err != nil {
				b.Fatal(err)
			}
			if scan.Next == "" {
				break
			}
			cursor = scan.Next
		}
		before := MediaIndexScans()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := FindMedia(index, "s1", ids[i%len(ids)], time.Time{}); err != nil {
				b.Fatal(err)
			}
		}
		if MediaIndexScans() != before {
			b.Fatal("a warm get scanned the index")
		}
	})
}

func mediaRowJSON(row Artifact) ([]byte, error) { return json.Marshal(row) }

func TestMediaCacheDoesNotLendOutTheBatchReceipt(t *testing.T) {
	resetMediaCache()
	dir := t.TempDir()
	index := filepath.Join(dir, "artifacts.jsonl")
	path := mediaFile(t, dir, "p.png")
	cost := 0.17
	fact := GeneratedMedia(path, []byte("picture"), "painter").WithRequested(4).WithBatch("bat_actual", 2, 3)
	fact.Charge = MediaCharged
	fact.CostScope = MediaCostScopeCall
	fact.CallCostUSD = &cost
	RecordMedia(index, Artifact{Path: path, Session: "s1", Title: "actual variant", Kind: "image", Created: time.Now()}, fact)
	ids := listAll(t, index, "s1")
	if len(ids) != 1 {
		t.Fatalf("ids%v", ids)
	}
	first, err := FindMedia(index, "s1", ids[0], time.Time{})
	if err != nil || first.Fact.CallCostUSD == nil {
		t.Fatalf("receipt unavailable: %+v %v", first, err)
	}
	*first.Fact.CallCostUSD = 999
	again, err := FindMedia(index, "s1", ids[0], time.Time{})
	if err != nil || again.Fact.CallCostUSD == nil || *again.Fact.CallCostUSD != cost || again.Fact.CostUSD != nil {
		t.Fatalf("caller changed another view's receipt: %+v %v", again, err)
	}
}
