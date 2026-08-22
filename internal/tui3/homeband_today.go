package tui3

// `today` — WHAT THE DAY HAS COME TO, COUNTED AND PRICED.
//
//	today
//	 3 chats · 5 tasks · $1.10 of $20.00
//
// The third of the three questions somebody opens this screen to ask, and the
// only one on the card that is arithmetic. It is the local day — since midnight
// where the person is sitting — read off what home already holds
// (homemachine.go's [app.machineDay]).
//
// ── THE EMPTINESS LAW IN FULL ──
//
// A ZERO PART IS ABSENT AND AN ALL-ZERO BAND DRAWS NOTHING. A day with no work
// in it says `3 chats` and stops; a machine nobody has touched today draws no
// band at all, not `0 chats · 0 tasks · $0.00`. Three absences dressed as three
// facts is the exact shape this codebase's oldest law forbids, and the calm of
// the card depends on it: the morning glance should be nearly empty.
//
// ── AND THE CEILING LIVES HERE AND NOWHERE ELSE ──
//
// The machine-wide daily allowance — the person's own daily budget row, which is
// also the rail everything standing is held to (cmd/aforge's
// v3StandingDailyRail) — is drawn on this line and on no other. The pulse at the
// top of the screen says what has been SPENT and never a fraction
// (docs/HOME-BRIDGE.md): a glance should not be an arithmetic problem, and a
// number that appeared in two places would drift.

func init() {
	registerHomeBand(homeBand{
		name:  "today",
		order: bandOrderToday,
		kinds: []bandKind{bandKindMachine},
		draw:  drawTodayBand,
	})
}

// The words this band says, quoted in internal/manual/chat/home.md exactly as
// they are spelled here.
const (
	machineTodayWord = "today"
	machineChatWord  = " chat"
	machineTaskWord  = " task"
	// machineOfWord joins what has been spent to what the day allows.
	machineOfWord = " of "
)

func drawTodayBand(a *app, ctx bandContext) []string {
	facts := a.machineFactsAt(ctx.now)
	// THE CLAUSES ARE PAINTED BEFORE THEY ARE PACKED, so the one that means
	// something can wear a hue while the counts beside it stay calm. It is the
	// answer band's own move ([app.answerChipLines]), and [bandClauses] is given
	// the identity ink because the colour has already been decided here.
	var parts []string
	if facts.chats > 0 {
		parts = append(parts, ctx.pal.dim(itoa(facts.chats)+plural(machineChatWord, facts.chats)))
	}
	if facts.tasks > 0 {
		parts = append(parts, ctx.pal.dim(itoa(facts.tasks)+plural(machineTaskWord, facts.tasks)))
	}
	if facts.spent > 0 {
		// THE CEILING RIDES THE SPEND AND NEVER STANDS ALONE. A day that has cost
		// nothing has no line to hang an allowance on, and `$0.00 of $20.00` would
		// be the emptiness law broken to say something nobody needs to know before
		// they have spent anything.
		money := dollars(facts.spent)
		if facts.ceiling > 0 {
			money += machineOfWord + dollars(facts.ceiling)
		}
		// AND THE FIGURE RISES OUT OF THE DIM ONLY WHEN THE BOUND IS CLOSE. Money
		// spent is an ordinary fact about the day; a day four fifths of the way
		// through its allowance is a bound about to be reached, which is the one
		// shape [hueWarn] exists for — and never [hueBad], because nothing has
		// failed and nothing has been refused.
		ink := ctx.pal.dim
		if facts.nearCeiling() {
			ink = ctx.pal.warn
		}
		parts = append(parts, ink(money))
	}
	if len(parts) == 0 {
		return nil
	}
	rows := bandClauses(ctx.width, 2, func(s string) string { return s }, parts...)
	return append([]string{ctx.pal.accent(fit(machineTodayWord, ctx.width))}, rows...)
}
