package livedoor

import "time"

// Clock is the time source for the multiplexer and live door so tests can
// drive pacing, ping deadlines, and idle detach without sleeping on wall time.
type Clock interface {
	// Now returns the current wall reading without a monotonic suffix so a
	// machine waking from sleep is seen as having jumped forward in time.
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer is a one-shot timer created by a Clock.
type Timer interface {
	C() <-chan time.Time
	Stop()
}

// RealClock returns the machine's wall clock backed by the standard time package.
func RealClock() Clock {
	return realClock{}
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().Round(0) }

func (realClock) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop()               { r.t.Stop() }
