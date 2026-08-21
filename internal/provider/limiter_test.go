package provider

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// inspect reads the limiter's accounting. The tests below assert on inFlight
// directly because it is the number that silently broke: a drift of one is
// invisible in behaviour until it reaches zero, and a drift below zero disables
// admission control entirely while every call still returns exactly as before.
func inspect(l *adaptiveLimiter) (inFlight, capacity, waiting int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inFlight, l.capacity, len(l.waiters)
}

// pin holds the ceiling still so a test measures admission rather than the
// AIMD growth its own successes would earn.
func pin(l *adaptiveLimiter, capacity int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.capacity, l.successes = capacity, 0
}

// giveBack returns a slot without the AIMD adaptation, so a test can hold the
// ceiling still and measure the accounting alone.
func giveBack(l *adaptiveLimiter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releaseLocked()
}

func TestAdaptiveLimiterCutsOnRateLimitAndRecovers(t *testing.T) {
	l := newAdaptiveLimiter()
	if l.capacity != limiterCeiling {
		t.Fatalf("start capacity %d", l.capacity)
	}
	ctx := context.Background()
	_ = l.acquire(ctx)
	l.release(true, 0)
	if l.capacity != limiterCeiling/2 {
		t.Fatalf("one 429 should halve capacity: %d", l.capacity)
	}
	// A burst of 429s inside the cooldown is one signal, not many.
	_ = l.acquire(ctx)
	l.release(true, 0)
	if l.capacity != limiterCeiling/2 {
		t.Fatalf("cooldown ignored: %d", l.capacity)
	}
	// Sustained success grows capacity back one slot per stretch.
	for i := 0; i < limiterGrowthEvery; i++ {
		_ = l.acquire(ctx)
		l.release(false, 0)
	}
	if l.capacity != limiterCeiling/2+1 {
		t.Fatalf("growth after %d successes: %d", limiterGrowthEvery, l.capacity)
	}
}

func TestAdaptiveLimiterBlocksAtCapacityAndReleases(t *testing.T) {
	l := newAdaptiveLimiter()
	l.capacity = 1
	ctx := context.Background()
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan struct{})
	go func() {
		_ = l.acquire(ctx)
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatal("second acquire should block at capacity 1")
	case <-time.After(50 * time.Millisecond):
	}
	l.release(false, 0)
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("waiter never woke after release")
	}
	l.release(false, 0)

	// A cancelled waiter must not leak or deadlock the queue.
	_ = l.acquire(ctx)
	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- l.acquire(cancelCtx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled acquire should error")
	}
	l.release(false, 0)
	if err := l.acquire(ctx); err != nil {
		t.Fatal("limiter wedged after cancelled waiter")
	}
	l.release(false, 0)
}

func TestAdaptiveLimiterBalancesAcquireAndRelease(t *testing.T) {
	l := newAdaptiveLimiter()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := l.acquire(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if inFlight, _, _ := inspect(l); inFlight != 5 {
		t.Fatalf("five acquires should be five in flight: %d", inFlight)
	}
	for i := 0; i < 5; i++ {
		l.release(false, 0)
	}
	if inFlight, _, waiting := inspect(l); inFlight != 0 || waiting != 0 {
		t.Fatalf("balanced churn left inFlight=%d waiting=%d", inFlight, waiting)
	}
}

// A cancelled waiter and a release can land in either order, and the losing
// order is the one that used to corrupt the count: the waiter was signalled,
// took the cancellation branch anyway, and gave back a slot it had never been
// given. A few hundred rounds of the race, with the accounting checked between
// each, is the cheapest way to hold that shut.
func TestAdaptiveLimiterInFlightSurvivesCancellationChurn(t *testing.T) {
	l := newAdaptiveLimiter()
	l.capacity = 1
	base := context.Background()

	for round := 0; round < 300; round++ {
		// Held at one so every round is the contended case; growth from the
		// round's own successes would otherwise widen the ceiling past it.
		pin(l, 1)
		if err := l.acquire(base); err != nil {
			t.Fatal(err)
		}
		cancelCtx, cancel := context.WithCancel(base)
		var waiter sync.WaitGroup
		waiter.Add(1)
		go func() {
			defer waiter.Done()
			if err := l.acquire(cancelCtx); err == nil {
				l.release(false, 0)
			}
		}()
		go cancel()
		l.release(false, 0)
		waiter.Wait()
		cancel()

		inFlight, capacity, waiting := inspect(l)
		if inFlight != 0 || waiting != 0 {
			t.Fatalf("round %d left inFlight=%d waiting=%d", round, inFlight, waiting)
		}
		if capacity != 1 {
			t.Fatalf("round %d moved capacity to %d", round, capacity)
		}
	}

	// The point of the count: admission still admits, and still limits.
	if err := l.acquire(base); err != nil {
		t.Fatal(err)
	}
	if inFlight, _, _ := inspect(l); inFlight != 1 {
		t.Fatalf("limiter lost count of a live slot: %d", inFlight)
	}
	l.release(false, 0)
}

// The ceiling has to hold under real contention, which is the thing a negative
// count destroys: with inFlight below zero every acquire is admitted at once
// and the limiter becomes a no-op nobody notices.
func TestAdaptiveLimiterNeverExceedsCapacity(t *testing.T) {
	const capacity = 4
	l := newAdaptiveLimiter()
	l.capacity = capacity
	ctx := context.Background()

	var live, peak atomic.Int64
	var workers sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for call := 0; call < 25; call++ {
				if err := l.acquire(ctx); err != nil {
					return
				}
				now := live.Add(1)
				for {
					high := peak.Load()
					if now <= high || peak.CompareAndSwap(high, now) {
						break
					}
				}
				live.Add(-1)
				giveBack(l)
			}
		}()
	}
	workers.Wait()

	if got := peak.Load(); got > capacity {
		t.Fatalf("admitted %d at once over a ceiling of %d", got, capacity)
	}
	if inFlight, _, waiting := inspect(l); inFlight != 0 || waiting != 0 {
		t.Fatalf("churn left inFlight=%d waiting=%d", inFlight, waiting)
	}
}

// One freed slot wakes one waiter. The loop that used to be here re-tested a
// condition its own body could not change, so every release woke the whole
// queue for a single slot and all but one of them queued straight back up.
func TestAdaptiveLimiterWakesOneWaiterPerSlot(t *testing.T) {
	l := newAdaptiveLimiter()
	l.capacity = 1
	ctx := context.Background()
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}

	admitted := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			if err := l.acquire(ctx); err == nil {
				admitted <- struct{}{}
			}
		}()
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, _, waiting := inspect(l); waiting == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("three waiters never queued")
		}
		time.Sleep(time.Millisecond)
	}

	l.release(false, 0)
	select {
	case <-admitted:
	case <-time.After(time.Second):
		t.Fatal("the freed slot woke nobody")
	}
	select {
	case <-admitted:
		t.Fatal("one freed slot admitted two waiters")
	case <-time.After(50 * time.Millisecond):
	}
	if inFlight, _, waiting := inspect(l); inFlight != 1 || waiting != 2 {
		t.Fatalf("handoff left inFlight=%d waiting=%d", inFlight, waiting)
	}

	// Drain, so the goroutines end rather than leak into the next test.
	for i := 0; i < 2; i++ {
		l.release(false, 0)
		select {
		case <-admitted:
		case <-time.After(time.Second):
			t.Fatal("a queued waiter was never admitted")
		}
	}
	l.release(false, 0)
	if inFlight, _, waiting := inspect(l); inFlight != 0 || waiting != 0 {
		t.Fatalf("drain left inFlight=%d waiting=%d", inFlight, waiting)
	}
}

// TestAdaptiveLimiterFaultUnderTheLockDoesNotWedgeAcquire injects a panic into
// a real critical section and asserts the limiter is still usable after it.
//
// A nil waiter is the seam: releaseLocked closes the head of the queue, and
// close(nil) panics with l.mu held. Since panics became absorbable rather than
// fatal, a critical section that unlocked only on the success path would trade
// one crash for a process-wide freeze — every provider call aforge makes passes
// through this one lock, and a waiter it never wakes waits forever.
func TestAdaptiveLimiterFaultUnderTheLockDoesNotWedgeAcquire(t *testing.T) {
	l := newAdaptiveLimiter()
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.capacity, l.inFlight, l.successes = 2, 1, 0
		l.waiters = append(l.waiters, nil)
	}()

	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		l.release(false, 0)
		return nil
	}()
	if recovered == nil {
		t.Fatal("the poisoned waiter was meant to panic inside the critical section")
	}

	admitted := make(chan error, 1)
	go func() { admitted <- l.acquire(context.Background()) }()
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatalf("acquire after the fault: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire blocked after a panic under the limiter lock — the mutex was never given back")
	}

	// The slot the faulted release was handing over stayed counted, so the
	// admitted caller is the second of two.
	if inFlight, _, waiting := inspect(l); inFlight != 2 || waiting != 0 {
		t.Fatalf("after the fault inFlight=%d waiting=%d, want 2 and 0", inFlight, waiting)
	}
	l.release(false, 0)
	l.release(false, 0)
	if inFlight, _, _ := inspect(l); inFlight != 0 {
		t.Fatalf("release after the fault left inFlight=%d", inFlight)
	}
}

func TestRetryAfterParsesSecondsAndDate(t *testing.T) {
	response := &http.Response{Header: http.Header{}}
	if retryAfter(response) != 0 {
		t.Fatal("no header should mean zero")
	}
	response.Header.Set("Retry-After", "7")
	if got := retryAfter(response); got != 7*time.Second {
		t.Fatalf("seconds form: %v", got)
	}
	response.Header.Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(http.TimeFormat))
	if got := retryAfter(response); got < time.Second || got > 3*time.Second {
		t.Fatalf("date form: %v", got)
	}
	response.Header.Set("Retry-After", "garbage")
	if retryAfter(response) != 0 {
		t.Fatal("unparseable should mean zero")
	}
}

// atClock hands the limiter a clock the test moves by hand, so healing written
// in minutes is measured in microseconds. The pacing it describes is real time
// on a real account; nothing about the arithmetic needs to be.
func atClock(l *adaptiveLimiter, now *time.Time) {
	l.now = func() time.Time { return *now }
}

// THE RATCHET. Several aforge processes share one API key on this machine, so a
// burst of 429s a sibling caused arrives here as if this process had caused it.
// Halvings compound; the successes that undo them are earned only by traffic
// this process may not have. A cut that can never heal is a sibling's minute
// shaping every later hour, and this is the test that it heals.
func TestASiblingsBurstDoesNotRatchetCapacityDownForGood(t *testing.T) {
	l := newAdaptiveLimiter()
	clock := time.Now()
	atClock(l, &clock)
	ctx := context.Background()

	// Six windows of somebody else's traffic take the ceiling to the floor.
	for cut := 0; cut < 6; cut++ {
		if err := l.acquire(ctx); err != nil {
			t.Fatal(err)
		}
		l.release(true, 0)
		clock = clock.Add(limiterCutCooldown + time.Second)
	}
	if l.capacity != limiterFloor {
		t.Fatalf("six windows of 429s left capacity %d, want the floor %d", l.capacity, limiterFloor)
	}

	// Then quiet — and no successes to earn anything back with, because the
	// traffic was never this process's to begin with.
	clock = clock.Add(limiterHealQuiet + limiterCutCooldown)
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	giveBack(l)
	if l.capacity <= limiterFloor {
		t.Fatalf("capacity was still %d after %s of quiet — the cut never healed", l.capacity, limiterHealQuiet)
	}

	// And a process left alone long enough comes all the way back: an
	// hour-old halving says nothing about now.
	clock = clock.Add(time.Hour)
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	giveBack(l)
	if l.capacity != limiterCeiling {
		t.Fatalf("an idle hour left capacity at %d, want the ceiling %d", l.capacity, limiterCeiling)
	}
}

// ONE WINDOW, ONE HALVING. A 429 that names a Retry-After has described how long
// this account is paced for; every 429 inside that window is the same fact
// restated by a request that was already in the air, and cutting again for each
// of them is how one burst becomes six halvings.
func TestARetryAfterNamesTheWindowAndTheBurstInsideItCutsOnce(t *testing.T) {
	l := newAdaptiveLimiter()
	base := time.Now()
	clock := base
	atClock(l, &clock)
	ctx := context.Background()

	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	l.release(true, 30*time.Second)
	want := limiterCeiling / 2
	if l.capacity != want {
		t.Fatalf("the first 429 left capacity %d, want %d", l.capacity, want)
	}

	// The rest of the burst, well past the two-second floor that would
	// otherwise have let every one of them cut again.
	for _, at := range []time.Duration{3 * time.Second, 10 * time.Second, 29 * time.Second} {
		clock = base.Add(at)
		if err := l.acquire(ctx); err != nil {
			t.Fatal(err)
		}
		l.release(true, 0)
	}
	if l.capacity != want {
		t.Fatalf("a burst inside the named window cut capacity to %d, want the single halving %d", l.capacity, want)
	}

	// And the way back out survives the burst too: a suppressed 429 used to
	// zero the growth counter anyway, so the halving was ignored once and the
	// recovery was thrown away four times.
	for success := 0; success < limiterGrowthEvery-1; success++ {
		if err := l.acquire(ctx); err != nil {
			t.Fatal(err)
		}
		l.release(false, 0)
	}
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	l.release(true, 0)
	if err := l.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	l.release(false, 0)
	if l.capacity != want+1 {
		t.Fatalf("capacity %d after %d successes across one suppressed 429, want %d",
			l.capacity, limiterGrowthEvery, want+1)
	}
}
