package tui3

// THE PULSE: THE ONE LINE AT THE TOP OF HOME.
//
//	aforge          2 want you · 4 moving · $0.55 / $20.00 · tue 1:11pm
//
// The program's name on the left, and right-aligned on the other end — quiet
// unless it has a reason not to be — the machine's own vital signs. It is the
// watch made visible (docs/HOME-BRIDGE.md): a person who has just sat down learns
// in one glance how many things are stopped on them, how many are moving, what
// the day has cost against what it is allowed, and what time it is, without
// reading a single row.
//
// THE WORDS AND THE ORDER ARE THE DESIGN'S, EXACTLY. SCREEN 2b and SCREEN 3b of
// docs/design/home-rethink/SCREENS.txt draw this line; FIDELITY.md item 2 is the
// owner's instruction to follow them to the letter, and every clause below is
// quoted from them rather than composed here.
//
// ── THE LAWS ──
//
//   - IT READS WHAT THE MACHINE'S CARD READS. Every segment comes off
//     [app.machineFactsAt], which is the same reading the `keeping an eye on`,
//     `since you left` and `today` bands draw from. One reader, two surfaces: a
//     top line saying `4 moving` over a card listing three would be the screen
//     arguing with itself.
//
//   - EVERY SEGMENT OBEYS THE EMPTINESS LAW. Nothing stopped on anybody draws no
//     `want you` clause — not `0 want you` — nothing in flight draws no `moving`,
//     and a day that has cost nothing says nothing about money. SCREEN 1c is that
//     law taken all the way and it is a real frame this file has to be able to
//     draw: a quiet morning is the name and the clock, and nothing else.
//
//   - THE CLOCK ALWAYS DRAWS, and it is the exception that proves the law: the
//     time is never absent, never zero, and it is the one segment that is worth
//     a cell on a screen with nothing else to say.
//
//   - THE ALLOWANCE IS A FRACTION HERE, AND THE OWNER OVERRULED THIS FILE TO PUT
//     IT THERE. What stood here for four waves was the opposite law, and it read:
//     "NEVER A QUOTA FRACTION HERE. What the day has spent belongs on the pulse;
//     what the day is ALLOWED to spend is the machine card's `today` band and
//     nowhere else. A ceiling on this line would turn a glance into an arithmetic
//     problem." The design draws `$0.55 / $20.00`, the owner said "follow the
//     exact design" on 2026-08-25, and the old law is retired rather than quietly
//     dropped — this paragraph is its headstone. The argument it lost is worth
//     keeping: the fraction is two figures where one would do. The argument that
//     beat it is that "how much is left" is the only thing anybody ever wanted the
//     first figure FOR, and a person who has to remember their own ceiling to read
//     a spend segment is doing the arithmetic anyway, in their head, wrongly.
//
//   - AND THE FRACTION RETIRED THE COLOUR CHANGE THAT STOOD IN FOR IT. The figure
//     used to rise out of the dim into the warning hue as the bound came close,
//     because a glance could take that in without reading. It no longer does: the
//     bound is now ON THE LINE, in cells a person can read, and a figure that both
//     stated the ceiling and changed colour about it would be saying one thing
//     twice. [machineFacts.nearCeiling] is unmoved and still governs the `today`
//     band, which is the surface that has a ceiling to talk about at length.

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
	// pulseWantWord follows the count of things that have stopped on a person —
	// `2 want you`. It is the design's own words (SCREEN 2b), and they are the
	// right ones for the reason the whole vocabulary law gives: `2 needs person`
	// is a machine describing its own state, and `2 want you` is the machine
	// telling somebody what is true about their morning.
	pulseWantWord = " want you"
	// pulseMovingWord follows the count of what the machine has in flight —
	// `4 moving`. It replaces the older ` working`, and it is the word the
	// switcher's own second zone already uses for the same rows (SCREEN 2b names
	// that zone `moving`), so the top line and the list under it now call one
	// fact by one name.
	pulseMovingWord = " moving"
	// pulseAllowanceGap separates the day's spend from the day's allowance. It is
	// a spaced slash rather than the surface's usual ` · ` because the two figures
	// are ONE clause — a fraction — and the middle dot is what this surface puts
	// between clauses; a dot here would read as five segments where there are
	// four.
	pulseAllowanceGap = " / "
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
	// So it takes the second tier — one rung above the margin it shares the line
	// with, so it still reads as the line's head — and keeps the WEIGHT, which is
	// what says "this is the title" on a sixteen-colour terminal that has no rungs
	// to spend.
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
// EACH SEGMENT IS PAINTED FOR WHAT IT MEANS AND NOT FOR WHERE IT SITS, and on a
// place there are exactly three meanings that get a colour (styles.go's THE
// ONE-ACCENT LAW): a person is being waited on, something is in flight, and this
// is money. The pulse happens to be the one line on the surface that can carry
// all three at once, which is why the design put them here — it is the whole
// machine in four clauses.
func (a *app) pulseSegments(now time.Time, pal palette) []string {
	facts := a.machineFactsAt(now)
	var out []string
	if facts.wants > 0 {
		// AMBER, AND THE WHOLE CLAUSE. The count and the words are one fact —
		// "two things have stopped and will not move until you look" — and the
		// design paints that fact in one colour wherever it appears. It is the
		// loudest thing this line can say and it is the first thing on it, which
		// is the sort order of the list underneath said in one segment.
		out = append(out, pal.warn(itoa(facts.wants)+pulseWantWord))
	}
	if facts.hands > 0 {
		// CYAN, AND THE WHOLE CLAUSE, for the same reason: in flight is one fact.
		//
		// AND IT DRAWS AT ONE. The emptiness law asks for the absence of a zero
		// and nothing more: a single hand out is worth knowing from across a room,
		// and `1 moving` is a fact where `0 moving` would be a permanent reminder
		// that nothing is happening.
		out = append(out, pal.accent(itoa(facts.hands)+pulseMovingWord))
	}
	if facts.spent > 0 {
		// GREEN, BECAUSE IT IS MONEY, and green on a place is money and nothing
		// else (styles.go's [hueMoneyPlace]).
		//
		// THE CEILING IS DRAWN ONLY WHERE THERE IS ONE. A machine with no
		// allowance set has no denominator, and `$0.55 / ` with nothing after it
		// would be the emptiness law broken in the most literal way available —
		// so the fraction collapses to the figure, which is what this line said
		// for four waves anyway.
		figure := dollars(facts.spent)
		if facts.ceiling > 0 {
			figure += pulseAllowanceGap + dollars(facts.ceiling)
		}
		out = append(out, placeMoneyInk(pal)(figure))
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
