package main

// seed_standing.go is the four standing orders and what they have cost.
//
// Every one of them goes through internal/standing's own store, so the document
// on disk is the one the ticker and the surface both read: [standing.Store.Create]
// validates it, stamps it and works out when it is next due, and
// [standing.Store.Save] is how the quiet half — what the last check said, how
// many firings in a row came back clean — is written afterwards. The daily
// ledger beside them is [standing.Store.Append], one line per firing and per
// check that cost money, which is where the cost-per-firing on the page is
// summed from.

import (
	"fmt"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// demoOrder is one standing order, in two halves: the item as it is created,
// and the quiet half a check or a firing writes onto it afterwards.
type demoOrder struct {
	project string
	item    standing.Item
	// after is what has happened to it since. It is applied with a Save, exactly
	// as the ticker applies it, because Create refuses to make anything that is
	// not active and has never run.
	after func(item *standing.Item, now time.Time)
	// spent is the ledger this order has left behind: one entry per element,
	// oldest first, counted back in days from today.
	spent []demoFiring
}

// entryCheck names a ledger line that is a probe and its sentinel rather than a
// firing: it costs money, so it counts against the day's rail, and it did not
// fire, so it counts against nothing else. The word is internal/standing's own
// (ledger.go's entryCheck) and is unexported there; "say" and "task" beside it
// are [standing.ActionKind]'s two values and are spelled through the package.
const entryCheck = "check"

// demoFiring is one line of a day's ledger: what it was, what it cost, and how
// many days back it happened.
type demoFiring struct {
	kind    string
	usd     float64
	daysAgo int
}

var demoOrders = []demoOrder{
	{
		// The one that is ASKING. Its latest run stopped on a question, which is
		// the field home sorts on and the reason this row is drawn first.
		project: firstProjectName,
		item: standing.Item{
			ID:       "watch-ci",
			Words:    "tell me when CI goes red on master",
			Altitude: standing.AltitudeProject,
			Brief:    standing.Brief{Title: "CI on master"},
			When:     standing.When{Kind: standing.WhenProbe, Words: "every five minutes", ProbeEvery: 5 * time.Minute, Hint: "yes when any run on master shows conclusion=failure", Probe: standing.Probe{Command: "gh run list --branch master --limit 5"}},
			Does:     standing.Action{Kind: standing.ActionSay, Say: "CI is red on master: {{evidence}}"},
			Rails:    standing.Rails{PerRunUSD: 0.25, MaxPerDay: 12},
			Grant:    "It may read the repository and the run log, and it asks before anything else.",
		},
		after: func(item *standing.Item, now time.Time) {
			item.LastChecked = now.Add(-3 * time.Minute)
			item.LastCheckLine = "the last five runs on master are green"
			item.Previous = []string{"three runs green, one cancelled → nothing", "all green → nothing"}
			item.Runs = 4
			item.LastFired = now.Add(-31 * time.Hour)
			item.LastOutcome = "said: CI is red on master (the typecheck job)"
			item.SpentUSD = 1.18
			item.NeedsPerson = "May I re-run the typecheck job to see whether it is flaky?"
		},
		spent: []demoFiring{
			{entryCheck, 0.01, 0}, {entryCheck, 0.01, 0}, {entryCheck, 0.02, 0},
			{string(standing.ActionSay), 0.21, 1}, {entryCheck, 0.02, 1},
			{entryCheck, 0.01, 2}, {string(standing.ActionSay), 0.19, 3},
		},
	},
	{
		// The one that FIRED TODAY and has come back clean three times running,
		// which is the count the rope column's middle rung is drawn from.
		project: firstProjectName,
		item: standing.Item{
			ID:       "morning-sweep",
			Words:    "sweep the repo every morning at nine and tell me what changed under me",
			Altitude: standing.AltitudeProject,
			Brief:    standing.Brief{Title: "the morning sweep"},
			When:     standing.When{Kind: standing.WhenEvery, Words: "every morning at nine", Every: "0 9 * * *"},
			Does: standing.Action{Kind: standing.ActionTask, MaxSteps: 40,
				Brief:      "Read what landed on this repository since yesterday morning and say what changed under me.",
				Acceptance: "One paragraph naming the branches and the files, or the sentence that nothing landed."},
			Rails: standing.Rails{PerRunUSD: 0.40, MaxPerDay: 2},
			Grant: "It may read the repository and run the test suite; it writes nothing.",
		},
		after: func(item *standing.Item, now time.Time) {
			morning := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
			if morning.After(now) {
				morning = morning.AddDate(0, 0, -1)
			}
			item.LastChecked = morning
			item.LastCheckLine = "nothing landed under you since yesterday morning"
			item.Runs = 12
			item.CleanRuns = 3
			item.LastFired = morning
			item.LastOutcome = "done: two branches landed, both in internal/tui3"
			item.SpentUSD = 3.94
		},
		spent: []demoFiring{
			{string(standing.ActionTask), 0.33, 0}, {string(standing.ActionTask), 0.29, 1},
			{string(standing.ActionTask), 0.31, 2}, {string(standing.ActionTask), 0.36, 3},
			{string(standing.ActionTask), 0.28, 4},
		},
	},
	{
		// The PAUSED one. Create only ever makes an active item — a proposal is a
		// card in a conversation and only a yes makes an item — so the pause is a
		// Save afterwards, exactly as the surface's own `p` writes it.
		project: "infra",
		item: standing.Item{
			ID:       "deps-weekly",
			Words:    "check the dependency advisories every Monday morning",
			Altitude: standing.AltitudeProject,
			Brief:    standing.Brief{Title: "the Monday advisory check"},
			When:     standing.When{Kind: standing.WhenEvery, Words: "every Monday at ten", Every: "0 10 * * 1"},
			Does:     standing.Action{Kind: standing.ActionSay, Say: "New advisories against what this repo depends on: {{evidence}}"},
			Rails:    standing.Rails{PerRunUSD: 0.20, MaxPerDay: 1},
		},
		after: func(item *standing.Item, now time.Time) {
			item.Status = standing.StatusPaused
			item.Runs = 6
			item.LastFired = now.Add(-9 * 24 * time.Hour)
			item.LastOutcome = "said: nothing new against this lockfile"
			item.SpentUSD = 0.72
		},
		spent: []demoFiring{{string(standing.ActionSay), 0.11, 9}},
	},
	{
		// The RULE. A hold never wakes, so it can never spend, which is why it
		// alone needs no rails and no action — and why it leaves no ledger line.
		project: firstProjectName,
		item: standing.Item{
			ID:       "public-api-hold",
			Words:    "never change the public API without telling me first",
			Altitude: standing.AltitudeProject,
			Brief:    standing.Brief{Title: "the public API"},
			When:     standing.When{Kind: standing.WhenHold, Words: "always"},
		},
	},
}

// writeStanding creates the four orders, writes the quiet half onto them, and
// leaves the day ledgers their cost-per-firing is summed from.
func writeStanding(root string, projects map[string]*demoProject, now time.Time) (int, error) {
	orders, err := standing.Open(root)
	if err != nil {
		return 0, fmt.Errorf("open the standing store: %w", err)
	}
	written := 0
	for _, order := range demoOrders {
		project, ok := projects[order.project]
		if !ok {
			return written, fmt.Errorf("the standing order %q names no project %q", order.item.ID, order.project)
		}
		item := order.item
		item.Workspace = project.dir
		made, err := orders.Create(item)
		if err != nil {
			return written, fmt.Errorf("create the standing order %q: %w", item.ID, err)
		}
		if order.after != nil {
			order.after(&made, now)
			if err := orders.Save(made); err != nil {
				return written, fmt.Errorf("write what has happened to %q: %w", item.ID, err)
			}
		}
		for _, firing := range order.spent {
			if err := orders.Append(standing.Entry{
				At:     now.AddDate(0, 0, -firing.daysAgo).Add(-time.Duration(written+1) * 17 * time.Minute),
				ItemID: made.ID,
				Kind:   firing.kind,
				USD:    firing.usd,
			}); err != nil {
				return written, fmt.Errorf("write the ledger for %q: %w", item.ID, err)
			}
		}
		written++
	}
	return written, nil
}
