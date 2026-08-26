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
// ── AND IT NEVER BLOCKS ──
//
// Everything below is either already in the view — the world, the standing
// bands — or a single small file, and it is taken at most once per [homeEvery]
// whoever asks first. The pulse is drawn on every frame; the disk is walked on
// home's clock and nowhere else.

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
	// ([app.machineHands] counts the world the list is ranked from). The rows the
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
	// three worlds in the same order ([app.machineWants]). The two are the whole
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

// machineFactsAt is that reading, taken once per [homeEvery] and answered from
// the view in between.
//
// IT IS KEPT ON THE VIEW AND DIES WITH THE SCREEN ([homeView] is zeroed when
// home closes), which is the same bargain every other cache on this surface
// makes: the numbers are worth a walk of the disk twice a minute and never
// worth one per paint.
func (a *app) machineFactsAt(now time.Time) machineFacts {
	h := &a.home
	if !h.machineAt.IsZero() && !now.IsZero() && now.Sub(h.machineAt) < homeEvery {
		return h.machine
	}
	facts := machineFacts{ceiling: a.machineAllowance()}
	facts.spent = a.machineSpentToday(now)
	facts.hands = a.machineHands()
	facts.wants = a.machineWants()
	h.machine, h.machineAt = facts, now
	return facts
}

// machineHands is how many things this machine has in flight, counted off the
// world the list is ranked from ([readSwitcher] judges the same rows moving).
//
// ONE ROW IS NOT ALWAYS ONE HAND. A conversation with three task nodes out is
// one row and three things being done, so it counts three. A row with no count —
// a conversation merely mid-turn, an errand answering, an order firing — is one
// hand: something IS being done there, and the machine has no finer number for
// it than "this".
//
// IT IS COUNTED OFF THE READING AND NOT OFF THE COLUMN. The list caps what it
// draws ([switcherShown]) and the pulse's figure is about the MACHINE, so a
// count taken from the rows on screen would fall the moment a ninth thing
// started — which is the opposite of what the figure means.
func (a *app) machineHands() int {
	hands := 0
	for _, row := range a.home.world.Sessions() {
		if row.Archived || row.NeedsPerson() {
			continue
		}
		if row.Tasks.Running > 1 {
			hands += row.Tasks.Running
			continue
		}
		if row.Tasks.Running == 1 || row.Live && row.Presence.State == session.PresenceWorking {
			hands++
		}
	}
	// ONE ITEM IS ONE HAND HOWEVER MANY PROJECTS HOLD IT. A machine-wide watch is
	// in every project's band ([app.readStandBands] keys them by directory), and a
	// walk that did not remember what it had counted would multiply it.
	counted := make(map[string]bool)
	for _, views := range a.home.items {
		for _, view := range views {
			if !view.Running || strings.TrimSpace(view.Item.NeedsPerson) != "" || counted[view.Item.ID] {
				continue
			}
			counted[view.Item.ID] = true
			hands++
		}
	}
	for _, ex := range a.home.exchanges {
		if ex.working && !ex.waiting() {
			hands++
		}
	}
	return hands
}

// machineWants is how many things on this machine have stopped on a person,
// counted off the world the list is ranked from — the same three walks
// [app.machineHands] makes, asking the opposite question of each.
//
// ONE ROW IS ONE THING HERE, and that is where it differs from its mirror. A
// conversation with three tasks out is three hands because three things are being
// done; a conversation that has asked you something is ONE question however many
// nodes it has parked behind it, because what a person does about it is answer it
// once. The count is a count of decisions waiting, not of work waiting.
//
// IT IS COUNTED OFF THE READING AND NOT OFF THE COLUMN, for [app.machineHands]'
// reason: the list caps what it draws and this figure is about the MACHINE, so a
// tenth thing asking would otherwise be a thing the top line did not know about.
func (a *app) machineWants() int {
	wants := 0
	for _, row := range a.home.world.Sessions() {
		if row.Archived || !row.NeedsPerson() {
			continue
		}
		wants++
	}
	// ONE ITEM IS ONE QUESTION HOWEVER MANY PROJECTS HOLD IT, which is the same
	// double-counting [app.machineHands] guards against and for the same reason:
	// a machine-wide order sits in every project's band.
	counted := make(map[string]bool)
	for _, views := range a.home.items {
		for _, view := range views {
			item := view.Item
			if strings.TrimSpace(item.NeedsPerson) == "" || counted[item.ID] {
				continue
			}
			counted[item.ID] = true
			wants++
		}
	}
	for _, ex := range a.home.exchanges {
		if ex.waiting() {
			wants++
		}
	}
	return wants
}

// machineSpentToday is what the day's work cost.
//
// THE DAY IS THE PERSON'S OWN DAY and not twenty-four hours: since midnight
// where they are sitting, which is what the standing ledger already means by it
// (internal/standing's ledger.go).
//
// WHAT THE MONEY IS MADE OF IS WHAT CAN BE HONESTLY DATED. A task carries the
// instant it landed and what it cost ([session.TaskIndexEntry]), and the
// standing ledger is a file per day; a conversation's own spend is a lifetime
// total on its meta.json with no day in it, so it is NOT split across days
// here. A figure invented by pretending a week of talking happened this morning
// would be worse than the one this leaves out.
func (a *app) machineSpentToday(now time.Time) float64 {
	day := machineDayStart(now)
	if day.IsZero() {
		return 0
	}
	spent := 0.0
	for _, project := range a.home.everyProject() {
		for _, row := range project.Sessions {
			for _, entry := range row.Tasks.Rows {
				if !entry.EndedAt.IsZero() && !entry.EndedAt.Before(day) {
					spent += entry.Cost
				}
			}
		}
	}
	return spent + a.machineStandingSpend(day)
}

// machineStandingSpend is what everything standing spent today, out of the
// ledger the ticker writes. A surface with no way to ask answers nothing, which
// is the same absence [app.standWeek] answers with.
func (a *app) machineStandingSpend(day time.Time) float64 {
	if a.stands.Runs == nil {
		return 0
	}
	total := 0.0
	for _, spend := range a.stands.Runs(day) {
		total += spend.USD
	}
	return total
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
