package tui3

// THE PULSE: THE ONE LINE AT THE TOP OF HOME.
//
//	aforge                      on watch · 4 orders · $1.10 today · fri 9:41am
//
// The program's name on the left, and right-aligned on the other end — quiet
// unless it has a reason not to be — the machine's own vital signs. It is the
// watch made visible (docs/HOME-BRIDGE.md):
// a person who has just sat down learns in one glance that something is keeping
// an eye on things, how many things, what the day has cost so far, and what time
// it is, without reading a single row.
//
// ── THE LAWS ──
//
//   - IT READS WHAT THE MACHINE'S CARD READS. Every segment comes off
//     [app.machineFactsAt], which is the same reading the `keeping an eye on`,
//     `since you left` and `today` bands draw from. One reader, two surfaces: a
//     top line saying `4 orders` over a card listing three would be the screen
//     arguing with itself.
//
//   - EVERY SEGMENT OBEYS THE EMPTINESS LAW. Nothing standing draws no watch
//     segment — not `0 orders` — and a day that has cost nothing says nothing
//     about money. A line that permanently reads `on watch · 0 orders · $0.00
//     today` is a permanent reminder of the absence of three things.
//
//   - THE CLOCK ALWAYS DRAWS, and it is the exception that proves the law: the
//     time is never absent, never zero, and it is the one segment that is worth
//     a cell on a screen with nothing else to say.
//
//   - NEVER A QUOTA FRACTION HERE. What the day has spent belongs on the pulse;
//     what the day is ALLOWED to spend is the machine card's `today` band and
//     nowhere else (homeband_today.go). A ceiling on this line would turn a
//     glance into an arithmetic problem. The figure may CHANGE COLOUR as that
//     bound comes close, which says the same thing in the one way a glance can
//     take in without reading.

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The words the pulse says, quoted in internal/manual/chat/home.md exactly as
// they are spelled here.
const (
	// pulseName is the left of the line: this program, which is what the screen
	// is a dashboard OF.
	pulseName = "aforge"
	// pulseWatchWord is the segment that says something is keeping an eye on
	// things. It is the same three words the ambient side uses everywhere else
	// for the same fact ([homeWatchLabel] is /status's spelling of it).
	pulseWatchWord = "on watch"
	// pulseOrderWord is what a standing thing is CALLED when it is counted —
	// `4 orders` — which is the standing-orders vocabulary (docs/STANDING-ORDERS.md)
	// and never a word for a row in a store.
	pulseOrderWord = " order"
	// pulseWorkingWord follows the count of what the machine has in flight —
	// `3 working`. It is the word the presence file already writes for the same
	// state ([session.PresenceWorking]) and the word the moving zone's own law
	// uses; the banned vocabulary is `auditor`, `verdict`, `verified` — machinery
	// nouns — and this is what a person calls it.
	pulseWorkingWord = " working"
	// pulseTodayWord follows the day's spend.
	pulseTodayWord = " today"
	// pulseGap is the separator every list of clauses on this surface uses.
	pulseGap = " · "
)

// pulseLine is that line, painted, exactly `width` cells wide at most.
//
// THE NAME NEVER GIVES WAY TO THE SEGMENTS. A frame too narrow to hold both
// draws the name alone: the segments are a glance somebody takes and the name is
// what tells them which program they are looking at, and a top line that clipped
// the second to fit the first would have got the order of those two backwards.
func (a *app) pulseLine(width int, pal palette) string {
	// THE NAME IS STRUCTURE, SO IT WEARS A QUIET ROLE. THE ACCENT BUDGET IS ONE
	// THING PER SCREEN and it is always the live one — the row waiting on
	// somebody, the work in flight, the card under the cursor. A product name is
	// none of those: it is the same word on every frame home has ever drawn,
	// which is the definition of a thing the eye learns to skip, and spending the
	// loudest hue on it left home with two places claiming to be first.
	//
	// So it takes [hueMuted] — one rung above the dim telemetry it shares the
	// line with, so it still reads as the line's head — and keeps the WEIGHT,
	// which is what says "this is the title" on a sixteen-colour terminal that
	// has no rungs to spend.
	name := " " + pal.bold(pal.muted(pulseName))
	tail := strings.Join(a.pulseSegments(a.now(), pal), pulseGap)
	if tail == "" {
		return name
	}
	gap := width - ansi.StringWidth(name) - ansi.StringWidth(tail) - 1
	if gap < 1 {
		return name
	}
	return name + strings.Repeat(" ", gap) + tail
}

// pulseSegments is what the right of the line says, in order and already
// painted, with every segment that is not true left out.
//
// EACH SEGMENT IS PAINTED FOR WHAT IT MEANS AND NOT FOR WHERE IT SITS. The
// ordinary state of this line is the dim tier, because it is the surface talking
// about itself; the two segments that can mean something more say so in the hue
// for that meaning and in no other way — the machine is working right now, or
// the day is coming up against what it is allowed to spend.
func (a *app) pulseSegments(now time.Time, pal palette) []string {
	facts := a.machineFactsAt(now)
	var out []string
	if orders := len(facts.watching); orders > 0 {
		// TWO CLAUSES AND ONE FACT. `on watch` is that the machine is watching
		// at all; the count is how much of it there is. They appear and go
		// together, because a count with no word for it is a number nobody can
		// read and the word alone would be a claim with nothing behind it.
		//
		// AND THE WORDS LIFT WHILE A PASS IS ACTUALLY IN FLIGHT, into the hue
		// this surface paints work with. It is the same thing the status line
		// says by turning its glyph into a spinner ([app.keepingWord]) — presence
		// felt rather than announced — said here without a moving cell, because a
		// dashboard's top line is read out of the corner of an eye.
		ink := pal.dim
		if facts.firing {
			ink = pal.muted
		}
		out = append(out, ink(pulseWatchWord), ink(itoa(orders)+plural(pulseOrderWord, orders)))
	}
	if facts.hands > 0 {
		// THE MACHINE'S HANDS, AND IT COMES AFTER THE WATCH RATHER THAN BEFORE
		// IT. The line reads standing, then moving, then arithmetic, then the
		// clock — and the two watch clauses are ONE fact said in two words
		// ([pulseWatchWord] and its count), so nothing may be pushed between
		// them.
		//
		// AND IT DRAWS AT ONE. The emptiness law asks for the absence of a zero
		// and nothing more: a single hand out is worth knowing from across a room,
		// and `1 working` is a fact where `0 working` would be a permanent
		// reminder that nothing is happening.
		//
		// THE FIGURE IS THE ANSWER AND THE WORD IS THE QUESTION, which is the
		// payload rule exactly (docs/DESIGN-LANGUAGE.md): the count steps up into
		// the datum hue and the word beside it stays in the dim tier the rest of
		// this line is written in. Never the accent — a top line is a glance and
		// not the one live or chosen thing on the screen.
		out = append(out, pal.data(itoa(facts.hands))+pal.dim(pulseWorkingWord))
	}
	if facts.spent > 0 {
		// THE SPEND RISES OUT OF THE DIM ONLY WHEN THE BOUND IS CLOSE, on the one
		// threshold both surfaces share ([machineFacts.nearCeiling]). The FIGURE
		// changes colour and never grows a fraction: what the day is allowed is
		// the machine card's `today` band and nowhere else.
		ink := pal.dim
		if facts.nearCeiling() {
			ink = pal.warn
		}
		out = append(out, ink(dollars(facts.spent)+pulseTodayWord))
	}
	if clock := pulseClock(now); clock != "" {
		out = append(out, pal.dim(clock))
	}
	return out
}

// pulseClock is the day and the time, in the words this surface already uses
// for both: a lowercase weekday, as a standing item's `mon 8am` is, and a
// twelve-hour clock with its am or pm on it.
//
// IT IS NEVER A BARE `9:41`. Half the hours of the day are ambiguous without
// the suffix, and a dashboard whose clock could mean either of two times is a
// dashboard nobody trusts about anything else either.
func pulseClock(now time.Time) string {
	if now.IsZero() {
		return ""
	}
	return strings.ToLower(now.Format("Mon 3:04pm"))
}
