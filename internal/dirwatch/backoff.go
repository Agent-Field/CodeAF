package dirwatch

import "time"

// backoff is the full-jitter ladder of reconnect waits. Its zero value is ready.
type backoff struct {
	ceiling time.Duration
	rand    func() float64 // in [0,1)
}

// next climbs one rung and returns a wait anywhere below it.
func (b *backoff) next() time.Duration {
	b.ceiling = min(max(b.ceiling*2, RetryBase), RetryCap)
	return time.Duration(b.rand() * float64(b.ceiling))
}

// reset starts the ladder over, after a socket that worked.
func (b *backoff) reset() { b.ceiling = 0 }
