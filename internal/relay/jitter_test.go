package relay

import (
	"testing"
	"time"
)

// pinned makes the random source answer the given draws in turn, repeating the
// last, and puts the real one back when the test ends.
func pinned(t *testing.T, draws ...float64) {
	t.Helper()
	was := rnd
	t.Cleanup(func() { rnd = was })
	i := 0
	rnd = func() float64 {
		d := draws[min(i, len(draws)-1)]
		i++
		return d
	}
}

// A client dropped by a relay restart waits one second and then a draw from the
// next five, so a thousand of them do not come back in the same second.
func TestTheFirstRetryIsOneSecondPlusAnythingUpToFive(t *testing.T) {
	for _, tc := range []struct {
		draw float64
		want time.Duration
	}{
		{0, time.Second},
		{0.5, 3500 * time.Millisecond},
		{0.2, 2 * time.Second},
	} {
		pinned(t, tc.draw)
		if got := FirstRetry(); got != tc.want {
			t.Errorf("draw %v: first retry %v, want %v", tc.draw, got, tc.want)
		}
	}
	pinned(t, 0.9999999)
	if got := FirstRetry(); got < 5*time.Second || got >= 6*time.Second {
		t.Errorf("the top of the range gave %v, want within [5s, 6s)", got)
	}
}

// Every later wait is the loop's own doubled wait scaled into three quarters to
// five quarters of it, so a cap keeps meaning what it meant on average.
func TestAWaitIsSpreadAQuarterEitherWay(t *testing.T) {
	pinned(t, 0)
	if got := Jitter(8 * time.Second); got != 6*time.Second {
		t.Errorf("the bottom of the range gave %v, want 6s", got)
	}
	pinned(t, 0.5)
	if got := Jitter(8 * time.Second); got != 8*time.Second {
		t.Errorf("the middle of the range gave %v, want exactly the wait asked for", got)
	}
	pinned(t, 0.9999999)
	if got := Jitter(8 * time.Second); got < 9*time.Second || got >= 10*time.Second {
		t.Errorf("the top of the range gave %v, want within [9s, 10s) of 8s", got)
	}
}

// A loop that asked for no wait, or for an impossible one, is not given one.
func TestNoWaitIsSpreadIntoAWait(t *testing.T) {
	pinned(t, 0.9)
	for _, d := range []time.Duration{0, -time.Second} {
		if got := Jitter(d); got != d {
			t.Errorf("Jitter(%v) = %v, want it unchanged", d, got)
		}
	}
}

// With the real random source every answer is inside its range and they are not
// all the same: a source that returned one number would pass the pinned tests
// and spread nothing.
func TestTheRealSourceStaysInRangeAndActuallySpreads(t *testing.T) {
	firsts := map[time.Duration]bool{}
	scaled := map[time.Duration]bool{}
	for i := 0; i < 2000; i++ {
		first := FirstRetry()
		if first < time.Second || first >= 6*time.Second {
			t.Fatalf("first retry %v is outside [1s, 6s)", first)
		}
		firsts[first] = true
		wait := Jitter(16 * time.Second)
		if wait < 12*time.Second || wait >= 20*time.Second {
			t.Fatalf("a 16s wait came back %v, outside [12s, 20s)", wait)
		}
		scaled[wait] = true
	}
	if len(firsts) < 100 || len(scaled) < 100 {
		t.Fatalf("2000 draws gave %d distinct first retries and %d distinct waits: nothing is being spread", len(firsts), len(scaled))
	}
}
