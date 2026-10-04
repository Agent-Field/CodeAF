package dirwatch

import "time"

// Clock is the time the feed lives by, so a test can own it.
type Clock interface {
	// Now is the wall clock. It carries no monotonic reading, because a laptop
	// that slept is told apart from one that did not by the wall moving on.
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer is a one-shot timer.
type Timer interface {
	C() <-chan time.Time
	Stop()
}

// realClock is the machine's clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now().Round(0) }

func (realClock) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop()               { r.t.Stop() }
