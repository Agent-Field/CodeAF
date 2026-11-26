package relay

// EVERY CLIENT OF THIS RELAY DROPS AT THE SAME INSTANT WHEN THE RELAY DOES. A
// deploy, a restart or a network that flaps closes every carrier and every
// attached surface in one breath, and a client that waits a fixed second before
// its first retry is then a thousand clients arriving in the same second. The
// relay's own limits count per minute and per network, so the second wave is
// turned away and retries on the same schedule as the third, which is how an
// outage that lasted a moment becomes one that lasts a minute.
//
// So a wait is never exactly what the loop above it computed. The first retry
// after a drop is spread over a few seconds, and every later wait is spread a
// little either way. Jitter cures the spike and nothing else: it cannot make a
// computer that needs more registrations a minute than the relay allows fit,
// because the allowance is per minute whatever the spread.

import (
	"math/rand/v2"
	"time"
)

// The spread of the first retry after a drop is one second at the least and six
// at the most, which is short enough that a chat that resumes when its host
// returns does not feel the wait, and wide enough to flatten one relay
// instance's reconnect burst.
const (
	firstRetryFloor  = time.Second
	firstRetrySpread = 5 * time.Second
)

// The band every later wait is scaled into: three quarters to five quarters of
// what the loop's own doubling computed. Centred on one, so the caps and the
// back-off a loop already has keep their meaning on average.
const (
	jitterLow  = 0.75
	jitterSpan = 0.5
)

// rnd is the random source, a package variable so a test can pin it. It answers
// a number in [0, 1), which is what [rand.Float64] answers and what the two
// formulas below are written against.
var rnd = rand.Float64

// FirstRetry is how long a client waits before the first retry after it was
// dropped: one second plus a uniform draw from [0, 5 s). Only that first wait
// uses it; a loop that has already waited once doubles and calls [Jitter].
func FirstRetry() time.Duration {
	return firstRetryFloor + time.Duration(rnd()*float64(firstRetrySpread))
}

// Jitter spreads a wait a loop has already computed: d times a uniform draw from
// [0.75, 1.25). A wait that is not positive comes back unchanged, because there
// is nothing to spread and a loop that asked for no wait meant none.
func Jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return time.Duration(float64(d) * (jitterLow + rnd()*jitterSpan))
}
