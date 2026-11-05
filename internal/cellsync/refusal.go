package cellsync

import (
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// MaxHalt bounds even halting refusals so sync discovers recovered capacity
// without reopening the chat or trusting a relay's clock beyond an hour.
const MaxHalt = time.Hour

// Refusal is one way the relay says no: the sentinel the wire names it by, the
// one plain sentence the person reads, and how the client carries on. The table
// below is the only place these three meet, so a new refusal is one row and no
// call site learns of it.
type Refusal struct {
	Is    error              // the sentinel the wire returns
	Say   func(error) string // what the person is told; it may read the refusal, for a number the relay named
	Quiet time.Duration      // how long the refusal must last before the person hears of it
	Flat  bool               // retry every interval: only time cures it, so backing off adds nothing
	Halts bool               // Wait MaxHalt because frequent retries cannot create capacity; Freed can wake it sooner.
	// RetryStartup also retries before a batcher exists, when time or capacity can cure the refusal.
	RetryStartup bool
}

// refusals is listed from most to least final. A refusal that time cures
// within minutes stays quiet for a few of them, because a burst that ended
// before anyone could act is noise; a longer refusal is said at once.
var refusals = []Refusal{
	// Both decoders preserve a general full match, so shared capacity must come first.
	{Is: wireauth.ErrRelayFull, Say: said(chatlist.RelayStorageFull), Halts: true, RetryStartup: true},
	{Is: blobstore.ErrFull, Say: fullLine, Halts: true, RetryStartup: true},
	{Is: wireauth.ErrRevoked, Say: said(chatlist.Removed), Halts: true},
	{Is: wireauth.ErrGone, Say: said(chatlist.ReplacedGone), Halts: true},
	{Is: wireauth.ErrRotated, Say: said(chatlist.Replaced), Halts: true},
	{Is: wireauth.ErrSkew, Say: said(chatlist.ClockOff), Flat: true},
	{Is: wireauth.ErrTooManyIdentities, Say: tooManyLine, RetryStartup: true},
	{Is: wireauth.ErrRateLimited, Say: said(chatlist.SlowDown), Quiet: 3 * time.Minute, RetryStartup: true},
}

// said is the Say of a refusal whose sentence never varies.
func said(line string) func(error) string { return func(error) string { return line } }

// fullLine names the ceiling the relay told us, when it told us one.
func fullLine(err error) string { return chatlist.RelayFull(blobstore.LimitOf(err)) }

// tooManyLine says when the relay's day for this network frees up, when it told us.
func tooManyLine(err error) string { return chatlist.TooManyNewIn(wireauth.After(err)) }

// RefusalOf finds the row err is, and false for a failure that is not a
// refusal (an unreachable relay, say), which is retried on the plain backoff
// and never shown.
func RefusalOf(err error) (Refusal, bool) {
	for _, r := range refusals {
		if errors.Is(err, r.Is) {
			return r, true
		}
	}
	return Refusal{}, false
}

// StartupWait shares the running retry policy with a chat whose drive could
// not start. Refusals that need a person leave startup stopped as before.
func (r Refusal) StartupWait(prev, interval time.Duration, err error) (time.Duration, bool) {
	if !r.RetryStartup {
		return 0, false
	}
	return r.next(prev, interval, err), true
}

// next is the wait after a failed flush that followed a wait of prev. The relay's
// own Retry-After is a floor on any wait, and a halt outranks every other.
func (r Refusal) next(prev, interval time.Duration, err error) time.Duration {
	wait := min(2*max(prev, interval), max(MaxBackoff, interval))
	if r.Flat {
		wait = interval
	}
	wait = max(wait, min(wireauth.After(err), MaxHalt))
	if r.Halts {
		wait = MaxHalt
	}
	return wait
}
