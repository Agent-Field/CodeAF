package main

// seed_automations.go is the seven automations on the demo machine and the
// fortnight they have had.
//
// EVERY ROW GOES THROUGH internal/automation's OWN STORE, AND SO DOES EVERY RUN.
// [automation.Store.Create] makes each automation exactly as a card's yes does,
// and the history is the clock's own sequence of doors: Take turns a slot into a
// run and moves the automation on, Start and Finish stamp the run, SetSeen
// records what a watch decided, FinishWatch ends a once-watch that spoke, and
// SetStatus is the person's pause. The one thing a fixture adds is WHEN —
// [automation.Store.SetClock] tells those doors what time it was, because a
// history written through them at today's wall clock would be a fortnight of
// runs that all happened this second.
//
// THE CLOCK IS SIMULATED, NOT SCRIPTED SLOT BY SLOT. An automation runs only
// while a codeaf window is open, so this file says when windows were open —
// working days, a weekend away, a morning started late, an afternoon left early
// — and walks every automation through them the way the clock does: a slot that
// comes while a window is open is taken on time, and whatever passed while none
// was is caught up ONCE, late, when the next one opens. So which runs are late,
// by how much, and when each automation next wakes are the schedule's own
// arithmetic and never a figure typed here. Only what each run CAME TO is
// written down, newest first.
//
// NOTHING HERE MAY BE LEFT IN HAND. Every run is over before the fixture's now,
// and every active automation next wakes after it — a run left `running` would
// make the quit question ask about work nobody is doing, and the demo window
// starts no clock to finish it (main.go's [demoNoClock]).

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// demoAutomation is one automation, in the two halves the product writes it in:
// what the person agreed to, and what the clock did with it afterwards.
type demoAutomation struct {
	project string
	// asked is the TITLE of the conversation it was said in, which becomes its
	// [automation.Origin] — the conversation a finished run's line lands in, and
	// the door "why did this happen?" opens. Its Words must be a turn the person
	// said there, verbatim, and it is made just after that turn.
	//
	// Empty is one typed on home: no conversation and no words of its own, which
	// is a real shape, and the only one an automation older than every
	// conversation on this machine can honestly have.
	asked string
	// made is when one typed on home was made.
	made demoWhen
	item automation.Automation
	// at is a one-time automation's moment, worked out from when it was made:
	// "tomorrow at four" is tomorrow of the day it was said.
	at func(made time.Time) time.Time
	// pause is when the person paused it, if they did.
	pause *demoWhen
	// speaksAfter names the piece of work (a [demoTasks] title) whose landing
	// changed what a watch looks at: its first look after that speaks, and a
	// once-watch finishes there.
	speaksAfter string
	// took is how long one run usually takes.
	took time.Duration
	// newest is what its runs came to, newest first. first is the oldest run,
	// when that one is not like the rest — a watch's first look never is — and
	// every run between them takes rest in turn.
	newest []demoRun
	first  *demoRun
	rest   []demoRun
}

// demoWhen is a wall time some days back.
type demoWhen struct {
	daysAgo int
	clock   time.Duration
}

func (w demoWhen) on(now time.Time) time.Time { return wallOn(now, w.daysAgo, w.clock) }

// demoRun is what one run came to. A reminder needs none: it is always done,
// and its line is its own.
type demoRun struct {
	outcome automation.Outcome
	// judged is a watch's decided answer for the look, "yes" or "no", and empty
	// for a look that could not be decided — which changes nothing the watch has
	// seen, exactly as the clock records it.
	judged string
	// line is the one sentence a person reads. A piece of work that reported
	// done leaves it empty: its line is the first line of its report, which is
	// how the runner spells it.
	line string
	// detail is the longer account: the work's report, or — for work cut off
	// before it reported — the last thing it said, which is what the runner
	// keeps; or what a watch that spoke saw.
	detail string
	// said is what a piece of work said last when its account keeps none of it
	// — a run stopped on a permission, or on a fault — so that its transcript
	// still ends on what it was doing.
	said string
	usd  float64
	took time.Duration
}

// The lines the clock puts on a run it ended itself. They are unexported in
// internal/automation (clock.go's lineClosed and lineStopped), so they are
// spelled here as the person reads them — and so, inline where it is used, is
// the runner's `needed your ok to run …` (internal/session's
// automationNeedsLine), which names the tool and then the command.
const (
	demoClosedLine  = "codeaf closed before it finished"
	demoStoppedLine = "stopped by you"
)

// ciRedSight is what the watch on CI saw the time it spoke: gh's own table, as
// it prints one without a terminal.
const ciRedSight = "completed\tfailure\ttui3: fold the quiet rows\tci\tmaster\tpush\t18734\t4m12s\n" +
	"completed\tsuccess\tsession: read Root on the fold\tci\tmaster\tpush\t18731\t3m58s\n" +
	"completed\tsuccess\tmanual: the page about reading a task\tci\tmaster\tpush\t18725\t4m3s\n" +
	"completed\tsuccess\ttui3: one reading of the world\tci\tmaster\tpush\t18719\t4m20s\n" +
	"completed\tsuccess\tpicker: walk the list's rows\tci\tmaster\tpush\t18712\t3m51s"

var demoAutomations = []demoAutomation{
	{
		// A REMINDER ON A RHYTHM, typed on home before anything else here. No
		// model is called when it speaks, so it never costs a cent, and on an
		// evening nobody was at a window it waited for the next morning.
		project: firstProjectName,
		made:    demoWhen{daysAgo: 13, clock: 10*time.Hour + 20*time.Minute},
		item: automation.Automation{
			Title:    "Write tomorrow's plan",
			Schedule: automation.Schedule{Every: "30 17 * * 1-5"},
			Action:   automation.Action{Say: "Write tomorrow's plan before you close the laptop."},
		},
	},
	{
		// A REMINDER THAT HAS NOT HAPPENED YET — the list's row with a moment to
		// come and no last result.
		project: "pricing-site",
		asked:   "Pricing Research",
		item: automation.Automation{
			Title:  "Send the ladder numbers",
			Words:  "and remind me tomorrow at four to send the ladder numbers to sales",
			Action: automation.Action{Say: "Send the ladder numbers to sales."},
		},
		at: func(made time.Time) time.Time {
			year, month, day := made.Date()
			return time.Date(year, month, day+1, 16, 0, 0, 0, made.Location())
		},
	},
	{
		// SCHEDULED WORK ON A CRON LINE, in the live checkout, for the whole
		// fortnight. It carries most of the outcomes a piece of work can come to,
		// and its newest three are clean, which is what the conversation about
		// it says.
		project: firstProjectName,
		made:    demoWhen{daysAgo: 13, clock: 10*time.Hour + 12*time.Minute},
		item: automation.Automation{
			Title:    "The morning sweep",
			Schedule: automation.Schedule{Every: "0 9 * * *"},
			Action:   automation.Action{Do: "Read what landed on this repository since yesterday morning and tell me what changed under me: the branches, the files, and anything that touches what I have uncommitted."},
			Limits:   automation.Limits{Time: 20 * time.Minute, USD: 1},
		},
		took: 6 * time.Minute,
		newest: []demoRun{
			{outcome: automation.OutcomeDone, usd: 0.21,
				detail: "Nothing landed under you since yesterday morning.\nmaster is where you left it, and nothing upstream touches README.md or scratch.md."},
			{outcome: automation.OutcomeDone, usd: 0.34, took: 8 * time.Minute,
				detail: "Two branches landed, both in internal/tui3.\nThe picker port and the lexer port; neither touches what you have uncommitted."},
			{outcome: automation.OutcomeDone, usd: 0.29,
				detail: "One branch landed: the tab bar's counts, three files under internal/tui3.\nIt reads the world once for the bar and the list, and nothing of yours is in its way."},
			{outcome: automation.OutcomeYourCall, usd: 0.04, took: time.Minute,
				line: "needed your ok to run bash git fetch origin",
				said: "Fetching first, so the comparison is against what is really on the remote."},
			{outcome: automation.OutcomeDone, usd: 0.38, took: 9 * time.Minute,
				detail: "Three branches landed; one of them rewrites docs/HOME.md, which you have open.\nThe other two are under internal/session and change nothing you are holding."},
			{outcome: automation.OutcomeStopped, usd: 0.12, took: 3 * time.Minute, line: demoClosedLine,
				detail: "Reading the eleven commits on the release branch, oldest first."},
			{outcome: automation.OutcomeIncomplete, usd: 0.61, took: 20 * time.Minute, line: "ran out of its 20m",
				detail: "The suite is still running against the third branch; there is nothing to report until it finishes."},
			{outcome: automation.OutcomeDone, usd: 0.27,
				detail: "One branch landed: the manual's page about reading a task.\nIt is all under internal/manual/chat and touches nothing you have uncommitted."},
			{outcome: automation.OutcomeIncomplete, usd: 0.03, took: time.Minute,
				line: "a fault: the provider answered 529: overloaded",
				said: "Reading the reflog first, to see where you were yesterday morning."},
		},
		rest: []demoRun{
			{outcome: automation.OutcomeDone, usd: 0.19, detail: "Nothing landed under you since yesterday morning."},
			{outcome: automation.OutcomeDone, usd: 0.26,
				detail: "One branch landed: the picker on the new list widget.\nTwo files under internal/tui3; your scratch.md is untouched."},
		},
	},
	{
		// SCHEDULED WORK IN A SEPARATE WORKTREE, on an interval rather than a cron
		// line, so both dialects of a rhythm are on the list. Every run's account
		// ends by saying where its branch was kept, which the runner does for every
		// run that cut one.
		project: firstProjectName,
		made:    demoWhen{daysAgo: 12, clock: 11*time.Hour + 5*time.Minute},
		item: automation.Automation{
			Title:    "Try the dependency bump",
			Schedule: automation.Schedule{Every: "2d"},
			Action:   automation.Action{Do: "Bump the Go dependencies with go get -u ./..., run go build and go test, and keep the branch for me to review. Merge nothing."},
			Worktree: true,
			Limits:   automation.Limits{Time: 45 * time.Minute, USD: 2},
		},
		took: 14 * time.Minute,
		newest: []demoRun{
			{outcome: automation.OutcomeDone, usd: 0.88,
				detail: "Bumped four modules; the build and the tests pass.\ngolang.org/x/sys, golang.org/x/term, golang.org/x/text and modernc.org/sqlite moved, and nothing else needed a change."},
			{outcome: automation.OutcomeIncomplete, usd: 1.37, took: 45 * time.Minute, line: "ran out of its 45m",
				detail: "go test ./... is on its fourth package; the race detector makes the suite slow."},
			{outcome: automation.OutcomeStopped, usd: 0.22, took: 6 * time.Minute, line: demoStoppedLine,
				detail: "Bumping golang.org/x/sys first; the other three wait on it."},
		},
		rest: []demoRun{
			{outcome: automation.OutcomeDone, usd: 0.31,
				detail: "Nothing to bump: every module is already at its newest release.\nThe branch is kept anyway, empty, so there is one place to look."},
			{outcome: automation.OutcomeDone, usd: 0.64,
				detail: "Bumped two modules; the build and the tests pass.\ngolang.org/x/net and golang.org/x/crypto moved, and nothing else needed a change."},
		},
	},
	{
		// A WATCH ON A COMMAND, said in a conversation five hours ago. It has
		// looked every fifteen minutes since, nearly always quietly — and its
		// newest looks hold the red run it spoke about and the look it could not
		// decide, which are the lines waiting in the conversation that made it.
		project: firstProjectName,
		asked:   "Standing Up the Watches",
		item: automation.Automation{
			Title:    "CI on master",
			Words:    "keep an eye on CI and tell me when master goes red",
			Schedule: automation.Schedule{Every: "15m"},
			Look: &automation.Look{
				Command:   "gh run list --branch master --limit 5",
				Condition: "any of the last five runs on master failed",
			},
			Action: automation.Action{Say: "CI is red on master."},
			Limits: automation.Limits{USD: 0.25},
		},
		took: 7 * time.Second,
		newest: []demoRun{
			{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0012, line: "the last five runs on master are green"},
			{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0013, line: "green again: the re-run of the typecheck job passed"},
			{outcome: automation.OutcomeQuiet, judged: "yes", usd: 0.0012, line: "the typecheck job is still red on master"},
			{outcome: automation.OutcomeDone, judged: "yes", usd: 0.0014,
				detail: "the latest run on master failed in the typecheck job\n\n" + ciRedSight},
			{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0012, line: "the last five runs on master are green"},
			{outcome: automation.OutcomeUnchecked, took: 30 * time.Second, line: "couldn't decide: the provider did not answer in time"},
		},
		first: &demoRun{outcome: automation.OutcomeUnchecked, usd: 0.0011,
			line: "gh printed an authentication error, so there was nothing to judge"},
		rest: []demoRun{
			{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0012, line: "the last five runs on master are green"},
		},
	},
	{
		// A WATCH ON FILES THAT SPOKE ONCE AND FINISHED. It looked every hour from
		// the evening it was said, caught up once the morning after the night,
		// spoke on the first look after the annual toggle rewrote pricing.json,
		// and is in the list now as finished, with its history.
		project: "pricing-site",
		asked:   "What the Discount Means",
		item: automation.Automation{
			Title:    "pricing.json changes",
			Words:    "tell me once pricing.json changes, so I read the new numbers before anyone ships them",
			Schedule: automation.Schedule{Every: "1h"},
			Look: &automation.Look{
				Files:     "pricing.json",
				Condition: "pricing.json changed since the last look",
				Once:      true,
			},
			Action: automation.Action{Say: "pricing.json changed: read the new numbers before they ship."},
		},
		speaksAfter: "Put the annual toggle on the pricing page",
		took:        5 * time.Second,
		newest: []demoRun{
			{outcome: automation.OutcomeDone, judged: "yes", usd: 0.0010,
				detail: "the annual prices were added beside the monthly ones; the enterprise seat is still 12\n\n" +
					"1 file(s) match pricing.json\n1 change(s) since the last look:\n~ pricing.json (changed)"},
			{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0009, line: "pricing.json has not changed since the last look"},
			{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0009, line: "pricing.json has not changed since the last look"},
			{outcome: automation.OutcomeUnchecked, took: 12 * time.Second, line: "couldn't decide: the provider answered 503: service unavailable"},
		},
		first: &demoRun{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0009,
			line: "this is the first look, so there is nothing earlier to compare with"},
		rest: []demoRun{
			{outcome: automation.OutcomeQuiet, judged: "no", usd: 0.0009, line: "pricing.json has not changed since the last look"},
		},
	},
	{
		// THE PAUSED ONE. Create only ever makes an active automation — a card's
		// yes is the only way one is made — so the pause is SetStatus afterwards,
		// exactly as the person's own `p` writes it, and nothing has run since.
		project: "infra",
		made:    demoWhen{daysAgo: 13, clock: 15*time.Hour + 40*time.Minute},
		item: automation.Automation{
			Title:    "The Monday advisory check",
			Schedule: automation.Schedule{Every: "0 10 * * 1"},
			Action:   automation.Action{Do: "Read the provider advisories published this week against terraform's lock file, and tell me which of them apply to what this repository uses."},
			Limits:   automation.Limits{Time: 15 * time.Minute, USD: 0.5},
		},
		pause: &demoWhen{daysAgo: 4, clock: 15*time.Hour + 5*time.Minute},
		took:  4 * time.Minute,
		newest: []demoRun{
			{outcome: automation.OutcomeDone, usd: 0.18,
				detail: "Two advisories against the aws provider; neither touches what this repository uses.\nBoth are in resources this lock file never loads."},
		},
		rest: []demoRun{
			{outcome: automation.OutcomeDone, usd: 0.11, detail: "Nothing new against this lock file this week."},
		},
	},
}

// savedAfter is how long after the person's turn an automation said in it is
// saved: the answer and its card, then one yes.
const savedAfter = 2 * time.Minute

// ── when a window was open ──────────────────────────────────────────────────

// The working day on the demo machine, as wall times. A window opened at the
// first and closed at the second is the only time the clock ran anything.
const (
	deskOpens  = 8*time.Hour + 40*time.Minute
	deskCloses = 18*time.Hour + 20*time.Minute
	// sittingFor is how long the window the person is in NOW has been open. It
	// reaches back past every automation said in a conversation today, so the
	// watch saved five hours ago has looked on every slot since, whatever hour
	// the fixture is built at.
	sittingFor = 6 * time.Hour
)

// deskDays are the days, by days back, that were not ordinary working days,
// and what each was instead. A zero pair is a day no window was opened at all.
// They are what make the late runs: every slot that passed on them is caught
// up once, late, by the first window after.
var deskDays = map[int]struct{ opens, closes time.Duration }{
	// A WEEKEND AWAY, so the morning sweep catches up ONCE when the person is
	// back rather than three times.
	9: {}, 8: {},
	// Back late after it.
	7: {opens: 10*time.Hour + 15*time.Minute, closes: deskCloses},
	// Left early, so the evening reminder waited for the next morning.
	4: {opens: deskOpens, closes: 16*time.Hour + 10*time.Minute},
	// Started late, so the nine o'clock sweep ran at half past eleven.
	3: {opens: 11*time.Hour + 30*time.Minute, closes: deskCloses},
}

// demoSpan is one stretch with a codeaf window open.
type demoSpan struct{ from, to time.Time }

// deskWindows is every stretch a window was open over the fortnight, merged and
// in order, none of it after now: the working days, the window the person is
// sitting in, and the moments automations were made — somebody making one was,
// by definition, at a window.
func deskWindows(now time.Time, made []time.Time) []demoSpan {
	var spans []demoSpan
	for day := usageDays - 1; day >= 0; day-- {
		opens, closes := deskOpens, deskCloses
		if odd, ok := deskDays[day]; ok {
			opens, closes = odd.opens, odd.closes
		}
		if closes > opens {
			spans = append(spans, demoSpan{wallOn(now, day, opens), wallOn(now, day, closes)})
		}
	}
	spans = append(spans, demoSpan{now.Add(-sittingFor), now})
	for _, at := range made {
		spans = append(spans, demoSpan{at.Add(-time.Minute), at.Add(10 * time.Minute)})
	}
	var clipped []demoSpan
	for _, span := range spans {
		if span.to.After(now) {
			span.to = now
		}
		if span.from.Before(span.to) {
			clipped = append(clipped, span)
		}
	}
	sort.Slice(clipped, func(i, j int) bool { return clipped[i].from.Before(clipped[j].from) })
	var merged []demoSpan
	for _, span := range clipped {
		if last := len(merged) - 1; last >= 0 && !span.from.After(merged[last].to) {
			if span.to.After(merged[last].to) {
				merged[last].to = span.to
			}
			continue
		}
		merged = append(merged, span)
	}
	return merged
}

// wallOn is a wall time on the day daysAgo days before now, read in now's zone.
// It is built from the clock's hours and minutes rather than added to midnight,
// so a day the clocks change on still opens at twenty to nine.
func wallOn(now time.Time, daysAgo int, clock time.Duration) time.Time {
	day := now.AddDate(0, 0, -daysAgo)
	return time.Date(day.Year(), day.Month(), day.Day(),
		int(clock/time.Hour), int(clock%time.Hour/time.Minute), 0, 0, now.Location())
}

// ── the clock, replayed ─────────────────────────────────────────────────────

// demoClock is the store with a fixture's hand on its clock.
type demoClock struct {
	store   *automation.Store
	at      time.Time // what the store's writes read as now
	now     time.Time // the fixture's now: nothing here happens after it
	windows []demoSpan
}

// demoTake is one slot the clock took: the slot, the moment it took it, and
// when the window it was taken in closed.
type demoTake struct {
	slot, at, closes time.Time
}

// when is the moment the clock took a slot: just after it, when a window was
// open then — the clock looks every two seconds — or the moment the next window
// opened, when none was. ok is false for a slot nothing has taken yet.
func (c *demoClock) when(slot time.Time) (at, closes time.Time, ok bool) {
	for _, span := range c.windows {
		if slot.After(span.to) {
			continue
		}
		if slot.Before(span.from) {
			return span.from, span.to, true
		}
		at = slot.Add(time.Second)
		if at.After(span.to) {
			at = span.to
		}
		return at, span.to, true
	}
	return time.Time{}, time.Time{}, false
}

// madeAutomations is what the seeding came to: the counts, and what the runs
// cost, which the ledger books (seed_spend.go) so the spend page and each run's
// own figure are one account.
type madeAutomations struct {
	automations, runs int
	bills             []session.UsageLine
}

// writeAutomations makes every automation and drives it through the fortnight,
// in the store under root, and answers what it wrote.
func writeAutomations(root string, projects map[string]*demoProject, ids map[string]string, now time.Time) (madeAutomations, error) {
	var made madeAutomations
	// WHOLE SECONDS. The store keeps milliseconds, so every moment here is one
	// it can hold exactly, and what is read back is what was written: the
	// replay below checks the slot the store took against the one it expected,
	// and that check should only ever be about the schedule, never rounding.
	now = now.Truncate(time.Second)
	store, err := automation.Open(root)
	if err != nil {
		return made, fmt.Errorf("open the automations store: %w", err)
	}
	defer store.Close()

	// When each was made comes first, because the windows depend on all of
	// them: nobody makes an automation without a window open.
	moments := make([]time.Time, len(demoAutomations))
	origins := make([]automation.Origin, len(demoAutomations))
	for index, spec := range demoAutomations {
		moment, origin, err := spec.madeAt(projects, ids, now)
		if err != nil {
			return made, err
		}
		moments[index], origins[index] = moment, origin
	}
	clock := &demoClock{store: store, now: now, windows: deskWindows(now, moments)}
	store.SetClock(func() time.Time { return clock.at })

	// THE ZONE IS CAPTURED THE WAY THE automation TOOL CAPTURES IT, so a rhythm
	// here reads as one said on this machine.
	zone := session.LocalZone()
	for index, spec := range demoAutomations {
		project, ok := projects[spec.project]
		if !ok {
			return made, fmt.Errorf("the automation %q names no project %q", spec.item.Title, spec.project)
		}
		item := spec.item
		item.Workspace = project.dir
		item.Origin = origins[index]
		if item.Schedule.Repeats() {
			item.Schedule.Zone = zone
		}
		if spec.at != nil {
			item.Schedule.At = spec.at(moments[index])
		}
		clock.at = moments[index]
		saved, err := store.Create(item)
		if err != nil {
			return made, fmt.Errorf("make the automation %q: %w", item.Title, err)
		}
		runs, bills, err := clock.drive(spec, saved)
		if err != nil {
			return made, fmt.Errorf("run the automation %q: %w", item.Title, err)
		}
		if spec.pause != nil {
			clock.at = spec.pause.on(now)
			if _, err := store.SetStatus(saved.ID, automation.StatusPaused); err != nil {
				return made, fmt.Errorf("pause the automation %q: %w", item.Title, err)
			}
		}
		made.automations++
		made.runs += runs
		made.bills = append(made.bills, bills...)
	}
	return made, nil
}

// madeAt is when an automation was made and the conversation it was made in.
//
// ONE SAID IN A CONVERSATION IS MADE JUST AFTER THE TURN THAT ASKED FOR IT, and
// that turn must be the person's own words verbatim — a Words that is not in
// the journal it points at would be provenance pointing at nothing.
func (spec demoAutomation) madeAt(projects map[string]*demoProject, ids map[string]string, now time.Time) (time.Time, automation.Origin, error) {
	if spec.asked == "" {
		return spec.made.on(now), automation.Origin{}, nil
	}
	talk, ok := talkNamed(spec.asked)
	if !ok {
		return time.Time{}, automation.Origin{}, fmt.Errorf("the automation %q was said in %q, which is not a conversation this fixture writes", spec.item.Title, spec.asked)
	}
	if talk.project != spec.project {
		return time.Time{}, automation.Origin{}, fmt.Errorf("the automation %q is in %q but was said in a conversation in %q", spec.item.Title, spec.project, talk.project)
	}
	turn := -1
	for index, spoken := range talk.turns {
		if spoken.said == spec.item.Words {
			turn = index
		}
	}
	if turn < 0 {
		return time.Time{}, automation.Origin{}, fmt.Errorf("the automation %q keeps words %q that nobody said in %q", spec.item.Title, spec.item.Words, spec.asked)
	}
	id, project := ids[talk.title], projects[talk.project]
	if id == "" || project == nil {
		return time.Time{}, automation.Origin{}, fmt.Errorf("the conversation %q was not written", talk.title)
	}
	moment := turnAt(now.Add(-talk.ago), len(talk.turns), turn).Add(savedAfter)
	if moment.After(now) {
		moment = now
	}
	return moment, automation.Origin{
		SessionID:  id,
		Transcript: filepath.Join(project.bucket, id, "transcript.jsonl"),
	}, nil
}

// drive replays the clock over one automation: the slots it took, in order,
// each one taken, started and finished through the store at the moment it
// happened. It answers how many runs it wrote and what they cost.
func (c *demoClock) drive(spec demoAutomation, a automation.Automation) (int, []session.UsageLine, error) {
	var paused, speaks time.Time
	if spec.pause != nil {
		paused = spec.pause.on(c.now)
	}
	if spec.speaksAfter != "" {
		ago, ok := taskLanded(spec.speaksAfter)
		if !ok {
			return 0, nil, fmt.Errorf("it speaks after %q, which is not work this fixture lands", spec.speaksAfter)
		}
		speaks = c.now.Add(-ago)
	}

	// FIRST THE SLOTS, worked out with the schedule's own arithmetic, because
	// what each run came to is written newest first and so the count comes
	// before the first write. The replay below then asks the store for each one,
	// and a store that disagrees about the slot is a failure, not a fixup.
	var takes []demoTake
	for next := a.Next; !next.IsZero(); {
		at, closes, ok := c.when(next)
		if !ok || at.After(c.now) || (!paused.IsZero() && !at.Before(paused)) {
			break
		}
		takes = append(takes, demoTake{slot: next, at: at, closes: closes})
		if !a.Schedule.Repeats() || (!speaks.IsZero() && !at.Before(speaks)) {
			break
		}
		var err error
		if next, err = a.Schedule.Next(at); err != nil {
			return 0, nil, err
		}
	}
	scripts := make([]demoRun, len(takes))
	for index := range takes {
		scripts[index] = spec.script(len(takes)-1-index, index)
	}
	if err := checkJudgments(a, scripts); err != nil {
		return 0, nil, err
	}

	var bills []session.UsageLine
	for index, take := range takes {
		script := scripts[index]
		current, err := c.store.Get(a.ID)
		if err != nil {
			return index, bills, err
		}
		c.at = take.at
		run, err := c.store.Take(current, take.at)
		if err != nil {
			return index, bills, fmt.Errorf("take the slot at %s: %w", take.slot, err)
		}
		if !run.Due.Equal(take.slot.Truncate(time.Millisecond)) {
			return index, bills, fmt.Errorf("the store took the slot at %s where the replay expected %s", run.Due, take.slot)
		}
		if err := c.store.Start(run.ID); err != nil {
			return index, bills, err
		}
		took := script.took
		if took == 0 {
			took = spec.took
		}
		finished := take.at.Add(took)
		if finished.After(take.closes) {
			finished = take.closes
		}
		if err := c.record(current, &run, script, take.at, finished); err != nil {
			return index, bills, err
		}
		if script.judged != "" {
			if err := c.store.SetSeen(a.ID, current.Revision, script.judged, ""); err != nil {
				return index, bills, err
			}
		}
		if current.Look != nil && current.Look.Once && script.outcome == automation.OutcomeDone {
			if err := c.store.FinishWatch(a.ID, current.Revision); err != nil {
				return index, bills, err
			}
		}
		c.at = finished
		if err := c.store.Finish(run); err != nil {
			return index, bills, err
		}
		if bill, ok := demoBill(current, run, finished); ok {
			bills = append(bills, bill)
		}
	}
	return len(takes), bills, nil
}

// script is what the run at newest-first position k, chronological position i,
// came to.
func (spec demoAutomation) script(k, i int) demoRun {
	switch {
	case k < len(spec.newest):
		return spec.newest[k]
	case i == 0 && spec.first != nil:
		return *spec.first
	case len(spec.rest) > 0:
		return spec.rest[i%len(spec.rest)]
	}
	return demoRun{outcome: automation.OutcomeDone}
}

// checkJudgments holds a watch's history to the clock's own rule (clock.go's
// watch): it speaks on the CHANGE to yes, is quiet while the condition stays
// true and whenever it is false, and a look it could not decide changes nothing
// it has seen. A history that broke the rule would be a fixture drawing a watch
// the product cannot have, so it fails here rather than on somebody's screen.
func checkJudgments(a automation.Automation, scripts []demoRun) error {
	seen := ""
	for index, run := range scripts {
		if a.Look == nil {
			if run.judged != "" {
				return fmt.Errorf("run %d judges a condition, and only a watch has one", index)
			}
			continue
		}
		var want automation.Outcome
		switch {
		case run.judged == "":
			want = automation.OutcomeUnchecked
		case run.judged == "no", seen == "yes":
			want = automation.OutcomeQuiet
		default:
			want = automation.OutcomeDone
		}
		if run.outcome != want {
			return fmt.Errorf("look %d judged %q after %q and came to %q; the clock would record %q", index, run.judged, seen, run.outcome, want)
		}
		if run.judged != "" {
			seen = run.judged
		}
	}
	return nil
}

// record writes what one run came to onto it, in the words the clock and the
// runner use, and lays down the session folder a piece of work ran in.
func (c *demoClock) record(a automation.Automation, run *automation.Run, script demoRun, started, finished time.Time) error {
	run.Outcome, run.USD = script.outcome, round(script.usd)
	run.Line, run.Detail = script.line, script.detail
	switch a.Kind() {
	case automation.KindReminder:
		run.Line = strings.TrimSpace(a.Action.Say)
	case automation.KindWatch:
		if script.outcome == automation.OutcomeDone {
			run.Line = strings.TrimSpace(a.Action.Say)
		}
	default:
		if run.Line == "" {
			run.Line, _, _ = strings.Cut(script.detail, "\n")
		}
		folder, err := c.writeRunFolder(a, *run, script, started, finished)
		if err != nil {
			return err
		}
		run.Transcript = folder
		if a.Worktree {
			run.Detail = strings.TrimSpace(run.Detail + "\n\n" + worktreeNote(folder, a, *run))
		}
	}
	return nil
}

// writeRunFolder lays down the session folder one run of work ran in, where
// the runner puts it — runs/<automation>/<run> under the store — with the
// identity file the runner writes and a journal of the brief and how it ended,
// so the transcript a run's history points at is one that is there.
func (c *demoClock) writeRunFolder(a automation.Automation, run automation.Run, script demoRun, started, finished time.Time) (string, error) {
	id := strconv.FormatInt(run.ID, 10)
	folder := filepath.Join(c.store.Root(), "runs", a.ID, id)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", folder, err)
	}
	worker := demoModels[1]
	if err := session.SaveMeta(folder, session.Meta{
		ID: id, Title: a.Title, Workspace: a.Workspace, Model: worker.slug, Created: started,
	}); err != nil {
		return "", fmt.Errorf("write the identity of %s: %w", folder, err)
	}
	words := script.said
	if words == "" {
		words = script.detail
	}
	// THE JOURNAL ENDS WHEN THE RUN DID. Its one turn is stamped back from the
	// finish, so nothing in it is later than the run it records.
	talk := demoTalk{
		title: a.Title, model: worker.slug, spent: round(script.usd),
		tokens: billTokens(script.usd, worker.rate),
		turns:  []demoTurn{{said: a.Action.Do, answered: words}},
	}
	if err := writeTranscript(filepath.Join(folder, "transcript.jsonl"), id, a.Workspace, talk, finished.Add(-savedAfter)); err != nil {
		return "", err
	}
	return folder, nil
}

// worktreeNote is the line the runner adds to the account of every run it cut a
// worktree for: the branch it kept, and where. The name is the runner's own
// shape — automation-<title>-<six hex> — with the hex taken from the run so a
// demo built twice names the same branch.
func worktreeNote(folder string, a automation.Automation, run automation.Run) string {
	name := fmt.Sprintf("automation-%s-%06x", session.TaskSlug(a.Title), (run.ID*0x9e3779)&0xffffff)
	return "work kept on automation/" + name + " in " + filepath.Join(folder, "trees", name)
}

// demoBill is the ledger line one run's money is booked under, and false for a
// run that cost nothing — a reminder never does, and a look the provider never
// answered was never priced.
//
// IT IS THE SHAPE THE PRODUCT WRITES. A watch's judgment is one call on the low
// tier's model — here the cheapest of [demoModels] — made as the sentinel role
// and booked against the automation with no conversation (internal/session's
// automation_run.go). A run of work is a session of its own whose journal is
// the run's folder, so its lines name that session — the run's id — beside the
// automation; its calls are booked on one line, the way this fixture books
// every turn.
func demoBill(a automation.Automation, run automation.Run, at time.Time) (session.UsageLine, bool) {
	if run.USD <= 0 {
		return session.UsageLine{}, false
	}
	line := session.UsageLine{At: at, USD: run.USD, Automation: a.ID, Workspace: a.Workspace}
	if a.Kind() == automation.KindWatch {
		judge := demoModels[len(demoModels)-1]
		tokens := billTokens(run.USD, judge.rate)
		line.Model, line.Role, line.Calls = judge.slug, string(roles.RoleSentinel), 1
		line.Input, line.Output = tokens-tokens/40, tokens/40
		return line, true
	}
	worker := demoModels[1]
	tokens := billTokens(run.USD, worker.rate)
	line.Model, line.Calls = worker.slug, 3+int(run.ID%6)
	line.Input, line.Output = tokens*4/5, tokens/5
	line.Session = strconv.FormatInt(run.ID, 10)
	return line, true
}

// billTokens is how many tokens a figure buys at a model's rough rate, so the
// ledger's tokens and its money agree with each other.
func billTokens(usd, rate float64) int {
	return int(math.Round(usd / rate * 1000))
}

// talkNamed is the conversation with that title.
func talkNamed(title string) (demoTalk, bool) {
	for _, talk := range demoConversations {
		if talk.title == title {
			return talk, true
		}
	}
	return demoTalk{}, false
}

// taskLanded is how long ago the piece of work with that title landed.
func taskLanded(title string) (time.Duration, bool) {
	for _, task := range demoTasks {
		if task.entry.Title == title && task.ago > 0 {
			return task.ago, true
		}
	}
	return 0, false
}
