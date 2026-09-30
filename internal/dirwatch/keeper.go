package dirwatch

import "time"

// keeper decides when a quiet socket is pinged and when it is called dead. It
// owns one timer that is always either "time to ping" or "the ping is overdue".
type keeper struct {
	clock       Clock
	timer       Timer
	armed       time.Time
	span        time.Duration
	outstanding bool // a ping has gone out and nothing has been heard since
}

func newKeeper(c Clock) *keeper {
	k := &keeper{clock: c}
	k.arm(KeepEvery)
	return k
}

// arm restarts the timer for d.
func (k *keeper) arm(d time.Duration) {
	if k.timer != nil {
		k.timer.Stop()
	}
	k.span, k.armed, k.timer = d, k.clock.Now(), k.clock.NewTimer(d)
}

// C is the channel the timer fires on.
func (k *keeper) C() <-chan time.Time { return k.timer.C() }

// heard files any sign of life: an answered ping is forgiven and the quiet
// wait starts over from now.
func (k *keeper) heard() {
	if k.outstanding {
		k.outstanding = false
		k.arm(KeepEvery)
	}
}

// dead reports, once the timer has fired, whether the socket should be given
// up. An unanswered ping is a dead socket. So is a timer that fired much later
// than it was set for by the wall clock: the machine slept, and whatever was
// on the other end of the socket has forgotten it.
func (k *keeper) dead() bool {
	return k.outstanding || k.clock.Now().Sub(k.armed) > 2*k.span
}

// pinged notes that a ping went out and gives the answer PongWithin to arrive.
func (k *keeper) pinged() {
	k.outstanding = true
	k.arm(PongWithin)
}

// stop releases the timer.
func (k *keeper) stop() { k.timer.Stop() }
