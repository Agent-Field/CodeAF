package teams

import (
	"errors"
	"time"
)

// ── THE WRAP-UP CLOCK, KEPT IN THE FILE ─────────────────────────────────────
//
// A wrap-up is bounded by time (internal/session's wrapUpFor, fifteen minutes
// when the product sets it). That clock used to live only in the manager
// process, so quitting codeaf or an engine restart forgot it and the team
// never closed, or the next process started the fifteen minutes again.
//
// THE RECORD IS THIS, on the team, written through [Change] and [ChangeIf]
// like every other mutation of teams.json. Started is when the wrap-up began
// and Bound is how long it was given, so a process that finds one computes
// the time left as Bound minus how long since Started, and one already past
// Bound closes on the same road a live clock would have. Absent is none: a
// file from before this field loads with [Team.Wrap] nil, and a team with no
// wrap-up is written without the key.
//
// A SECOND REQUEST DOES NOT RESET THE CLOCK. [File.SetWrap] keeps the first
// start, the same rule the session keeps in memory. [File.ClearWrap] is the
// end: the report went out, or the bound was already past and the incomplete
// report was raised. Closing the team clears it too ([File.Close]).

// Wrap is one team's wrap-up in progress.
type Wrap struct {
	// Started is when the wrap-up began.
	Started time.Time `json:"started"`
	// Bound is how long it was given. It is stored as a number of nanoseconds,
	// which is how a duration is written in JSON, so an old reader that does
	// not know the field still leaves it alone.
	Bound time.Duration `json:"bound"`
}

// ErrWrap is [File.SetWrap] handed a start or a bound that cannot be resumed.
var ErrWrap = errors.New("teams: a wrap-up needs a start and a bound")

// SetWrap records a wrap-up on team id, once. A wrap-up already recorded is
// left as it was, so a second request keeps the first clock. started and
// bound must both be set.
func (f *File) SetWrap(id string, started time.Time, bound time.Duration) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	if f.Teams[i].Wrap != nil {
		return nil
	}
	if started.IsZero() || bound <= 0 {
		return ErrWrap
	}
	f.Teams[i].Wrap = &Wrap{Started: started, Bound: bound}
	return nil
}

// ClearWrap forgets team id's wrap-up. A team with none is left as it was.
func (f *File) ClearWrap(id string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	f.Teams[i].Wrap = nil
	return nil
}
