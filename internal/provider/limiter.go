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
func (l *adaptiveLimiter) acquire(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.inFlight < l.capacity {
			l.inFlight++
			l.mu.Unlock()
			return nil
		}
		wait := make(chan struct{})
		l.waiters = append(l.waiters, wait)
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			l.abandon(wait)
			return ctx.Err()
		case <-wait:
		}
	}
}

func (l *adaptiveLimiter) abandon(wait chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, w := range l.waiters {
		if w == wait {
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			return
		}
	}
	// Already signalled: the slot we were handed goes back to the pool.
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
}

func (l *adaptiveLimiter) releaseLocked() {
	l.inFlight--
	for len(l.waiters) > 0 && l.inFlight < l.capacity {
		wait := l.waiters[0]
		l.waiters = l.waiters[1:]
		close(wait)
		// The awakened waiter re-checks under the lock; reserve nothing here.
	}
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
