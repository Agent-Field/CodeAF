package session

// THE SOURCE'S MEMORY IS BOUNDED. Every bound here is an ESTIMATE the cache
// budgets with and a count it enforces; no claim is made about the process's
// resident size.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workgraph"
)

func wgOwners(t *testing.T, n, each int) []string { t.Helper(); return scaleFolders(t, n, each) }

func wgOwnerOf(dir string) string { return filepath.Base(dir) }

func TestManyOwnersStayWithinTheByteBudgetAndEvictLeastRecentlyUsed(t *testing.T) {
	dirs := wgOwners(t, 40, 30)
	probe := NewWorkSource()
	first, err := probe.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	budget := first.Bytes * 5 // five conversations fit
	source := NewWorkSourceLimits(WorkLimits{CacheBytes: budget, CacheOwners: 1000})
	ctx := context.Background()
	for _, dir := range dirs {
		if _, err := source.Owner(ctx, wgOwnerOf(dir), dir); err != nil {
			t.Fatal(err)
		}
		if _, held, _, _ := source.Held(); held > budget {
			t.Fatalf("holds %d bytes of a %d budget", held, budget)
		}
	}
	n, held, _, _ := source.Held()
	evicted, _ := source.Evictions()
	if n > 5 || held > budget || evicted < 30 {
		t.Fatalf("held %d conversations, %d bytes (budget %d), evicted %d", n, held, budget, evicted)
	}
	// The newest are still in memory: asking again costs no parse.
	before := source.Parses()
	if _, err := source.Owner(ctx, wgOwnerOf(dirs[39]), dirs[39]); err != nil || source.Parses() != before {
		t.Fatalf("the most recent conversation was parsed again (%v)", err)
	}
	// The oldest was evicted and is read again, correctly.
	again, err := source.Owner(ctx, wgOwnerOf(dirs[0]), dirs[0])
	if err != nil || source.Parses() != before+1 || len(again.Facts) != 30 {
		t.Fatalf("evicted conversation: %d facts, parses %d -> %d, %v", len(again.Facts), before, source.Parses(), err)
	}
}

func TestTheOwnerCountIsBoundedToo(t *testing.T) {
	dirs := wgOwners(t, 20, 3)
	source := NewWorkSourceLimits(WorkLimits{CacheOwners: 4})
	for _, dir := range dirs {
		if _, err := source.Owner(context.Background(), wgOwnerOf(dir), dir); err != nil {
			t.Fatal(err)
		}
	}
	if n, _, _, _ := source.Held(); n != 4 {
		t.Fatalf("holds %d conversations, bound is 4", n)
	}
}

// A projection over half the budget is served and NOT retained: one giant
// conversation neither evicts everyone else nor stays in memory.
func TestAnOversizedProjectionIsServedButNotRetained(t *testing.T) {
	dirs := wgOwners(t, 2, 400)
	source := NewWorkSourceLimits(WorkLimits{CacheBytes: 64 << 10})
	facts, err := source.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0])
	if err != nil || len(facts.Facts) != 400 || facts.Bytes <= 32<<10 {
		t.Fatalf("served %d facts of %d bytes, %v", len(facts.Facts), facts.Bytes, err)
	}
	if n, held, _, _ := source.Held(); n != 0 || held != 0 {
		t.Fatalf("an oversized projection was retained: %d, %d", n, held)
	}
	if _, un := source.Evictions(); un != 1 {
		t.Fatalf("unretained = %d", un)
	}
}

// A checkpoint over the raw bound is not opened at all: its stat alone says
// too_large, and the conversation is empty rather than silently short.
func TestAGiantCheckpointIsNeverRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), sourceOwner)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Not valid JSON: if it were read the issue would be unreadable.
	writeFile(t, Place{Dir: dir}.Tasks(), strings.Repeat("x", 4096))
	source := NewWorkSourceLimits(WorkLimits{FileMax: 1024})
	got, err := source.Owner(context.Background(), sourceOwner, dir)
	if err != nil || got.Issue != WorkIssueTooLarge || len(got.Facts) != 0 {
		t.Fatalf("giant checkpoint: %+v %v", got, err)
	}
}

// Past the per-conversation bound the rest is left out and the conversation says
// partial; no count is silently short.
func TestFactAndWaitBoundsAreReportedPartialNotSilent(t *testing.T) {
	dirs := wgOwners(t, 1, 25)
	source := NewWorkSourceLimits(WorkLimits{Facts: 10})
	got, err := source.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0])
	if err != nil || len(got.Facts) != 10 || got.Issue != WorkIssuePartial {
		t.Fatalf("%d facts, issue %q, %v", len(got.Facts), got.Issue, err)
	}
	wide := filepath.Join(t.TempDir(), sourceOwner)
	doc := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 50}
	for n := 1; n <= 50; n++ {
		r := taskRecord{ID: uint64(n), Title: "t", Brief: "b", Acceptance: "a", State: TaskQueued}
		for d := 1; d < n; d++ {
			r.DependsOn = append(r.DependsOn, uint64(d))
		}
		doc.Nodes = append(doc.Nodes, r)
	}
	writeCheckpoint(t, Place{Dir: wide}.Tasks(), doc)
	limited := NewWorkSourceLimits(WorkLimits{Waits: 5})
	w, err := limited.Owner(context.Background(), sourceOwner, wide)
	if err != nil || w.Issue != WorkIssuePartial {
		t.Fatalf("wide: %q %v", w.Issue, err)
	}
	if f, _ := w.Find("50"); len(f.Waits) != 5 {
		t.Fatalf("task 50 keeps %d waits", len(f.Waits))
	}
}

// Many readers asking for one changed conversation share one read, and what
// they get is one immutable value they may all read at once.
func TestConcurrentReadsOfOneConversationAreCoalescedAndSafe(t *testing.T) {
	dirs := wgOwners(t, 1, 6000)
	source := NewWorkSource()
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]*OwnerFacts, 16)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := source.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0])
			if err != nil {
				t.Error(err)
				return
			}
			results[i] = got
			total := 0
			for j := range got.Facts {
				total += len(got.Facts[j].Node.Title)
			}
			if f, ok := got.Find("6000"); !ok || total == 0 || f.Node.Ref.Task != "6000" {
				t.Error("a shared value read wrongly")
			}
		}()
	}
	close(start)
	wg.Wait()
	if p := source.Parses(); p > 3 {
		t.Fatalf("16 concurrent readers caused %d parses", p)
	}
	for _, r := range results[1:] {
		if r != nil && results[0] != nil && r != results[0] {
			// Distinct only if a read finished before the next began; they must still be equal.
			if len(r.Facts) != len(results[0].Facts) {
				t.Fatal("readers got different answers")
			}
		}
	}
}

// A caller that gives up stops waiting at once, and when nobody waits the read
// is cancelled; the next caller reads normally.
func TestAWaiterThatLeavesCancelsTheReadAndTheNextCallerSucceeds(t *testing.T) {
	dirs := wgOwners(t, 1, 6000)
	source := NewWorkSource()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := source.Owner(ctx, wgOwnerOf(dirs[0]), dirs[0]); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Log("the read finished before the cancel landed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled caller kept waiting")
	}
	got, err := source.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0])
	if err != nil || len(got.Facts) != 6000 || got.Issue != "" {
		t.Fatalf("next caller: %d facts, %v", len(got.Facts), err)
	}
}

// Nothing is followed out of a conversation's folder: a linked folder is
// missing, a linked checkpoint or plan store is unreadable.
func TestTheSourceNeverFollowsALink(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "tasks.json"), `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"t","brief":"b","acceptance":"a","state":"done"}]}`)
	root := t.TempDir()
	linkedDir := filepath.Join(root, "00000000000000d1")
	if err := os.Symlink(outside, linkedDir); err != nil {
		t.Skip("no symlinks here")
	}
	source := NewWorkSource()
	if _, err := source.Owner(context.Background(), "00000000000000d1", linkedDir); err != ErrWorkOwnerMissing {
		t.Fatalf("a linked folder: %v", err)
	}
	real := filepath.Join(root, "00000000000000d2")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "tasks.json"), Place{Dir: real}.Tasks()); err != nil {
		t.Fatal(err)
	}
	got, err := source.Owner(context.Background(), "00000000000000d2", real)
	if err != nil || got.Issue != WorkIssueUnreadable || len(got.Facts) != 0 {
		t.Fatalf("a linked checkpoint: %+v %v", got, err)
	}
	if err := os.Remove(Place{Dir: real}.Tasks()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "plandb.db"), filepath.Join(real, planStoreFilename)); err != nil {
		t.Fatal(err)
	}
	got, err = source.Owner(context.Background(), "00000000000000d2", real)
	if err != nil || got.Issue != WorkIssueUnreadable {
		t.Fatalf("a linked plan store: %+v %v", got, err)
	}
	// An owner id that could walk is refused before any path is built.
	if _, err := source.Owner(context.Background(), "../x", real); err != ErrWorkOwnerMissing {
		t.Fatalf("unsafe owner: %v", err)
	}
}

// A check index larger than its bound is read from its newest end and says so;
// the remembered indexes are bounded too.
func TestACheckIndexOverItsBoundIsReadFromItsNewestEndAndSaysPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.jsonl")
	var b strings.Builder
	row := func(session string, n int) string {
		return fmt.Sprintf(`{"id":"%d","sessionId":"%s","title":"t","verdictBasis":{"kind":"run","runs":[{"command":"go test","exitCode":0}]}}`+"\n", n, session)
	}
	for n := 1; n <= 2000; n++ {
		b.WriteString(row("00000000000000e1", n))
	}
	writeFile(t, path, b.String())
	source := NewWorkSourceLimits(WorkLimits{IndexMax: 20 << 10})
	index, err := source.Checks(context.Background(), path)
	if err != nil || !index.Partial {
		t.Fatalf("index: %+v %v", index, err)
	}
	if index.Of("00000000000000e1", "2000") == nil || index.Of("00000000000000e1", "1") != nil {
		t.Fatal("the newest rows are kept and the oldest are not")
	}
	whole := NewWorkSource()
	full, _ := whole.Checks(context.Background(), path)
	if full.Partial || full.Of("00000000000000e1", "1") == nil {
		t.Fatal("an index within its bound is read whole")
	}
	if _, _, n, bytes := whole.Held(); n != 1 || bytes <= 0 {
		t.Fatalf("held indexes %d, bytes %d", n, bytes)
	}
}

func BenchmarkOwnerCacheHit(b *testing.B) {
	dirs := scaleFolders(b, 1, 1000)
	source := NewWorkSource()
	ctx := context.Background()
	if _, err := source.Owner(ctx, wgOwnerOf(dirs[0]), dirs[0]); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := source.Owner(ctx, wgOwnerOf(dirs[0]), dirs[0]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOwnerEvictingReads cycles more conversations than the budget holds,
// so every read is a parse: the cost of a bound that is too small.
func BenchmarkOwnerEvictingReads(b *testing.B) {
	dirs := scaleFolders(b, 8, 1000)
	source := NewWorkSourceLimits(WorkLimits{CacheOwners: 2})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := dirs[i%len(dirs)]
		if _, err := source.Owner(ctx, wgOwnerOf(d), d); err != nil {
			b.Fatal(err)
		}
	}
}

// MANY DISTINCT OWNERS AT ONCE: two hundred different conversations are asked for
// in the same instant. At most Flights of them are read; the rest are refused as
// busy at the door, with no goroutine queued for them, and the ones admitted all
// come back whole.
func TestManyDistinctOwnersAtOnceAreAdmittedUpToTheFlightLimit(t *testing.T) {
	dirs := wgOwners(t, 200, 40)
	const flights = 4
	source := NewWorkSourceLimits(WorkLimits{Flights: flights})
	baseline := runtime.NumGoroutine()
	var wg sync.WaitGroup
	var served, busy atomic.Int64
	start := make(chan struct{})
	for _, dir := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := source.Owner(context.Background(), wgOwnerOf(dir), dir)
			switch {
			case err == nil && len(got.Facts) == 40:
				served.Add(1)
			case errors.Is(err, ErrWorkBusy):
				busy.Add(1)
			default:
				t.Errorf("%v (%v)", err, got)
			}
		}()
	}
	close(start)
	wg.Wait()
	if served.Load() < flights || busy.Load() == 0 || served.Load()+busy.Load() != 200 {
		t.Fatalf("served %d, busy %d", served.Load(), busy.Load())
	}
	if refused := source.owners.refused.Load(); refused != busy.Load() {
		t.Fatalf("refused %d, callers told busy %d", refused, busy.Load())
	}
	time.Sleep(50 * time.Millisecond)
	if extra := runtime.NumGoroutine() - baseline; extra > 4 {
		t.Fatalf("%d goroutines left behind", extra)
	}
}

// THE RAW GATE: the weight of reads decoding at once never passes the gate, a
// read heavier than the whole runs alone, and a waiter that leaves frees its room.
func TestRawDecodesAreWeighedAndTheGateNeverOverfills(t *testing.T) {
	dirs := wgOwners(t, 12, 200)
	one := NewWorkSource().rawWeight(dirs[0])
	if one <= 0 {
		t.Fatalf("weight %d", one)
	}
	// The gate fits two decodes; twelve conversations are read, flights allowed.
	source := NewWorkSourceLimits(WorkLimits{Flights: 12, RawBytes: one * 2})
	var wg sync.WaitGroup
	for _, dir := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := source.Owner(context.Background(), wgOwnerOf(dir), dir); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if source.Parses() < 12 {
		t.Fatalf("parses %d", source.Parses())
	}
	// NO EXEMPTION: a gate smaller than one decode refuses it, typed, and reads nothing.
	tiny := NewWorkSourceLimits(WorkLimits{RawBytes: one - 1})
	before := tiny.Parses()
	if _, err := tiny.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0]); !errors.Is(err, workgraph.ErrTooLarge) || tiny.Parses() != before {
		t.Fatalf("a decode heavier than the gate: err %v, parses %d -> %d", err, before, tiny.Parses())
	}
}

// A caller waiting for room at the gate stops at its own deadline.
func TestAReadWaitingForRawRoomStopsWithItsCaller(t *testing.T) {
	dirs := wgOwners(t, 2, 50)
	room := NewWorkSource().rawWeight(dirs[0])
	source := NewWorkSourceLimits(WorkLimits{RawBytes: room})
	held, err := source.raw.Acquire(context.Background(), room)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := source.Owner(ctx, wgOwnerOf(dirs[0]), dirs[0]); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	source.raw.Release(held)
	if _, err := source.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0]); err != nil {
		t.Fatal(err)
	}
}

// The aggregate budget is a number somebody can add up: what is retained plus
// what may be in flight, with no exemption in any of them, is at most 128 MiB.
func TestTheAggregateBudgetIsAtMostOneHundredTwentyEightMiB(t *testing.T) {
	l := WorkLimits{}.withDefaults()
	retained := l.CacheBytes + l.IndexBytes + WorkSnapshotBytes
	inflight := l.RawBytes + WorkBuildBytes
	if sum := retained + inflight; sum > 128<<20 {
		t.Fatalf("retained %d MiB + in flight %d MiB = %d MiB", retained>>20, inflight>>20, sum>>20)
	}
	// A conversation that is read and kept fits its own cache share twice over,
	// and the largest file read weighs no more than the gate that admits it.
	if l.OwnerBytes*2 > l.CacheBytes || (l.FileMax+l.PlanMax)*rawInflation > l.RawBytes {
		t.Fatalf("owner quota %d, file %d + plan %d against cache %d and gate %d", l.OwnerBytes, l.FileMax, l.PlanMax, l.CacheBytes, l.RawBytes)
	}
}

// A conversation past its byte quota is cut at the quota, says partial, and
// keeps everything before the cut whole.
func TestAConversationPastItsByteQuotaIsPartialAndNotCut(t *testing.T) {
	dirs := wgOwners(t, 1, 400)
	source := NewWorkSourceLimits(WorkLimits{OwnerBytes: 100 << 10})
	got, err := source.Owner(context.Background(), wgOwnerOf(dirs[0]), dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.Issue != WorkIssuePartial || len(got.Facts) == 0 || len(got.Facts) >= 400 || got.Bytes > 100<<10+int64(len(got.Facts))*48 {
		t.Fatalf("%d facts, %d bytes, issue %q", len(got.Facts), got.Bytes, got.Issue)
	}
	if got.Weight <= got.Bytes {
		t.Fatalf("weight %d is under bytes %d", got.Weight, got.Bytes)
	}
}
