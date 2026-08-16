package aimdlimiter

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// src/router/aimd-limiter.ts ships without a .test.ts, so there is nothing to
// translate verbatim. These tests instead cover the one axis the golden
// fixtures structurally cannot reach: the TS gets its mutual exclusion for free
// from the event loop, while this port has to earn it with a mutex. Run them
// under -race.
//
// Validation contract:
//   - A caller that cannot get a slot stays blocked until one is freed.
//   - Blocked callers are woken strictly FIFO.
//   - No number of concurrent callers can push inflight past the cap.
//   - Counters stay exact under concurrent acquire/release churn.
//   - GetOpenRouterLimiter constructs at most one limiter, however many
//     goroutines race the first call.
//   - A panicking onChange never escapes the resize call.
//   - An onChange that re-enters the limiter completes instead of deadlocking
//     (the TS permits it, so the port must too).

func capPtr(v float64) *float64 { return &v }

func isPending(t *testing.T, ch <-chan struct{}) bool {
	t.Helper()
	select {
	case <-ch:
		return false
	default:
		return true
	}
}

func TestAcquireBlocksUntilASlotIsFreed(t *testing.T) {
	limiter := NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(1), MinCap: capPtr(1), MaxCap: capPtr(1),
	})

	first := limiter.Acquire()
	if isPending(t, first) {
		t.Fatal("first acquire under the cap should be granted outright")
	}
	second := limiter.Acquire()
	if !isPending(t, second) {
		t.Fatal("second acquire at the cap should be queued, not granted")
	}

	resumed := make(chan struct{})
	go func() {
		<-second
		close(resumed)
	}()

	// Still nothing to wake it with.
	select {
	case <-resumed:
		t.Fatal("waiter resumed before any slot was released")
	case <-time.After(20 * time.Millisecond):
	}

	limiter.Release()
	select {
	case <-resumed:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter never resumed after release")
	}
	if got := limiter.Inspect(); got.Inflight != 1 || got.Waiters != 0 {
		t.Fatalf("after handoff want inflight 1 waiters 0, got %+v", got)
	}
}

func TestWaitersWakeInFIFOOrder(t *testing.T) {
	limiter := NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(1), MinCap: capPtr(1), MaxCap: capPtr(1),
	})
	<-limiter.Acquire()

	const waiters = 8
	queued := make([]<-chan struct{}, waiters)
	for i := range queued {
		queued[i] = limiter.Acquire()
	}

	// Each release hands the slot to exactly one waiter, in enqueue order.
	for i := 0; i < waiters; i++ {
		limiter.Release()
		for j, ch := range queued {
			pending := isPending(t, ch)
			if j <= i && pending {
				t.Fatalf("release %d: waiter %d should have been woken", i, j)
			}
			if j > i && !pending {
				t.Fatalf("release %d: waiter %d jumped the queue", i, j)
			}
		}
	}
}

func TestConcurrentTrafficNeverExceedsTheCap(t *testing.T) {
	const slots = 4
	limiter := NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(slots), MinCap: capPtr(slots), MaxCap: capPtr(slots),
	})

	var held, peak int64
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				<-limiter.Acquire()
				now := atomic.AddInt64(&held, 1)
				for {
					was := atomic.LoadInt64(&peak)
					if now <= was || atomic.CompareAndSwapInt64(&peak, was, now) {
						break
					}
				}
				atomic.AddInt64(&held, -1)
				limiter.Release()
			}
		}()
	}
	wg.Wait()

	if peak > slots {
		t.Fatalf("observed %d concurrent holders with a cap of %d", peak, slots)
	}
	if got := limiter.Inspect(); got.Inflight != 0 || got.Waiters != 0 {
		t.Fatalf("counters did not settle back to zero: %+v", got)
	}
}

func TestConcurrentResizesKeepTheCapInBounds(t *testing.T) {
	limiter := NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(8), MinCap: capPtr(2), MaxCap: capPtr(16),
	})

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				switch (seed + j) % 3 {
				case 0:
					limiter.OnSuccess()
				case 1:
					limiter.OnThrottle()
				default:
					limiter.OnLowRemaining()
				}
			}
		}(i)
	}
	wg.Wait()

	got := float64(limiter.CurrentCap())
	if math.IsNaN(got) || got < 2 || got > 16 {
		t.Fatalf("cap escaped [2, 16]: %v", got)
	}
}

func TestGetOpenRouterLimiterConstructsExactlyOne(t *testing.T) {
	SetOpenRouterLimiterForTest(nil)
	defer SetOpenRouterLimiterForTest(nil)

	const racers = 32
	seen := make([]*AIMDConcurrencyLimiter, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			<-start
			seen[slot] = GetOpenRouterLimiter()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, limiter := range seen {
		if limiter != seen[0] {
			t.Fatalf("goroutine %d got a different limiter instance", i)
		}
	}
	if got := seen[0].Inspect(); got.Cap != 8 || got.Min != 1 || got.Max != 64 {
		t.Fatalf("singleton defaults drifted: %+v", got)
	}
}

func TestSetOpenRouterLimiterForTestSwapsTheInstance(t *testing.T) {
	SetOpenRouterLimiterForTest(nil)
	defer SetOpenRouterLimiterForTest(nil)

	original := GetOpenRouterLimiter()
	custom := NewAIMDConcurrencyLimiter(AIMDOptions{InitialCap: capPtr(3), MaxCap: capPtr(3)})
	SetOpenRouterLimiterForTest(custom)
	if GetOpenRouterLimiter() != custom {
		t.Fatal("setOpenRouterLimiterForTest did not install the supplied limiter")
	}

	SetOpenRouterLimiterForTest(nil)
	fresh := GetOpenRouterLimiter()
	if fresh == original || fresh == custom {
		t.Fatal("clearing the seam should build a brand-new limiter")
	}
	if got := fresh.Inspect(); got.Cap != 8 {
		t.Fatalf("fresh singleton should start at cap 8, got %+v", got)
	}
}

func TestPanickingOnChangeIsSwallowed(t *testing.T) {
	calls := 0
	limiter := NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(4), MinCap: capPtr(1), MaxCap: capPtr(8),
		OnChange: func(ConcurrencyChange) {
			calls++
			panic("telemetry exploded")
		},
	})

	limiter.OnSuccess()
	limiter.OnThrottle()
	limiter.OnLowRemaining()

	if calls != 3 {
		t.Fatalf("want 3 telemetry calls, got %d", calls)
	}
	if got := limiter.CurrentCap(); got != 1 {
		t.Fatalf("resizes should still have landed; cap is %v", got)
	}
}

func TestReentrantOnChangeDoesNotDeadlock(t *testing.T) {
	var limiter *AIMDConcurrencyLimiter
	observed := make(chan Inspection, 1)
	limiter = NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(2), MinCap: capPtr(1), MaxCap: capPtr(8),
		OnChange: func(ConcurrencyChange) {
			// The TS calls onChange on the same stack with no lock held, so a
			// callback may touch the limiter. Firing it under the mutex would
			// hang here instead.
			observed <- limiter.Inspect()
		},
	})

	done := make(chan struct{})
	go func() {
		limiter.OnSuccess()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("re-entrant onChange deadlocked")
	}
	if got := <-observed; got.Cap != 3 {
		t.Fatalf("callback should observe the already-applied cap, got %+v", got)
	}
}

func TestWedgedNaNCapNeverGrantsASlot(t *testing.T) {
	changes := 0
	limiter := NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(math.NaN()), MinCap: capPtr(1), MaxCap: capPtr(64),
		OnChange: func(ConcurrencyChange) { changes++ },
	})

	first := limiter.Acquire()
	if !isPending(t, first) {
		t.Fatal("a NaN cap must fail `inflight < cap` and queue every acquire")
	}
	limiter.Release()
	if !isPending(t, first) {
		t.Fatal("a NaN cap must fail the drain guard too")
	}
	limiter.OnSuccess()
	limiter.OnThrottle()
	limiter.OnLowRemaining()
	// NaN !== NaN, so resize's short-circuit is unreachable and every call
	// reports a no-op change. This is the TS behaviour, kept on purpose.
	if changes != 3 {
		t.Fatalf("want 3 no-op changes from a wedged cap, got %d", changes)
	}
	if got := limiter.Inspect(); got.Waiters != 1 || got.Inflight != 0 {
		t.Fatalf("waiter should still be stuck: %+v", got)
	}
}

func TestNaNRecoveryWithFixFlag(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_AIMD_NAN_CAP", "1")

	changes := 0
	limiter := NewAIMDConcurrencyLimiter(AIMDOptions{
		InitialCap: capPtr(math.NaN()), MinCap: capPtr(1), MaxCap: capPtr(64),
		OnChange: func(ConcurrencyChange) { changes++ },
	})

	// Initially wedged: NaN cap means no acquire can succeed.
	first := limiter.Acquire()
	if !isPending(t, first) {
		t.Fatal("a NaN cap must fail `inflight < cap` and queue every acquire")
	}

	// OnSuccess triggers resizeLocked, which with the fix clamps NaN to
	// minCap (1), drains waiters, and reports a single change.
	limiter.OnSuccess()

	if isPending(t, first) {
		t.Fatal("after NaN recovery, the queued acquire should have been granted")
	}
	if got := float64(limiter.CurrentCap()); got != 1 {
		t.Fatalf("after NaN recovery, cap should be minCap (1), got %v", got)
	}
	if changes != 1 {
		t.Fatalf("want 1 change from NaN recovery, got %d", changes)
	}
}
