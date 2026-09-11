package tui3

// WHAT THIS MACHINE HAS TO SAY ABOUT ITSELF, IN ONE READING.
//
// The pulse line at the top of every place says it in four clauses — how many
// things have stopped on a person, how many are in flight, what the day has
// cost, and the time (pulse.go) — and this file is where those numbers come
// from.
//
// IT USED TO FEED A CARD AS WELL. Home had a resting state: the cursor walked
// up off the top of the list onto no row at all, and the right-hand column
// became a card about the machine — what was keeping an eye on things, what had
// happened since you left, what the day had come to. That state is retired.
// `↑` off the top row reaches the TAB BAR now (pages.go's [barCursor]), which is
// a row a person can walk along and open a room from, and the three questions
// the card answered each have a room of their own on that bar: standing, the
// `since you left` lines home draws in its own list, and spend. So the facts
// stayed and the second surface went, which is one reading feeding one line
// rather than one reading feeding two things that could drift apart.
//
// ── AND IT NEVER BLOCKS, AND A DRAW NEVER TAKES IT ──
//
// The reading is taken on a beat and at a door — home's three-second beat and
// every place's ([homeEvery]), the pulse's own ten-second beat while no home is
// open (pulsebeat.go), and [app.showPage] on the way into any room — and left
// in [app.machine]. The pulse is drawn on every frame and reads that memo and
// nothing else, so a frame costs no file, however often it is painted.

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// machineFacts is the whole of what this machine has to say about itself, as
// the pulse line reads it.
//
// EVERY FIELD IS ZERO WHEN IT IS NOT TRUE, because the emptiness law is enforced
// by the READING and not by four separate drawing decisions: a machine with no
// allowance has no ceiling, a day with no work has cost nothing, and each of
// those absences reaches the pulse as one absent segment.
type machineFacts struct {
	// spent is what the day's work cost, in dollars, and ceiling is the
	// machine-wide daily allowance it is spending against — zero for a machine
	// that has none.
	spent   float64
	ceiling float64
	// hands is HOW MANY THINGS THIS MACHINE HAS IN FLIGHT RIGHT NOW, across
	// every project and every kind of thing: a task node out, a conversation
	// mid-turn, an errand answering, a standing order firing.
	//
	// IT IS THE LIST'S OWN ARITHMETIC AND NOT A SECOND ACCOUNTING
	// ([machineCounts] counts the world the list is ranked from). The rows the
	// switcher calls moving are the same things this figure counts, and a count
	// derived a second way here would be a top line saying `3 working` over a
	// list showing four, which is the exact failure the one-reader law was
	// written against.
	hands int
	// wants is HOW MANY THINGS ON THIS MACHINE HAVE STOPPED ON A PERSON RIGHT
	// NOW: a conversation waiting for an answer, a standing order that will not
	// fire until somebody says so, an errand holding a question.
	//
	// IT IS [machineFacts.hands]' MIRROR AND IS COUNTED THE SAME WAY, off the same
	// three worlds in the same walk ([machineCounts]). The two are the whole
	// of the switcher's sort order said as two numbers — SCREEN 2b ranks the one
	// list by "what wants you first", then what is moving — so a pulse whose two
	// counts came from anywhere else would be a headline over somebody else's
	// article.
	wants int
}

// machineCeilingNear is how much of the day's allowance has to be gone before
// the figure stops being an ordinary dim fact.
//
// FOUR FIFTHS IS WHERE A BOUND STARTS TO MATTER. [hueWarn] exists for exactly
// this shape — a bound that is about to be reached, which is not a failure and
// must not wear the failure hue — and the honest moment to raise the figure out
// of the dim is when there is still enough left to do something about it.
const machineCeilingNear = 0.8

// nearCeiling reports that the day's spend is close enough to the machine's
// allowance to be worth a colour. It is ONE PREDICATE and one caller: the
// pulse raises its spend segment on it rather than drawing the figure in the
// ordinary dim, which is the one thing on that line that is allowed to change
// colour because of arithmetic.
func (f machineFacts) nearCeiling() bool {
	return f.ceiling > 0 && f.spent >= f.ceiling*machineCeilingNear
}

// readMachine is the whole reading, taken over one world: the day's money off
// the ledger and the two counts off the world's rows and the standing bands
// read with it. It runs on a beat and never on a draw.
func (a *app) readMachine(now time.Time, sessions []session.SessionRow, bands map[string][]StandingItemView) {
	a.readMachineMoney(now)
	a.machine.hands, a.machine.wants = machineCounts(now, sessions, bands, a.exchanges)
}

// readMachineMoney is the money half alone: what the day has cost, and the
// allowance it is spending against. It is the half every place's beat can take
// by itself, because the ledger is one file and the counts need a world.
//
// THE COUNTS ARE LEFT AS THE LAST WORLD LEFT THEM. A place's beat has no world
// of its own to count, and zeroing them here would draw `2 want you` on one
// beat and nothing on the next.
func (a *app) readMachineMoney(now time.Time) {
	a.machine.ceiling = a.machineAllowance()
	a.machine.spent = a.machineSpentToday(now)
}

// machineCounts is how many things this machine has in flight and how many
// have stopped on a person, counted off one reading of the world.
//
// ONE ROW IS NOT ALWAYS ONE HAND. A conversation with three task nodes out is
// one row and three things being done, so it counts three. A row with no count —
// a conversation merely mid-turn, an errand answering, an order firing — is one
// hand: something IS being done there, and the machine has no finer number for
// it than "this".
//
// BUT ONE ROW IS ONE WANT. A conversation that has asked you something is ONE
// question however many nodes it has parked behind it, because what a person
// does about it is answer it once. The count is a count of decisions waiting,
// not of work waiting.
//
// IT IS COUNTED OFF THE READING AND NOT OFF ANY COLUMN. A panel caps what it
// draws and these figures are about the MACHINE, so a count taken from the rows
// on screen would fall the moment a ninth thing started — which is the opposite
// of what the figure means.
func machineCounts(now time.Time, sessions []session.SessionRow, bands map[string][]StandingItemView, exchanges []*homeExchange) (hands, wants int) {
	for _, row := range sessions {
		// AND A LANDING NOBODY HAS CHECKED IS A WANT TOO, counted through the
		// panel's own reading of it (homepanel_needs.go's [needsCallOf] and
		// [needsAged]) rather than through a second test here. It is what makes
		// `N want you` the sum of the two groups of `needs you`: the count and
		// the rows are the same arithmetic, so a pulse over a home can no longer
		// claim a number the panel under it does not show.
		wants += machineChecks(now, row)
		switch {
		case row.Archived:
		case row.NeedsPerson():
			wants++
		case row.Tasks.Running > 1:
			hands += row.Tasks.Running
		case row.Tasks.Running == 1 || row.Live && row.Presence.State == session.PresenceWorking:
			hands++
		}
	}
	// ONE ITEM IS ONE HAND, OR ONE QUESTION, HOWEVER MANY PROJECTS HOLD IT. A
	// machine-wide watch is in every project's band ([app.readStandBands] keys
	// them by directory), and a walk that did not remember what it had counted
	// would multiply it.
	counted := make(map[string]bool)
	for _, views := range bands {
		for _, view := range views {
			if counted[view.Item.ID] {
				continue
			}
			counted[view.Item.ID] = true
			switch {
			case strings.TrimSpace(view.Item.NeedsPerson) != "":
				wants++
			case view.Running:
				hands++
			}
		}
	}
	for _, ex := range exchanges {
		switch {
		case ex.waiting():
			wants++
		case ex.working:
			hands++
		}
	}
	return hands, wants
}

// machineChecks is how many of one conversation's landings are waiting to be
// checked — the `to check` group's rows for that conversation, counted with the
// group's own reading.
//
// AN ARCHIVED CONVERSATION IS NOT COUNTED, for the same reason its own row is
// not: it has been put away, and work put away is not waiting on anybody.
func machineChecks(now time.Time, row session.SessionRow) int {
	if row.Archived {
		return 0
	}
	n := 0
	for i := range row.Tasks.Rows {
		entry := row.Tasks.Rows[i]
		if _, ok := needsCallOf(row, entry); !ok || needsAged(needsCallAt(entry), now) {
			continue
		}
		n++
	}
	return n
}

// machineSpentToday is WHAT THIS MACHINE HAS SPENT TODAY, and it is the usage
// ledger's answer — the same reading, through the same arithmetic
// ([spendDayTotal]), that the spend place draws as `today $3.42 of $500`.
//
// THE DAY IS THE PERSON'S OWN DAY and not twenty-four hours: since midnight
// where they are sitting, which is what every other reading of the day on this
// surface already means by it.
//
// THERE IS ONE READING BECAUSE TWO WERE TWO ANSWERS. This function used to sum
// the task records under home's own world and add the standing ledger to them,
// which was a THIRD accounting of money the ledger already holds: the top line
// drew `$1.85 / $500.00` over a spend place drawing `today $0.13 of $500` on the
// same frame, fourteen times apart against one denominator, and a person could
// not tell which of the two was their bill. The ledger is the only reading that
// sees every call — internal/session writes a row as each call's bill is
// decoded, interrupted turns included (docs/changes/unreleased's
// 290-one-ledger-one-number) — so the second reading is gone rather than made to
// agree, because two readings that agree today are two readings that disagree
// later.
//
// A LEDGER NOBODY CAN READ COSTS NOTHING RATHER THAN ZERO. Over a connection the
// far machine may not have answered yet ([app.usageSince] carries the seam's own
// "not known"), and the emptiness law renders an unknown as an absent segment
// and never as `$0.00`.
func (a *app) machineSpentToday(now time.Time) float64 {
	day := machineDayStart(now)
	if day.IsZero() {
		return 0
	}
	lines, known := a.usageSince(day)
	if !known {
		return 0
	}
	return spendDayTotal(lines, now)
}

// machineDayStart is midnight, locally.
func machineDayStart(now time.Time) time.Time {
	if now.IsZero() {
		return time.Time{}
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// machineAllowance is the machine-wide daily allowance everything on it spends
// against — the person's own daily budget row, which is the rail a firing is
// held to as well (cmd/aforge's v3StandingDailyRail).
//
// IT IS ONE SETTING READ IN ONE PLACE, and the pulse is the one line that draws
// it — as the denominator under what has been spent, and only where a machine
// has an allowance at all (pulse.go's [app.pulseSegments]).
func (a *app) machineAllowance() float64 {
	rail, err := config.DailyBudgetUSDAt(a.profileDir)
	if err != nil || rail <= 0 {
		return 0
	}
	return rail
}
