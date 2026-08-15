// Package reltime is the one relative-time formatter in the product.
//
// THE LAW IT HOLDS: time is RELATIVE everywhere, and absolute only inside an
// expanded record. A transcript row, a rail card, a receipt, a status line and
// a home all answer "when" with "now", "12s", "5m", "2h", "yesterday", "mon",
// "aug 3" — because the question a reader is actually asking of a running
// system is "how stale is this", and a wall-clock stamp makes them do the
// subtraction. The exact instant is what you get when you OPEN the record: an
// expanded task page, a trace row, a journal dump. Those are the only places an
// absolute stamp belongs, and they do not come through here.
//
// It exists as its own package for the reason 8.1.5 gives the header grammar:
// a vocabulary with two implementations is two vocabularies. Four surfaces were
// each one `fmt.Sprintf` away from inventing their own ladder, and a product
// that says "5m" in the rail and "5 minutes ago" in the transcript is a product
// with two clocks.
//
// It is pure: two functions of their arguments, no ambient `time.Now`, no
// state, and no dependency beyond the standard library. `now` is a parameter so
// a frame can latch ONE instant and format every cell in it from that instant —
// the same discipline blocks.Clock applies to animation (8.1.3), for the same
// reason: cells derived from separate reads of the clock disagree with each
// other on the frame where the second ticks.
//
// It is NOT the width-stable live cell. `tokens.Elapsed` is that — "45s, 5m,
// 1h02, 99d23", fixed width, no jitter, for a column that re-renders every
// frame (5.21). This package is the READING form: what a sentence, a receipt or
// a settled row says about a time, where the words matter more than the column.
// A caller that needs a fixed column pads what it gets here, or asks tokens.
package reltime

import (
	"strconv"
	"strings"
	"time"
)

// The rungs. They are named rather than inlined because the ladder is the
// specification, and a reader checking whether "12s" or "1m" is right for
// 61 seconds should be able to read the answer off the constants.
const (
	// nowBelow is where a reading stops being a number. Under ten seconds the
	// digits change faster than the eye can use them, and "now" is both truer
	// and quieter than a counter nobody is reading.
	nowBelow = 10 * time.Second
	// weekBelow is where the weekday stops being a location in time. Past six
	// days "mon" is ambiguous — WHICH monday — so the ladder switches to a date.
	weekBelow = 7
)

// Short is the relative reading of an instant: how long ago it was, in the
// fewest characters that stay honest.
//
// The ladder, in order:
//
//	now         under ten seconds, and anything in the future (see below)
//	12s         under a minute
//	5m          under an hour
//	2h          under a day
//	yesterday   the calendar day before now's
//	mon         within the last week
//	aug 3       older, in now's year
//	aug 3 2025  older still, in another year
//
// Each rung TRUNCATES rather than rounds, so a reading never claims more time
// has passed than actually has: 119 seconds is "1m", not "2m". A row that
// overstates its own staleness is the one direction of error that makes a
// reader distrust a live surface.
//
// THE FUTURE READS AS "now", deliberately. Clock skew is real — a journal row
// stamped by another machine, a database default, a laptop that just woke — and
// the honest rendering of "this happened a moment ago according to a clock that
// is not quite yours" is "now", never "-3s", which is a number no reader has a
// use for.
//
// A ZERO TIME RETURNS "", because a time that was never set is missing data and
// not a moment zero seconds ago. Rendering it as "now" would be exactly the
// invented number 8.2.20 forbids; the caller draws the missing glyph over an
// empty reading, as it does for a missing cost.
//
// The calendar rungs are read in now's LOCATION: "yesterday" means yesterday
// where the reader is sitting, not where the timestamp was written.
func Short(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	switch d := now.Sub(t); {
	case d < nowBelow:
		return "now"
	case d < time.Minute:
		return itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	}

	local := t.In(now.Location())
	switch days := calendarDays(local, now); {
	case days <= 1:
		// A day-or-more elapsed that lands on the same calendar day cannot
		// happen, but a clamp beats a "0 days ago" reading if it ever does.
		return "yesterday"
	case days < weekBelow:
		return lower3(local.Weekday().String())
	}
	date := lower3(local.Month().String()) + " " + itoa(local.Day())
	if local.Year() != now.Year() {
		return date + " " + itoa(local.Year())
	}
	return date
}

// Elapsed is the reading of a DURATION rather than of an instant: how long
// something took, or has been taking.
//
//	12s      under a minute
//	1m 12s   under an hour
//	2h 5m    under a day
//	3d 4h    beyond
//
// TWO RUNGS, NEVER THREE. "2h 5m 13s" is a stopwatch reading, and nobody
// waiting on a two-hour job is spending the seconds; the second rung is there
// to keep "1m" from swallowing 59 seconds of real waiting, and it stops being
// worth its cells the moment the first rung is hours.
//
// A rung whose remainder is zero is DROPPED rather than padded: "5m", not
// "5m 0s". The zero carries no information and the cells it costs are cells a
// receipt beside it could have used.
//
// A negative duration is zero. The two ways to get one are a clock that went
// backwards and a start stamp that has not landed yet, and "0s" is the honest
// reading of both.
//
// It is not width-stable, by design — see the package comment. A live column
// wants [tokens.Elapsed]; a receipt, a settled row or a sentence wants this.
func Elapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return two(int(d/time.Minute), "m", int((d%time.Minute)/time.Second), "s")
	case d < 24*time.Hour:
		return two(int(d/time.Hour), "h", int((d%time.Hour)/time.Minute), "m")
	default:
		const day = 24 * time.Hour
		return two(int(d/day), "d", int((d%day)/time.Hour), "h")
	}
}

// two renders the two-rung form, dropping a zero remainder.
func two(big int, bigUnit string, small int, smallUnit string) string {
	out := itoa(big) + bigUnit
	if small == 0 {
		return out
	}
	return out + " " + itoa(small) + smallUnit
}

// calendarDays is how many midnights separate t from now, both read in now's
// location.
//
// The midnights are normalized and then subtracted, rather than dividing the
// elapsed duration by 24 hours, because a DST day is 23 or 25 hours long and
// the integer division gets the spring-forward day wrong — which is the one day
// of the year a reader would notice "yesterday" reading as "mon". The half-day
// rounding is what absorbs that hour either way.
func calendarDays(t, now time.Time) int {
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	loc := now.Location()
	from := time.Date(ty, tm, td, 0, 0, 0, 0, loc)
	to := time.Date(ny, nm, nd, 0, 0, 0, 0, loc)
	const day = 24 * time.Hour
	return int((to.Sub(from) + day/2) / day)
}

// lower3 is the three-letter lowercase form the ladder's weekday and month
// rungs are written in: "mon", "aug". Lowercase because the TUI's voice is
// lowercase throughout — a capitalized "Mon" in a row of dim telemetry reads as
// a proper noun, which is exactly what it is not.
func lower3(name string) string {
	if len(name) > 3 {
		name = name[:3]
	}
	return strings.ToLower(name)
}

func itoa(n int) string { return strconv.Itoa(n) }
