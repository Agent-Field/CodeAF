package taskdoor

import "time"

const (
	PingEvery   = 15 * time.Second
	SilentAfter = 3 * PingEvery
	RosterFor   = time.Minute
	DialWithin  = 10 * time.Second
)

// Clock supplies both time readings and wakeups: swapping only Now would
// leave a silent-watch test waiting for the wall clock's ticker.
type Clock interface {
	Now() time.Time
	NewTicker(time.Duration) Ticker
}
type Ticker interface {
	C() <-chan time.Time
	Stop()
}
type wallClock struct{}

func (wallClock) Now() time.Time                   { return time.Now() }
func (wallClock) NewTicker(d time.Duration) Ticker { return wallTicker{time.NewTicker(d)} }

type wallTicker struct{ *time.Ticker }

func (t wallTicker) C() <-chan time.Time { return t.Ticker.C }
func clockOrWall(clock Clock) Clock {
	if clock != nil {
		return clock
	}
	return wallClock{}
}
