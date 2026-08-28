package standing

// WORDS: the three sentences a page says about a standing item, made here so
// that every surface says them the same way.
//
// They live in this package and not in a surface for [taskStateWord]'s reason
// one package over: three screens ask the same question of the same item — the
// standing page, home's card, and whatever asks next — and three spellings of
// "nothing had changed" is three chances for two screens to disagree about one
// promise. Each is a PURE FUNCTION of an item a caller is already holding: no
// store, no clock it was not handed, nothing that can block a draw.
//
// The register is the one this package already writes in. `checked`, `fired`,
// `nothing` and `needs your look` are words the surface uses today
// (internal/tui3's standRollup), and no machinery word — sentinel, probe,
// outcome, tick — reaches a screen through here.

import (
	"strconv"
	"strings"
	"time"
)

// LastLookLine is what this item did the last time it looked, in one sentence.
//
// It is the line screen 2f draws under a card, and the whole reason it can be
// written at all is that the item keeps the three facts a firing leaves behind:
// [Item.LastFired], [Item.LastOutcome], and [Item.LastCheckLine] — the field
// whose own comment says it exists so *a watch that checked faithfully for
// thirty mornings and found nothing reads differently from one that never ran*.
//
// THIS IS THE ONE HONEST ROUTE TO THAT SENTENCE. The inbox cannot deliver it: a
// run that came to nothing writes NO note anywhere, by contract (standing.go's
// QUIET IS THE DESIGN), so the line the design most wants is precisely the line
// the inbox refuses to carry. It is composed from the item instead, which every
// surface already holds.
//
// AN ITEM THAT HAS NEVER BEEN LOOKED AT SAYS NOTHING — the emptiness law, and
// the same nothing a [WhenHold] rule always says, since nothing examines a rule
// and it will never have a look to report.
//
// The item's OWN WORDS lead wherever it has them. `LastCheckLine` is written by
// the thing that looked — "it was the time you asked for", a sentinel's own
// sentence, "could not check: …" — and a page that replaced it with a phrase of
// its own would be throwing away the only account of what was actually seen.
func LastLookLine(item Item, now time.Time) string {
	last := item.LastChecked
	if item.LastFired.After(last) {
		last = item.LastFired
	}
	if last.IsZero() {
		return ""
	}
	found := oneLine(item.LastCheckLine)
	// THE LAST LOOK DID NOT FIRE, which is the commonest thing that happens to a
	// watch and the whole sentence the mockup was written around. It is knowable
	// exactly: a firing stamps both instants at once (tick.go's fire), so a check
	// later than the last firing is a check that came to nothing.
	if item.LastFired.IsZero() || item.LastFired.Before(item.LastChecked) {
		if found == "" {
			// "nothing" IS A FINDING and the most common one there is — the
			// difference between a watch that is working and one that never ran.
			// The row's own spelling of it is the bare word (internal/tui3's
			// standRollup); a sentence needs the whole clause.
			found = "nothing had changed"
		}
		return "looked " + agoWord(last, now) + " · " + found + ", so nothing was done"
	}
	line := "fired " + agoWord(last, now)
	if found != "" {
		line += " · " + found
	}
	if came := outcomeClause(item.LastOutcome); came != "" {
		line += " — " + came
	}
	return line
}

// outcomeClause is what came of a firing, in words. An outcome nobody recorded
// — an item that fired under a build before outcomes were kept — says nothing
// rather than guessing, and an outcome this build does not know says nothing
// rather than printing itself at somebody.
func outcomeClause(outcome string) string {
	switch strings.TrimSpace(outcome) {
	case "said":
		return "it told you"
	case "landed":
		return "it did the work"
	case OutcomeNeedsYou:
		return "it needs your look"
	case OutcomeFailed:
		return "it could not finish"
	case OutcomeNothing:
		return "it came to nothing"
	}
	return ""
}

// agoWord is how long ago something happened, on the same ladder the surface
// already draws ages on (internal/tui3's sinceAt): minutes, then hours, then
// days, then a plain date once the distance has stopped being the point.
//
// It is here rather than borrowed because a data package must not import a
// surface, and because these three sentences are the only things in this
// package that need it. The ladder is the same on purpose: a card that said
// "3h ago" over a row that said "3h" would be two clocks on one screen.
func agoWord(at, now time.Time) string {
	if at.IsZero() || now.IsZero() {
		return ""
	}
	distance := now.Sub(at)
	switch {
	case distance < time.Minute:
		return "just now"
	case distance < time.Hour:
		return strconv.Itoa(int(distance/time.Minute)) + "m ago"
	case distance < 24*time.Hour:
		return strconv.Itoa(int(distance/time.Hour)) + "h ago"
	case distance < 30*24*time.Hour:
		return strconv.Itoa(int(distance/(24*time.Hour))) + "d ago"
	}
	return "on " + strings.ToLower(at.Format("Jan 2"))
}

// The words the rope column is allowed to say, and no others.
const (
	// RopeAsksFirst is an item that has been given no licence to act unasked.
	RopeAsksFirst = "asks first"
	// RopeTrusted is an item with a grant that has since fired [TrustAfter]
	// times in a row without needing anybody.
	RopeTrusted = "trusted alone"
	// ropeEarning leads the middle rung, which finishes with the count.
	ropeEarning = "earning trust "
)

// TrustAfter is how many clean firings in a row earn an item the top rung. It
// is exported because it is the denominator a person reads — `earning trust
// 3/5` — and a page that spelled the 5 itself would be a second answer to a
// number this package owns.
const TrustAfter = 5

// RopeWord is HOW MUCH ROPE this item has, in three rungs: [RopeAsksFirst],
// `earning trust 3/5`, or [RopeTrusted]. It is the only derivation of that
// answer in the program, and every surface that draws the column reads it.
//
// ── THE RULE, AND WHY IT IS THIS ONE ──
//
// [Item.Grant] is the only record in the whole program of a person letting a
// standing item act unattended: one sentence, in their own words, of *what
// acting on this may do without asking* — "open a pull request but never merge
// it" — written only when they actually said something like it, and its own
// documentation finishes the thought: **with none, it may only tell them
// things**. So a grant is the FLOOR: with none, nothing has been agreed and
// anything past telling somebody things is theirs to allow.
//
// THIS IS THE OPPOSITE POLARITY TO THE ONE THE PLAN'S PARENTHETICAL ASSUMED.
// docs/design/home-rethink/LANES.md decision 5 glosses "asks first" as *the
// item's grant names something it must ask for*, which reads the field
// backwards: a grant names what it may do WITHOUT asking, so a grant is more
// rope and not less.
//
// AND ABOVE THE FLOOR THERE ARE TWO RUNGS AND NOT ONE, WHICH IS A DECISION AND
// NOT A DERIVATION. This function argued at length, and until 2026-08-25
// correctly, that there was no third rung — quoting standing.go's own *there
// are no probation counters: the rules are the tenure*. The owner has since
// ordered the counter ("follow the exact design please", recorded in
// docs/design/home-rethink/FIDELITY.md item 6), so the counter exists: an item
// is trusted alone once [Item.CleanRuns] reaches [TrustAfter], and until then
// the column says how far along it is. The old argument is gone rather than
// left standing beside code that contradicts it — a comment arguing against the
// function under it is worse than no comment at all.
//
// The design's own word is *tenured*. THE WORD SHIPPED IS *trust*, and the
// reason is not the one FIDELITY gives. That doc says the resident-separation
// test bans "tenure"; it does not — internal/tui3's manual_test.go bans five
// phrases and that is not among them, and internal/manual/chat/commands.md
// already ships the words "tenure after" for a settings row. The true reasons
// are better ones. First, *tenure* is the RESIDENT's own name for this exact
// mechanism — config.KeyTenureAfter, internal/resident/tenure.go, a charter
// earning tenure after so many clean firings — and CLAUDE.md's rule about the
// two products is that vocabulary does not travel between them, whether or not
// a test happens to catch a given word. Second, it is an employment term for a
// thing a person thinks of as trust. FIDELITY records this as a deviation in
// WORDING ONLY, which is right; only its stated cause needs correcting.
//
// AND THE RESIDENT'S THRESHOLD IS NOT THIS ONE. config.TenureAfterAt is a
// setting over CHARTERS in that other product; [TrustAfter] is a constant over
// standing items in this one. They are the same idea about different objects,
// so this package does not read that setting — a data package reaching into the
// other product's configuration to answer a question about its own records
// would be the assumption-carrying CLAUDE.md forbids.
//
// WHAT IS DELIBERATELY NOT IN THE RULE, because both would make the column say
// something it does not mean:
//
//   - [Item.NeedsPerson] is a STATE and not rope. It is set while the latest
//     run is stopped on a question and cleared by the next firing, so an item
//     that walked into something this morning would flip its RUNG twice in a
//     day — and how much rope a thing has is not a thing that changes while you
//     are asleep. It does reset [Item.CleanRuns], which is a different claim:
//     the streak starts again, and the column says so by counting from nothing
//     rather than by changing what it means.
//   - [Item.Exceptions] narrow WHERE an item reaches — a workspace it skips, a
//     conversation it stays out of — and not what it may do when it does reach.
//     An item that runs everywhere but one repository has the same rope in the
//     repositories it does run in.
func RopeWord(item Item) string {
	if strings.TrimSpace(item.Grant) == "" {
		// The commonest answer, and the most strongly true one: an item with no
		// grant and a say action cannot act at all, so there is nothing it could
		// do unasked even if it wanted to.
		return RopeAsksFirst
	}
	if item.CleanRuns >= TrustAfter {
		return RopeTrusted
	}
	// A COUNT PAST THE THRESHOLD CANNOT BE DRAWN, so it cannot be reached: the
	// clamp above answers every value at or over it, and what is left is the
	// range the fraction can honestly print.
	return ropeEarning + strconv.Itoa(max(item.CleanRuns, 0)) + "/" + strconv.Itoa(TrustAfter)
}

// moneyFloor is where rounding to cents turns a real figure into "$0.00", which
// reads as free. It is half a cent because the rounding is round-half-up, and
// the number is written down rather than assumed so that a change to the
// formatter fails a test instead of quietly bringing the bug back.
//
// The failure it came from is on the record, in internal/tui2's homes/standing.go:
// a model once wrote "$20.00 a run" beside a measured $0.0017, and the surface
// let it stand. A figure a page cannot state at the precision it has is stated
// in WORDS instead, which is true at every magnitude and cannot be misread as
// nothing.
const moneyFloor = 0.005

// CostPerRunWord is what one firing of this item costs, in words: "under a
// cent", "$0.31", or NOTHING AT ALL where nobody measured it.
//
// It is the rate and not the total: [Spend.USD] over [Spend.Fired], which is the
// only per-firing figure this package can honestly produce. Two things about
// that are worth a caller knowing, because both make the figure an over-estimate
// rather than an under-estimate:
//
//   - A CHECK'S MONEY IS IN THE NUMERATOR AND ITS FIRING IS NOT. [Spend.count]
//     adds what a quiet check cost and does not count it as a firing, because a
//     check that came to nothing is not a firing — so an item that looks thirty
//     times to fire twice carries thirty looks' worth of sentinel calls across
//     two firings.
//   - THERE IS NO EXACT LAST-FIRING COST. Nothing on the item records what the
//     last firing alone came to, and every exported reader of the daily ledger
//     sums rather than handing back rows, so an average is the honest answer and
//     the only one.
//
// NOTHING FIRED IS NOTHING MEASURED, and so is a run nobody could price: both
// answer "" rather than "$0.00", by the emptiness law — zero here means nobody
// could say, never that the work was free.
//
// The word is the FIGURE alone. "a run" is the page's to append, because the
// same rate reads as "under a cent a run" in a sentence and as a bare cell in a
// column, and a formatter that decided which was a formatter drawing the page.
func CostPerRunWord(spend Spend) string {
	if spend.Fired <= 0 || spend.USD <= 0 {
		return ""
	}
	rate := spend.USD / float64(spend.Fired)
	if rate < moneyFloor {
		return "under a cent"
	}
	return moneyWord(rate)
}

// moneyWord is a figure at the precision cents have. It is one function so that
// [CostPerRunWord] and anything that follows it cannot come to round money two
// ways, and it is never asked to render a sub-cent figure — that is what
// [moneyFloor] is for.
func moneyWord(usd float64) string {
	return "$" + strconv.FormatFloat(usd, 'f', 2, 64)
}
