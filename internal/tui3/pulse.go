package tui3

// THE PULSE: THE FAR END OF THE LINE AT THE TOP OF EVERY FRAME.
//
//	 >● codeaf   home  teams  chats …   2 want you · 4 moving · $0.55 / $20.00 · tue 1:11pm
//	 >● codeaf   home  teams  chats …                     $0.55 / $20.00 · tue 1:11pm
//
// The program's name and the places on the left (topnav.go), and right-aligned
// on the other end, quiet unless it has a reason not to be, the machine's own
// vital signs. The first
// line is a conversation's and every place's; the second is home's, which leaves
// its counts to the panels under it (DESIGN.md's law 11, [pulseBudget]). It is the
// watch made visible (docs/HOME-BRIDGE.md): a person who has just sat down learns
// in one glance how many things are stopped on them, how many are moving, what
// the day has cost against what it is allowed, and what time it is, without
// reading a single row.
//
// THE WORDS AND THE ORDER ARE THE DESIGN'S, EXACTLY. The retained
// docs/design/home-rethink/FIDELITY.md item 2 records the owner's instruction
// for this line, and every clause below follows that record.
//
// ── THE LAWS ──
//
//   - THE MONEY ON THIS LINE IS THE MONEY ON THE SPEND PLACE, and it is ONE
//     FUNCTION rather than two that agree. Every segment comes off
//     [app.machine], and the day's figure inside it comes off the usage
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
//     machine itself, ON A BEAT AND NEVER ON A DRAW (homemachine.go's
//     [app.readMachine]), and this line reads the memo that beat left behind.
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
//     while the whole right end went unwritten.)
//
//   - THE LADDER IS THE NAV'S NOW (topnav.go's [app.navTails]). The line shares
//     its row with the places, and the owner ruled the order the row gives
//     things up in (2026-09-24): the clock, then the counts, then the places
//     fold, and the day's figure is the last clause standing. This file used
//     to rank `2 want you` last to go, on the argument that nothing else on
//     the machine says it; a place's own rows and the strip's marks now say
//     it on every page, and the places are how a person gets to them.
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

// pulseMode is which of the line's two readings a frame draws. It is ONE
// PARAMETER over ONE SET OF CLAUSES ([app.pulseParts]), so the two frames can
// differ in what they leave out and never in how a clause is spelled.
type pulseMode uint8

const (
	// pulseWhole is every clause — the two counts, the budget and the clock. It
	// is the line over a conversation and over every place but home: inside a
	// chat nothing else on the frame says that two things have stopped on you.
	pulseWhole pulseMode = iota
	// pulseBudget is the budget and the clock alone, and it is home's. HOME IS
	// THE SUMMARY OF THE TABS and its panels ARE the counts — `needs you` over
	// the very rows that need you — so a `2 want you` above them would be the
	// same news said twice on one frame (DESIGN.md's laws 10 and 11).
	pulseBudget
)

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
// It is the TOP RUNG of the nav's ladder ([app.navTails]) and it is built out of
// the same pieces, so the widest line and the line the ladder starts from can
// never come to disagree about how a segment is spelled.
func (a *app) pulseSegments(now time.Time, pal palette) []string {
	p := a.pulseParts(now, pal, pulseWhole)
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
// than two so that [app.pulseSegments] and [app.navTails] cannot drift.
//
// `money` and `spend` are the two spellings of one clause — the fraction and the
// figure — and they are the ladder's way of giving up the allowance without
// giving up the day's bill.
type pulseParts struct{ wants, hands, money, spend, clock string }

func (a *app) pulseParts(now time.Time, pal palette, mode pulseMode) pulseParts {
	facts := a.machine
	if mode == pulseBudget {
		// THE COUNTS ARE LEFT OUT HERE AND NOWHERE ELSE, by zeroing them in the
		// reading rather than skipping them in the ladder: every rung below is
		// then home's line with no second list of which clauses home may carry.
		facts.wants, facts.hands = 0, 0
	}
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
