package relay

// THE ABUSE BREAKER FOR A LIVE STREAM. It is the relay's one answer to a surface
// that sends more than any live view ever does, and it is computed from the
// clock of the chunk that arrives: no timer and no goroutine stand behind it.
// The hosted relay has the same rule for the same reason, which is that a timer
// is exactly what would keep an idle object awake, and its twin is
// relay/hosted/src/engine/breaker.js, under the same two numbers.
//
// IT CUTS AND DOES NOT SHAPE. This package cannot read a byte it carries, so it
// cannot batch, delay or fold anything: ending the stream is its only move. The
// host and the viewer pace themselves long before this matters, and the number
// is four times the pace a correct host holds, so a correct host never trips it.
// It exists for a host that ignores its own pace.

import "time"

// breakerReason is what a cut stream's close frame says, with close code 1008
// (policy violation) on the WebSocket carrier. It is the same sentence the
// hosted relay uses (BREAKER_REASON in carrier.js).
const breakerReason = "sending faster than a live view does"

// breaker is a leaky bucket holding [BreakerWindow] worth of [BreakerRate] and
// draining at that rate, so a stream is over its rate when it has sent more than
// that rate sustained across the window, whether as one burst or as a steady flow
// a little above the line. One direction of one stream has one, and one goroutine
// reads it, so it holds no lock.
type breaker struct {
	level   float64
	at      time.Time
	started bool
}

// capacity is how many bytes the bucket holds: the rate for the whole window.
func (b *breaker) capacity() float64 {
	return float64(BreakerRate) * BreakerWindow.Seconds()
}

// charge counts n bytes arriving at now and answers true once the stream is over
// its rate. A clock that steps backwards drains nothing, which only ever forgives.
func (b *breaker) charge(n int, now time.Time) bool {
	if b.started && now.After(b.at) {
		b.level -= now.Sub(b.at).Seconds() * float64(BreakerRate)
		if b.level < 0 {
			b.level = 0
		}
	}
	if !b.started || now.After(b.at) {
		b.at = now
	}
	b.started = true
	b.level += float64(n)
	return b.level > b.capacity()
}
