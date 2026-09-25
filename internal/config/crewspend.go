package config

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// WHAT A SEAT CALL IS HELD TO BEFORE IT IS MADE (internal/session's
// spendguard.go does the holding): the model's prices, the day's cap, and each
// seat's own ceiling on one task.

// CrewCallPrice is a model's prices per token — prompt, completion, cache
// read — as the catalog or the evidence table knows them. ok is false for a
// model neither prices, and for a free pool.
func CrewCallPrice(model string) (prompt, completion, cacheRead float64, ok bool) {
	m, known := crewCatalogModel(model)
	if !known || (m.PromptPrice <= 0 && m.CompletionPrice <= 0) {
		return 0, 0, 0, false
	}
	return m.PromptPrice, m.CompletionPrice, m.CacheReadPrice, true
}

// CrewSpendCap is the day's cap a crew's seat calls are held to, and the one
// sentence a call it stops ends on: the crew's own daily cap
// ([CrewCapAt]), or — where withDaily says the run is bound by it — the
// day's spending limit ([DailyBudgetUSDAt]), whichever is lower. Zero is no
// cap.
func CrewSpendCap(profileDir string, withDaily bool) (float64, string) {
	capUSD, action := CrewCapAt(profileDir), ""
	if capUSD > 0 {
		action = "today's crew spend has reached the daily cap of " + crewroute.Money(capUSD) + " · raise it with /crew cap"
	}
	if withDaily {
		if daily, err := DailyBudgetUSDAt(profileDir); err == nil && daily > 0 && (capUSD <= 0 || daily < capUSD) {
			capUSD = daily
			action = fmt.Sprintf("today's spending limit of $%.2f is reached · raise it with /budget", daily)
		}
	}
	return capUSD, action
}

// CrewSeatCeilings is each seat's own spend ceiling on one task, keyed by the
// ids the seat is asked for: the CHECKER'S, a few times what it is expected to
// cost and never under a floor. A check is the seat whose length nothing else
// bounds — it reads until it is satisfied — and one that ran to eleven times
// its estimate on a dear model was the whole of a day's overshoot. A seat on a
// route that bills nothing has no ceiling.
func CrewSeatCeilings(d crewroute.Decision) map[string]float64 {
	checker := d.Seat(crewroute.Checker)
	if checker.EstUSD <= 0 {
		return nil
	}
	ceiling := checker.EstUSD * crewCheckCeilingTimes
	if ceiling < crewCheckCeilingFloor {
		ceiling = crewCheckCeilingFloor
	}
	// A CHECKER ON A MODEL ANOTHER SEAT ALSO SITS has no ceiling: the guard
	// keeps a model's spend, not a seat's, and a crew whose three seats are one
	// model would stop its WORKER at the checker's line.
	for _, pick := range d.Crew {
		if pick.Seat != crewroute.Checker && (pick.Send == checker.Send || pick.Model == checker.Model) {
			return nil
		}
	}
	out := map[string]float64{}
	for _, id := range []string{checker.Send, checker.Model} {
		if id != "" {
			out[id] = ceiling
		}
	}
	return out
}

// CrewCheckCeilingAction is the sentence a check its ceiling ends says, with
// the ceiling's dollars.
const CrewCheckCeilingAction = "the check stopped at its spend ceiling of $%.2f, three times its estimate, before it finished"

// The checker's ceiling: how many times its estimate, and the least it is.
const (
	crewCheckCeilingTimes = 3.0
	crewCheckCeilingFloor = 0.05
)
