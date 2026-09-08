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
//   - THE MONEY ON THIS LINE IS THE MONEY ON THE SPEND PLACE, and it is ONE
//     FUNCTION rather than two that agree. Every segment comes off
//     [app.machineFactsAt], and the day's figure inside it comes off the usage
//     ledger through [spendDayTotal] — the same arithmetic over the same rows that
//     draws `today $3.42 of $500` in the body of the spend place and `today` on
//     Settings→Spending. This line used to sum the task records hanging off home's
//     own world instead, so it drew `$1.85 / $500.00` over a spend place drawing
//     `today $0.13 of $500` on the same frame, and which of the two you were shown
//     depended on which rooms you had walked through. A top line that argues with
//     the body under it is worse than no top line.
//
//   - AND NO SEGMENT IS A FACT ABOUT A SCREEN. A place a person walked out of is
//     not a source of facts about the machine: the figures here are read from the
//     machine itself, on this line's own three-second beat, and [homeView] holds
//     the memo of that reading and never the reading.
//
//   - EVERY SEGMENT OBEYS THE EMPTINESS LAW. Nothing stopped on anybody draws no
//     `want you` clause — not `0 want you` — nothing in flight draws no `moving`,
//     and a day that has cost nothing says nothing about money. SCREEN 1c is that
//     law taken all the way and it is a real frame this file has to be able to
//     draw: a quiet morning is the name and the clock, and nothing else.
//
//   - THE CLOCK IS NEVER EMPTY AND IS ALWAYS THE FIRST TO GIVE WAY, which are
//     two different laws that used to be written as one. The time is never
//     absent and never zero, so on a screen with nothing else to say it is the
//     one segment left and it draws; but it is also the LOWEST-RANKED thing on
//     the line, and a frame too narrow for every segment sheds it before it
//     sheds a word about the machine. (What this law used to say was "THE CLOCK
//     ALWAYS DRAWS", and it was read as a width law as well as an emptiness law,
//     which is how sixty columns came to spend twelve cells on `thu 12:01am`
//     while the whole right end went unwritten — see [app.pulseRungs].)
//
//   - THE ALLOWANCE IS A FRACTION HERE, AND THE OWNER OVERRULED THIS FILE TO PUT
//     IT THERE. What stood here for four waves was the opposite law, and it read:
//     "NEVER A QUOTA FRACTION HERE. What the day has spent belongs on the pulse;
//     what the day is ALLOWED to spend is the machine card's `today` band and
//     nowhere else. A ceiling on this line would turn a glance into an arithmetic
//     problem." (That band is retired with home's resting state and this line is
//     the only one left that draws either figure.) The design draws
//     `$0.55 / $20.00`, the owner said "follow the
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
//
// AND THE SEGMENTS GIVE WAY ONE AT A TIME, BY RANK. This line used to be drawn
// ALL OR NOTHING: everything, or the name alone. So a sixty-column frame — a
// split pane, an ssh session from a train — spent twelve of its cells on
// `thu 12:01am` and then, one segment later, threw the whole right end away and
// said nothing about the machine at all. It walks [app.pulseRungs] now, which is
// [rowfit.go]'s ranked-prefix law applied to this line: the widest rung that
// fits is the one drawn, and what a narrow frame shows is a SUBSET of what a
// wide one shows rather than a different line.
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
	// AND IT IS THE ONE NAME, READ OFF THE ONE CONSTANT (styles.go's [product]).
	// This line used to hold a second spelling of its own (`pulseName`), which is
	// how the surface came to greet a fresh install with `openaf` in the wordmark
	// and `aforge` in the prose under it.
	name := " " + pal.bold(pal.muted(product))
	for _, tail := range a.pulseRungs(a.now(), pal) {
		if tail == "" {
			break
		}
		gap := width - ansi.StringWidth(name) - ansi.StringWidth(tail) - 1
		if gap >= 1 {
			return name + strings.Repeat(" ", gap) + tail
		}
	}
	return name
}

// pulseRungs is every line the right end of the pulse is allowed to be, WIDEST
// FIRST, and it is where this file's ranking is written down.
//
// ── THE RANK, AND THE ARGUMENT FOR IT ──
//
// From the top of the ladder down, the order things are given up in is: the
// clock, then the day's ALLOWANCE, then the day's SPEND, then what is moving,
// and the count of what has stopped on a person is the last thing on the line to
// go. THE ARGUMENT IS ONE SENTENCE: the terminal's own bar, the window manager
// and the wall clock all say what time it is, and nothing anywhere else on this
// machine says that two pieces of work have stopped and will not move until
// somebody looks — so a cell that could carry either carries the one that is
// only available here, and the whole ladder falls out of ranking the segments by
// how much a person could have learned that fact any other way.
//
// Within that, `2 want you` outranks `4 moving` because a thing that has stopped
// needs a person and a thing in flight does not, which is the sort order of the
// list underneath this line said again; and the spend outranks the allowance
// because a figure is a fact and a fraction is that fact plus a bound, so the
// bound is the half that can go while the clause still says something true.
//
// ── AND A DROPPED SEGMENT MAY NOT MAKE THE LINE LIE ──
//
// This is the constraint that shapes the money rungs. The allowance is dropped
// by RESPELLING the money clause — `$0.55 / $20.00` becomes `$0.55`, which is
// what this line said for four waves and is true — and the spend is dropped by
// removing the clause whole, which leaves no `$` on the line at all. What must
// never happen is a narrow frame drawing `$0.00`, or a `/ $20.00` with nothing
// in front of it: either would be the line reporting a figure it had actually
// given up on. (The ONE sanctioned `$0.00` on this surface is the live status
// line of a conversation, so its segments do not jump sideways as money arrives
// — that is what [dollars] returning `$0.00` at zero is for. It is not this
// line, and this line never borrows the exception: the emptiness law keeps a
// zero day off the pulse ([app.pulseParts]) and the ladder never puts one back.
func (a *app) pulseRungs(now time.Time, pal palette) []string {
	p := a.pulseParts(now, pal)
	rung := func(parts ...string) string {
		var kept []string
		for _, part := range parts {
			if part != "" {
				kept = append(kept, part)
			}
		}
		return strings.Join(kept, pulseGap)
	}
	// The rungs, in the order the ladder is climbed down. Two neighbours can come
	// out identical — a machine with no allowance set has one money spelling, a
	// quiet day has none — and a rung that is the same string as the one above it
	// simply fails the same measurement twice, which costs nothing and keeps the
	// ladder readable as the list of decisions it is.
	return []string{
		rung(p.wants, p.hands, p.money, p.clock),
		rung(p.wants, p.hands, p.money),
		rung(p.wants, p.hands, p.spend),
		rung(p.wants, p.hands),
		rung(p.wants),
		"",
	}
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
//
// It is the TOP RUNG of [app.pulseRungs] and it is built out of the same pieces,
// so the widest line this file can draw and the line the ladder starts from can
// never come to disagree about how a segment is spelled.
func (a *app) pulseSegments(now time.Time, pal palette) []string {
	p := a.pulseParts(now, pal)
	var out []string
	for _, part := range []string{p.wants, p.hands, p.money, p.clock} {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// pulseParts is every clause the top line can carry, each already painted and
// each "" where the emptiness law says it is not true. It is ONE FUNCTION rather
// than two so that [app.pulseSegments] and [app.pulseRungs] cannot drift.
//
// `money` and `spend` are the two spellings of one clause — the fraction and the
// figure — and they are the ladder's way of giving up the allowance without
// giving up the day's bill.
type pulseParts struct{ wants, hands, money, spend, clock string }

func (a *app) pulseParts(now time.Time, pal palette) pulseParts {
	facts := a.machineFactsAt(now)
	var p pulseParts
	if facts.wants > 0 {
		// AMBER, AND THE WHOLE CLAUSE. The count and the words are one fact —
		// "two things have stopped and will not move until you look" — and the
		// design paints that fact in one colour wherever it appears. It is the
		// loudest thing this line can say and it is the first thing on it, which
		// is the sort order of the list underneath said in one segment.
		p.wants = pal.warn(itoa(facts.wants) + pulseWantWord)
	}
	if facts.hands > 0 {
		// CYAN, AND THE WHOLE CLAUSE, for the same reason: in flight is one fact.
		//
		// AND IT DRAWS AT ONE. The emptiness law asks for the absence of a zero
		// and nothing more: a single hand out is worth knowing from across a room,
		// and `1 moving` is a fact where `0 moving` would be a permanent reminder
		// that nothing is happening.
		p.hands = pal.accent(itoa(facts.hands) + pulseMovingWord)
	}
	if facts.spent > 0 {
		// GREEN, BECAUSE IT IS MONEY, and green on a place is money and nothing
		// else (styles.go's [hueMoney]).
		//
		// AND IT IS THE DAY THE WHOLE MACHINE HAD, not this conversation's: every
		// model call written down since midnight, wherever it was made — the chat
		// in front of the person, a task running behind it, a standing order that
		// fired at six.
		//
		// THE CEILING IS DRAWN ONLY WHERE THERE IS ONE. A machine with no
		// allowance set has no denominator, and `$0.55 / ` with nothing after it
		// would be the emptiness law broken in the most literal way available —
		// so the fraction collapses to the figure, which is what this line said
		// for four waves anyway. A NARROW FRAME COLLAPSES IT THE SAME WAY, which
		// is why the two spellings are both kept here rather than the second one
		// being reconstructed by the ladder.
		figure := dollars(facts.spent)
		p.spend = placeMoneyInk(pal)(figure)
		p.money = p.spend
		if facts.ceiling > 0 {
			// AND THE LIMIT IS SPELLED THE WAY EVERY OTHER SURFACE SPELLS IT.
			// The two halves of this fraction are not the same kind of fact: the
			// left is a MEASUREMENT and keeps [dollars], and the right is a figure
			// somebody TYPED, which [railFigure] writes whole when it is whole
			// (settingspend.go states that law). Spelling it here with [dollars]
			// put `$500.00` on the first line of the product against `$500` on
			// every page that says the same number.
			p.money = placeMoneyInk(pal)(figure + pulseAllowanceGap + railFigure(facts.ceiling))
		}
	}
	if clock := pulseClock(now); clock != "" {
		p.clock = pal.dim(clock)
	}
	return p
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
