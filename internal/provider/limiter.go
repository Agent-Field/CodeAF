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
//
// EVERY CUT HEALS, and that is not decoration. A halving is a guess about a
// moment, and this process is not the only thing spending the account's rate:
// sibling aforge processes share the key, so a cut here is often a report about
// somebody else's second. A guess that can only ever tighten is a ratchet. So
// capacity comes back two ways — in successes under load, and on the clock when
// there is no load to earn them with (healLocked).
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
	// It is the FLOOR under that window, not the whole of it — a 429 that named
	// a Retry-After names its own window instead (see release).
	limiterCutCooldown = 2 * time.Second
	// limiterHealQuiet is how long the pacing has to have been over before a cut
	// starts being given back, measured from the end of the cut's own window.
	// Twenty seconds is a third of the minute most provider windows are counted
	// in: long enough that the burst is genuinely finished, short enough that a
	// person waiting out somebody else's burst is not still paying for it a
	// minute later.
	limiterHealQuiet = 20 * time.Second
	// limiterHealEvery is one slot returned per this much continued quiet. Five
	// seconds walks the floor back to the ceiling in about five minutes, which
	// is slow enough that the climb cannot re-trigger the burst it is climbing
	// out of, and fast enough that one burst does not shape the next hour.
	limiterHealEvery = 5 * time.Second
)

// adaptiveLimiter is shared by every request a client sends.
type adaptiveLimiter struct {
	mu        sync.Mutex
	capacity  int
	inFlight  int
	successes int
	// cutUntil is the end of the window the last cut covers. Every 429 arriving
	// before it is the same signal as the one that caused the cut.
	cutUntil time.Time
	// healFrom is the first instant a cut slot may come back, and it is zero
	// while there is no cut to undo. Zero therefore means "never healed", which
	// is what keeps a limiter nobody has paced from drifting.
	healFrom time.Time
	waiters  []chan struct{}
	// now is the clock, injectable so the healing above can be tested in
	// microseconds rather than in the minutes it describes.
	now func() time.Time
}

func newAdaptiveLimiter() *adaptiveLimiter {
	return &adaptiveLimiter{capacity: limiterCeiling, now: time.Now}
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
	// Quiet is earned on the clock and not in requests, so a limiter that was
	// cut and then went idle has to be asked here — it will never reach a
	// release to be asked there.
	l.healLocked()
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
// success accumulates toward growth. namedWait is the Retry-After the provider
// sent with a 429, and zero when it sent none.
//
// ── ONE WINDOW, ONE HALVING ──
//
// The window a cut covers is the provider's own instruction when it gave one:
// a 429 that says "come back in thirty seconds" has already described how long
// this account is paced for, and every 429 arriving inside those thirty seconds
// is that same fact restated by a request that was already in the air. Halving
// again for each of them is how a single burst becomes six halvings and a
// process pinned at one slot. Below the header, limiterCutCooldown is the floor
// under the same idea.
//
// The growth counter is cleared only by a cut that LANDED, for the same reason.
// It used to be cleared by every 429 including the ones the cooldown ignored,
// so a burst that was supposed to count once still zeroed the way back out of
// it once per response — the halvings were suppressed and the recovery was not.
func (l *adaptiveLimiter) release(rateLimited bool, namedWait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rateLimited {
		now := l.now()
		if now.After(l.cutUntil) {
			l.capacity = l.capacity / 2
			if l.capacity < limiterFloor {
				l.capacity = limiterFloor
			}
			hold := limiterCutCooldown
			if namedWait > hold {
				hold = namedWait
			}
			// The same ceiling the wait itself is held to (retry.go): a
			// Retry-After may be an HTTP date naming tomorrow morning, and a
			// limiter that took it literally would refuse to cut again for
			// hours.
			if hold > maxProviderWait {
				hold = maxProviderWait
			}
			l.cutUntil = now.Add(hold)
			l.healFrom = l.cutUntil.Add(limiterHealQuiet)
			l.successes = 0
		}
	} else {
		l.successes++
		if l.successes >= limiterGrowthEvery && l.capacity < limiterCeiling {
			l.capacity++
			l.successes = 0
		}
	}
	l.healLocked()
	l.releaseLocked()
	l.wakeLocked()
}

// healLocked gives back slots that a cut took and quiet has since made
// meaningless.
//
// ── THE RATCHET THIS UNDOES ──
//
// Growth is earned in successes, and a 429 zeroes the counter. That is a fair
// bargain when the 429 is ours; it is not one on this machine, where several
// aforge processes — windows, task nodes, the resident — share ONE API KEY, and
// a sibling's burst arrives here as if this process had caused it. Under that
// contention the halvings compound while the successes that would undo them
// never accumulate, so one minute of somebody else's traffic ratchets every
// session's throughput down and leaves it there. An idle process is worse
// still: it earns no successes at all, so without a clock its capacity never
// comes back at any speed.
//
// So quiet is the second currency, and it is the one a sibling cannot spend.
// The success path above is still the fast way back under load; this is the way
// back for a process that has been cut and then given nothing to do.
func (l *adaptiveLimiter) healLocked() {
	if l.healFrom.IsZero() || l.capacity >= limiterCeiling {
		return
	}
	now := l.now()
	if now.Before(l.healFrom) {
		return
	}
	// One slot for arriving at healFrom at all, then one per interval since.
	// A process that was idle for an hour heals in a single step here, which is
	// the point: an hour-old halving carries no information about now.
	slots := 1 + int(now.Sub(l.healFrom)/limiterHealEvery)
	l.capacity += slots
	if l.capacity >= limiterCeiling {
		l.capacity = limiterCeiling
		// Nothing left to give back; disarm until the next cut arms it again.
		l.healFrom = time.Time{}
		return
	}
	l.healFrom = l.healFrom.Add(time.Duration(slots) * limiterHealEvery)
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
//
// IT IS SHARED NO FURTHER THAN THE PROCESS, and that is a decision rather than
// an oversight. The contention is per-KEY: several aforge processes on this
// machine send under one account, so each of them adapts alone to a rate all of
// them are spending. A file-coordinated limiter could close that gap — the
// presence files are already there to build it on — and it would buy a lock on
// the hot path of every provider call, a stale-holder problem every time a
// process is killed, and a shared number that is wrong the moment somebody
// opens a session on a second machine with the same key. The cheaper answer is
// the one above: assume the cut may not be ours, and heal it on the clock. If
// the sharing ever needs to be exact rather than merely un-ratcheted, that is
// where the work starts.
var sharedLimiter = newAdaptiveLimiter()
