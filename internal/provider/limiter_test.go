package provider

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestAdaptiveLimiterCutsOnRateLimitAndRecovers(t *testing.T) {
	l := newAdaptiveLimiter()
	if l.capacity != limiterCeiling {
		t.Fatalf("start capacity %d", l.capacity)
	}
	ctx := context.Background()
	_ = l.acquire(ctx)
	l.release(true)
	if l.capacity != limiterCeiling/2 {
		t.Fatalf("one 429 should halve capacity: %d", l.capacity)
	}
	// A burst of 429s inside the cooldown is one signal, not many.
	_ = l.acquire(ctx)
	l.release(true)
	if l.capacity != limiterCeiling/2 {
		t.Fatalf("cooldown ignored: %d", l.capacity)
	}
	// Sustained success grows capacity back one slot per stretch.
	for i := 0; i < limiterGrowthEvery; i++ {
		_ = l.acquire(ctx)
		l.release(false)
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
	l.release(false)
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("waiter never woke after release")
	}
	l.release(false)

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
	l.release(false)
	if err := l.acquire(ctx); err != nil {
		t.Fatal("limiter wedged after cancelled waiter")
	}
	l.release(false)
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
