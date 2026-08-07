package provider

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// The concurrency doctrine: aforge sets no artificial ceiling on how much work
// runs at once — the only real limit is what the provider's rate limiting
// permits, and the provider tells us when we cross it. This limiter is that
// signal made adaptive, TCP-style AIMD: every 429 halves the number of
// requests allowed in flight (multiplicative decrease), every stretch of
// clean successes adds one back (additive increase), so admission converges
// on whatever rate the account actually sustains — no constant to mistune.
const (
	// limiterCeiling is not a policy cap; it is a memory/socket sanity bound
	// far above any realistic account rate.
	limiterCeiling = 64
	// limiterFloor keeps at least one request moving so progress never stops.
	limiterFloor = 1
	// limiterGrowthEvery is how many consecutive successes earn one more slot.
	limiterGrowthEvery = 8
	// limiterCutCooldown ignores further 429s just after a cut: a burst of
	// rate limits from requests already in flight is one signal, not many.
	limiterCutCooldown = 2 * time.Second
)

// adaptiveLimiter is shared by every request a client sends.
type adaptiveLimiter struct {
	mu        sync.Mutex
	capacity  int
	inFlight  int
	successes int
	lastCut   time.Time
	waiters   []chan struct{}
}

func newAdaptiveLimiter() *adaptiveLimiter {
	return &adaptiveLimiter{capacity: limiterCeiling}
}

// acquire blocks until a slot is free or the context ends.
//
// A woken waiter has *been handed* a slot rather than invited to race for one.
// The distinction is the whole of the accounting: while a wake was only an
// invitation, a waiter that was signalled and then cancelled could not tell
// whether it held a slot, and returning one it never had drove inFlight
// negative — at which case `inFlight < capacity` is permanently true and
// admission control silently stops admitting anything at all.
func (l *adaptiveLimiter) acquire(ctx context.Context) error {
	wait := l.enter()
	if wait == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		l.abandon(wait)
		return ctx.Err()
	case <-wait:
		// The releasing goroutine kept the slot counted on our behalf, so
		// there is nothing to increment and nothing to re-check.
		return nil
	}
}

// enter is acquire's whole critical section, split off so the unlock is a defer
// and the wait happens outside the lock. It either takes a free slot and
// returns nil, or queues and returns the channel whoever frees the next slot
// will close.
func (l *adaptiveLimiter) enter() chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight < l.capacity {
		l.inFlight++
		return nil
	}
	wait := make(chan struct{})
	l.waiters = append(l.waiters, wait)
	return wait
}

func (l *adaptiveLimiter) abandon(wait chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, w := range l.waiters {
		if w == wait {
			// Still queued: no slot was ever handed over, so there is nothing
			// to give back.
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			return
		}
	}
	// Off the queue means a concurrent release handed us its slot before the
	// cancellation landed. That slot is real and now unused, so it goes back.
	l.releaseLocked()
}

// release returns the slot and adapts: rateLimited cuts capacity in half,
// success accumulates toward growth.
func (l *adaptiveLimiter) release(rateLimited bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rateLimited {
		if time.Since(l.lastCut) > limiterCutCooldown {
			l.capacity = l.capacity / 2
			if l.capacity < limiterFloor {
				l.capacity = limiterFloor
			}
			l.lastCut = time.Now()
		}
		l.successes = 0
	} else {
		l.successes++
		if l.successes >= limiterGrowthEvery && l.capacity < limiterCeiling {
			l.capacity++
			l.successes = 0
		}
	}
	l.releaseLocked()
	l.wakeLocked()
}

// wakeLocked admits waiters into slots that are free because the ceiling moved
// rather than because a request finished. Growth adds a slot nobody holds, and
// unlike the release handoff this loop's body does change what it tests: each
// admission counts a slot, so it stops when the new ceiling is full.
func (l *adaptiveLimiter) wakeLocked() {
	for len(l.waiters) > 0 && l.inFlight < l.capacity {
		wait := l.waiters[0]
		l.waiters = l.waiters[1:]
		l.inFlight++
		close(wait)
	}
}

// releaseLocked returns one slot: to the head of the queue if anyone is
// waiting for it, otherwise to the pool.
//
// Exactly one waiter is woken because exactly one slot came free. The loop that
// used to be here re-tested a condition the loop body could not change — the
// woken waiter had not run yet, so inFlight was still below capacity — and so
// woke every waiter on the queue for a single freed slot, at which point all of
// them raced back to the lock and all but one queued again.
//
// When inFlight is above capacity the slot is not handed on: a 429 has just
// halved the ceiling and the excess has to drain before anybody new is let in.
func (l *adaptiveLimiter) releaseLocked() {
	if len(l.waiters) > 0 && l.inFlight <= l.capacity {
		wait := l.waiters[0]
		l.waiters = l.waiters[1:]
		close(wait)
		// The slot stays counted: it moved to the waiter, it did not free up.
		return
	}
	l.inFlight--
}

// retryAfter reads the provider's own instruction for when to come back:
// Retry-After as seconds or an HTTP date. Zero means the header said nothing.
func retryAfter(response *http.Response) time.Duration {
	value := response.Header.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if wait := time.Until(at); wait > 0 {
			return wait
		}
	}
	return 0
}

// sharedLimiter is process-global: many clients (talk, work, boost, media,
// vision) share one OpenRouter account, and the account's rate limit is the
// thing being adapted to — per-client limiters would each rediscover it.
var sharedLimiter = newAdaptiveLimiter()
