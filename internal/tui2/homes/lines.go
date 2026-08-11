package homes

import (
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The one-line summaries. Each is the sentence its row says about itself on
// line 2 of a card, and each is assembled from the same separator the rest of
// the surface uses (5.17's `·`) rather than from a per-room punctuation.
//
// They live together because they answer one question — "what does a reader need
// from this row without opening it" — and because putting them beside each other
// is how the four rooms were kept from growing four different tones of voice.

// sepRun is the telemetry separator with its spaces, the spelling the rail and
// the place line both use.
const sepRun = " " + tokens.GlyphSeparator + " "

// clause accumulates `a · b · c` without a Sprintf.
type clause struct {
	b strings.Builder
}

func (c *clause) add(s string) {
	if s == "" {
		return
	}
	if c.b.Len() > 0 {
		c.b.WriteString(sepRun)
	}
	c.b.WriteString(s)
}

func (c *clause) addInt(n int, word string) {
	var buf [24]byte
	out := strconv.AppendInt(buf[:0], int64(n), 10)
	out = append(out, ' ')
	out = append(out, word...)
	c.add(string(out))
}

func (c *clause) String() string { return c.b.String() }

// age renders how long ago at was, or "" when either end of the subtraction is
// missing. A missing time draws NOTHING rather than [tokens.GlyphMissing]: on a
// card there is no room to explain what the dash stands for, and a dash a reader
// cannot decode is worse than a cell that is not there.
func age(at, now time.Time) string {
	if at.IsZero() || now.IsZero() {
		return ""
	}
	d := now.Sub(at)
	if d < 0 {
		d = 0
	}
	return tokens.Elapsed(d)
}

// until renders how long until at, or "" when it is missing or already past.
func until(at, now time.Time) string {
	if at.IsZero() || now.IsZero() {
		return ""
	}
	d := at.Sub(now)
	if d <= 0 {
		return ""
	}
	return tokens.Elapsed(d)
}

// line is a belief's status: what it is about, how much it is trusted, and how
// long it has been held. A let-go belief says so first, because that is the one
// thing about it a reader must not miss.
func (b Belief) line(now time.Time) string {
	var c clause
	if b.Retired {
		c.add("let go")
	} else if b.Provisional {
		c.add("candidate")
	}
	c.add(b.Scope)
	c.add(b.Trust)
	if a := age(b.Learned, now); a != "" {
		c.add(a)
	}
	return c.String()
}

// State reports the belief's reading in one word, for the detail pane's header.
func (b Belief) state() string {
	switch {
	case b.Retired:
		return "let go"
	case b.Provisional:
		return "candidate"
	}
	return "held"
}

// life maps a charter's standing onto the rail's lifecycle vocabulary.
//
// Proposed is [LifeQueued] and not something louder: the amber ? comes from the
// row's question count, which outranks every lifecycle (5.9), so encoding the
// ask twice would be two facts about one glyph. Paused is [LifePaused] — 5.17
// bans ⏸ for width instability and spends `=` on it. Retired is [LifeCancelled]
// rather than [LifeSettled]: a retired charter did not succeed, it stopped.
func (c Charter) life() Lifecycle {
	switch c.State {
	case CharterActive:
		return LifeWorking
	case CharterPaused:
		return LifePaused
	case CharterRetired:
		return LifeCancelled
	}
	return LifeQueued
}

// line is a charter's status: its cadence, where it is on the probation ladder,
// and the last thing the sentinel said. The sentinel's own sentence wins the
// tail of the line when there is one, because a charter that has never fired has
// nothing else true to say about itself.
func (c Charter) line(tenureAt int) string {
	var cl clause
	switch c.State {
	case CharterProposed:
		cl.add("waiting to be stood up")
	case CharterPaused:
		cl.add("paused")
	case CharterRetired:
		cl.add("retired")
	}
	cl.add(c.Cadence)
	if c.Probation && tenureAt > 0 {
		cl.add(ladder(c.Greens, tenureAt))
	}
	if c.Today > 0 {
		cl.addInt(c.Today, "today")
	}
	cl.add(firstLine(c.LastLine))
	return cl.String()
}

// ladder is the probation reading: `probation 2/3`. Numbers stay the primary
// encoding (5.13) and there is no bar, because three steps is not a quantity —
// it is a countable thing a person can hold in their head.
func ladder(greens, tenureAt int) string {
	var buf [32]byte
	out := append(buf[:0], "probation "...)
	out = strconv.AppendInt(out, int64(greens), 10)
	out = append(out, '/')
	out = strconv.AppendInt(out, int64(tenureAt), 10)
	return string(out)
}

// line is a service's status: what it is, where it answers, and how much it has
// had to be brought back. A restart count is only shown once it is non-zero,
// because "0 restarts" is a number about nothing — but the moment it is not
// zero it is the most useful fact on the row, since a running service with
// eleven restarts is a flapping service and nothing else on the card says so.
func (s Service) line() string {
	var c clause
	c.add(s.word())
	c.add(s.Health)
	if s.Restarts > 0 {
		c.addInt(s.Restarts, restartWord(s.Restarts))
	}
	if s.AutoRestart {
		c.add("auto-restart")
	}
	c.add(firstLine(s.Command))
	return c.String()
}

func restartWord(n int) string {
	if n == 1 {
		return "restart"
	}
	return "restarts"
}

// word is the service's run state in the product's language. It is not derived
// from the glyph: the glyph says the CATEGORY and the word says the thing, and
// a reader scanning a list of four services wants the word.
func (s Service) word() string {
	switch s.Life {
	case LifeWorking:
		return "running"
	case LifeFailed:
		return "failed"
	case LifeCancelled:
		return "stopped"
	case LifePaused:
		return "resting"
	}
	return "queued"
}

// Attention is what an [Item]'s glyph says. It mirrors [rail.Row.Attention]'s
// precedence — a human first, then broken, then moving — so a row means the
// same thing in the rail and in the detail pane it previews.
func (i Item) Attention() Lifecycle {
	if i.Inert {
		return LifeQueued
	}
	return i.Life
}
