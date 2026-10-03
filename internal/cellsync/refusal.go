package cellsync

import (
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// MaxHalt is the longest a wait may be, and how long a halted chat waits: a
// refusal that only a person can cure is not retried, and a Retry-After is
// never trusted past an hour, so a wrong clock on the relay cannot silence sync.
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
	Halts bool               // retrying is pointless until something changes, so stop until Freed or close
}

// refusals is listed from most to least final. A refusal that time cures
// within minutes stays quiet for a few of them, because a burst that ended
// before anyone could act is noise; one that only the person can cure is
// said at once.
var refusals = []Refusal{
	{Is: blobstore.ErrFull, Say: fullLine, Halts: true},
	{Is: wireauth.ErrRevoked, Say: said(chatlist.Removed), Halts: true},
	{Is: wireauth.ErrGone, Say: said(chatlist.ReplacedGone), Halts: true},
	{Is: wireauth.ErrRotated, Say: said(chatlist.Replaced), Halts: true},
	{Is: wireauth.ErrSkew, Say: said(chatlist.ClockOff), Flat: true},
	{Is: wireauth.ErrTooManyIdentities, Say: tooManyLine},
	{Is: wireauth.ErrRateLimited, Say: said(chatlist.SlowDown), Quiet: 3 * time.Minute},
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
