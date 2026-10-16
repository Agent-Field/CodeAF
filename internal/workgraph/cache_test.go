package workgraph

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func smallInput(n int) Builder {
	return func(context.Context) (Input, error) { return scaleInput(1, n), nil }
}

func scopeN(i int) GraphScope { return GraphScope{Kind: ScopeCrew, ID: fmt.Sprintf("crew%d", i)} }

// THE ESTIMATE IS A CEILING, NOT A GUESS: what the snapshot really keeps alive
// (measured after its input is released and the heap collected) is below
// Bytes(), and Bytes() is not absurdly above it — a budget that counts ten times
// what is held would waste the cache it protects.
func TestSnapshotBytesIsAConservativeCeilingOnTheRealHeap(t *testing.T) {
	for _, n := range []int{2_000, 20_000} {
		before := heapNow()
		in := scaleInput(10, n/10)
		snap, err := Build(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		in = Input{}
		real := int64(heapNow()) - int64(before)
		est := snap.Bytes()
		runtime.KeepAlive(snap)
		t.Logf("%d tasks: held %d KiB, estimate %d KiB (x%.2f)", n, real/1024, est/1024, float64(est)/float64(real))
		if est < real {
			t.Fatalf("%d tasks: estimate %d below the %d bytes really held", n, est, real)
		}
		if est > real*4 {
			t.Fatalf("%d tasks: estimate %d is over four times the %d held", n, est, real)
		}
	}
}

// A snapshot keeps nothing of its input alive: not the facts slice, not what the
// resolver closure captured. The input carries 16 MiB that only those hold.
func TestASnapshotRetainsNothingOfItsInput(t *testing.T) {
	base := heapNow()
	ballast := make([]byte, 16<<20)
	in := scaleInput(2, 50)
	in.Resolve = func(TaskRef) Resolution { return Resolution{State: fmt.Sprint(len(ballast))} }
	snap, err := Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in = Input{}
	ballast = nil
	held := int64(heapNow()) - int64(base)
	runtime.KeepAlive(snap)
	if held > 4<<20 {
		t.Fatalf("a 100-task snapshot keeps %d bytes alive after its input was released", held)
	}
}

func TestACacheHoldsToItsByteBudgetAndEvictsOldestFirst(t *testing.T) {
	probe, err := Build(context.Background(), scaleInput(1, 300))
	if err != nil {
		t.Fatal(err)
	}
	one := probe.Bytes()
	c := NewCache(CacheLimits{Bytes: one*5/2 + 1, Entries: 100, Pending: 4, BuildBytes: 1 << 20})
	for i := 0; i < 6; i++ {
		if _, err := c.Get(context.Background(), scopeN(i), "r", smallInput(300)); err != nil {
			t.Fatal(err)
		}
		if entries, held := c.Held(); held > one*5/2+1 || entries > 2 {
			t.Fatalf("after %d: %d entries, %d bytes over a %d budget", i, entries, held, one*5/2+1)
		}
	}
	// The newest are the ones kept.
	builds, _ := c.Stats()
	if _, err := c.Get(context.Background(), scopeN(5), "r", smallInput(300)); err != nil {
		t.Fatal(err)
	}
	if after, hits := c.Stats(); after != builds || hits != 1 {
		t.Fatalf("the newest snapshot was evicted: builds %d→%d hits %d", builds, after, hits)
	}
}

// NO EXEMPTION: a snapshot over the whole budget is served in full to everyone
// who asked, and never kept — not even alone, not even as the newest.
func TestASnapshotOverTheBudgetIsServedWholeAndNeverKept(t *testing.T) {
	c := NewCache(CacheLimits{Bytes: 1 << 10, Entries: 10, Pending: 4, BuildBytes: 1 << 20})
	for i := 0; i < 3; i++ {
		snap, err := c.Get(context.Background(), scopeN(1), "r", smallInput(500))
		if err != nil {
			t.Fatal(err)
		}
		if snap.Len() != 500 || snap.Bytes() <= 1<<10 {
			t.Fatalf("served %d nodes at %d bytes: truncated or under-counted", snap.Len(), snap.Bytes())
		}
	}
	if entries, held := c.Held(); entries != 0 || held != 0 {
		t.Fatalf("held %d entries, %d bytes of an over-budget snapshot", entries, held)
	}
	if builds, _ := c.Stats(); builds != 3 {
		t.Fatalf("builds %d: an uncached snapshot is rebuilt each time", builds)
	}
	if _, unstored := c.Refusals(); unstored != 3 {
		t.Fatalf("unstored %d", unstored)
	}
}

func TestTheEntryCountCapsSmallSnapshotsToo(t *testing.T) {
	c := NewCache(CacheLimits{Bytes: 1 << 30, Entries: 3, Pending: 4, BuildBytes: 1 << 20})
	for i := 0; i < 10; i++ {
		if _, err := c.Get(context.Background(), scopeN(i), "r", smallInput(5)); err != nil {
			t.Fatal(err)
		}
	}
	if entries, _ := c.Held(); entries != 3 {
		t.Fatalf("entries %d", entries)
	}
}

// MANY DISTINCT CONCURRENT KEYS: five hundred callers each ask for a different
// scope while every build is stuck. Only Pending of them are admitted, the rest
// are refused at once as busy, the goroutines spent are bounded by Pending and not
// by callers, and the weight running never passes the gate.
func TestManyDistinctKeysAreRefusedAtTheDoorAndSpendBoundedGoroutines(t *testing.T) {
	const callers, pending = 500, 4
	c := NewCache(CacheLimits{Bytes: 1 << 30, Entries: 100, Pending: pending, BuildBytes: 100})
	release := make(chan struct{})
	var running, peak atomic.Int64
	build := stuckBuild(release, &running, &peak)
	baseline := runtime.NumGoroutine()
	var wg sync.WaitGroup
	var ok, busy atomic.Int64
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := c.GetWeighted(context.Background(), scopeN(i), "r", 60, build)
			countAnswer(t, err, &ok, &busy)
		}(i)
	}
	waitFor(5*time.Second, func() bool { return busy.Load() >= callers-pending })
	if got := c.Pending(); got != pending {
		t.Fatalf("pending %d, want exactly %d", got, pending)
	}
	// 4 pending builds of weight 60 against a gate of 100: one runs, three queue.
	if r := running.Load(); r != 1 {
		t.Fatalf("%d builds running against a gate that fits one", r)
	}
	if extra := runtime.NumGoroutine() - baseline; extra > callers-int(busy.Load())+pending+4 {
		t.Fatalf("%d extra goroutines for %d pending builds", extra, pending)
	}
	close(release)
	wg.Wait()
	if ok.Load() != pending || busy.Load() != callers-pending || peak.Load() != 1 {
		t.Fatalf("served %d refused %d peak %d", ok.Load(), busy.Load(), peak.Load())
	}
}

// stuckBuild blocks until release closes, counting how many run at once.
func stuckBuild(release chan struct{}, running, peak *atomic.Int64) Builder {
	return func(ctx context.Context) (Input, error) {
		n := running.Add(1)
		defer running.Add(-1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		select {
		case <-release:
		case <-ctx.Done():
			return Input{}, ctx.Err()
		}
		return scaleInput(1, 3), nil
	}
}

func countAnswer(t *testing.T, err error, ok, busy *atomic.Int64) {
	switch {
	case err == nil:
		ok.Add(1)
	case errors.Is(err, ErrBusy):
		busy.Add(1)
	default:
		t.Error(err)
	}
}

func waitFor(d time.Duration, done func() bool) {
	for end := time.Now().Add(d); !done() && time.Now().Before(end); {
		time.Sleep(time.Millisecond)
	}
}

// Callers for the SAME key are not distinct builds: a thousand of them are one
// build and none is refused.
func TestOneKeyIsOneBuildWhoeverAsks(t *testing.T) {
	c := NewCache(CacheLimits{Bytes: 1 << 30, Entries: 8, Pending: 1, BuildBytes: 1 << 20})
	release := make(chan struct{})
	var builds atomic.Int64
	build := func(context.Context) (Input, error) { builds.Add(1); <-release; return scaleInput(1, 3), nil }
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Get(context.Background(), scopeN(1), "r", build); err != nil {
				t.Error(err)
			}
		}()
	}
	for c.Pending() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if builds.Load() != 1 {
		t.Fatalf("%d builds for one key", builds.Load())
	}
}

// A build queued at the gate that nobody waits for any more leaves the queue and
// frees its pending slot; it neither runs nor holds weight.
func TestAQueuedBuildNobodyWaitsForLeavesTheGate(t *testing.T) {
	c := NewCache(CacheLimits{Bytes: 1 << 30, Entries: 8, Pending: 4, BuildBytes: 10})
	hold := make(chan struct{})
	go c.GetWeighted(context.Background(), scopeN(1), "r", 10, func(ctx context.Context) (Input, error) {
		<-hold
		return scaleInput(1, 2), nil
	})
	for c.Pending() == 0 {
		time.Sleep(time.Millisecond)
	}
	var ran atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := c.GetWeighted(ctx, scopeN(2), "r", 10, func(context.Context) (Input, error) { ran.Store(true); return scaleInput(1, 2), nil })
		errc <- err
	}()
	for c.Pending() < 2 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for c.Pending() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.Pending() != 1 {
		t.Fatalf("pending %d: the abandoned build still holds a slot", c.Pending())
	}
	close(hold)
	time.Sleep(20 * time.Millisecond)
	if ran.Load() {
		t.Fatal("an abandoned queued build ran")
	}
}

func BenchmarkCacheHit(b *testing.B) {
	c := NewCache(CacheLimits{Bytes: 1 << 30, Entries: 8, Pending: 4, BuildBytes: 1 << 20})
	if _, err := c.Get(context.Background(), scopeN(1), "r", smallInput(1000)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Get(context.Background(), scopeN(1), "r", nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkManyDistinctKeysAgainstTheDoor(b *testing.B) {
	c := NewCache(CacheLimits{Bytes: 1 << 20, Entries: 8, Pending: 2, BuildBytes: 1 << 20})
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			_, _ = c.Get(context.Background(), scopeN(i%64), "r", smallInput(20))
		}
	})
}
