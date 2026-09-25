package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// WHAT A SEAT CALL IS HELD TO BEFORE IT IS MADE (internal/session's
// spendguard.go does the holding): the model's prices, the day's cap, and each
// seat's own ceiling on one task.

// CrewCallPrice is a model's prices per token — prompt, completion, cache
// read — as the catalog or the evidence table knows them. ok is false only for
// a price NOBODY KNOWS: a model the catalog does not list, lists with no price,
// or lists at nothing without being a free pool. A free pool's price is known,
// and it is nothing.
func CrewCallPrice(model string) (prompt, completion, cacheRead float64, ok bool) {
	if crewroute.IsFree(model) {
		return 0, 0, 0, true
	}
	m, known := crewCatalogModel(model)
	if !known || (m.PromptPrice <= 0 && m.CompletionPrice <= 0) {
		return 0, 0, 0, false
	}
	return m.PromptPrice, m.CompletionPrice, m.CacheReadPrice, true
}

// CrewCallPriceAt is [CrewCallPrice] on one profile's connections: a call SENT
// THROUGH A SUBSCRIPTION PLAN OR TO A MODEL ON THIS MACHINE bills nothing per
// token, so it is priced at nothing whatever the catalog lists for the model —
// a z-ai coding plan answers `z-ai/…` ids the catalog prices as metered. The
// connections are read once, on the first call priced.
func CrewCallPriceAt(profileDir string) func(model string) (prompt, completion, cacheRead float64, ok bool) {
	var once sync.Once
	nothing := map[string]bool{}
	return func(model string) (float64, float64, float64, bool) {
		once.Do(func() {
			for _, p := range CrewProvidersAt(profileDir) {
				if p.Kind == crewroute.Plan || p.Kind == crewroute.Local {
					nothing[strings.ToLower(strings.TrimSpace(p.Written))] = true
				}
			}
		})
		if slash := strings.Index(model, "/"); slash > 0 && nothing[strings.ToLower(strings.TrimSpace(model[:slash]))] {
			return 0, 0, 0, true
		}
		return CrewCallPrice(model)
	}
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

// CrewTaskSpendCap is the per-task limit every priced call of one task is
// held to ([CrewTaskCapAt]) and the sentence a call it stops ends on. It is
// not lifted by --yes-spend: that flag answers the day's questions, not this.
func CrewTaskSpendCap(profileDir string) (float64, string) {
	capUSD := CrewTaskCapAt(profileDir)
	return capUSD, CrewTaskCapAction(capUSD)
}

// CrewTaskCapAction is the sentence a call the per-task limit stops ends on.
func CrewTaskCapAction(capUSD float64) string {
	return "this task reached its " + CrewTaskMoney(capUSD) + " limit · raise it in /crew"
}

// CrewTaskMoney is a per-task limit in words: whole dollars without cents
// ($5), anything else to the cent ($2.50).
func CrewTaskMoney(usd float64) string {
	if usd == math.Trunc(usd) {
		return fmt.Sprintf("$%.0f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// CrewSeatCeilings is each seat's own spend ceiling on one task, keyed by the
// seat rather than its model: the CHECKER'S, a few times what it is expected to
// cost and never under a floor. A check is the seat whose length nothing else
// bounds — it reads until it is satisfied — and one that ran to eleven times
// its estimate on a dear model was the whole of a day's overshoot. A seat on a
// route that bills nothing has no ceiling.
func CrewSeatCeilings(d crewroute.Decision) map[crewroute.Seat]float64 {
	checker := d.Seat(crewroute.Checker)
	if checker.EstUSD <= 0 {
		return nil
	}
	ceiling := checker.EstUSD * CrewCheckCeilingTimes
	if ceiling < CrewCheckCeilingFloor {
		ceiling = CrewCheckCeilingFloor
	}
	// THE CEILING BELONGS TO THE SEAT, NOT TO A MODEL. A fresh profile's
	// narrow fix can put one model in all three seats, and a ceiling keyed by
	// model had to be dropped there or it would have stopped the worker at the
	// checker's line. The guard attributes each call to the seat that made it
	// (internal/session's SeatCompleter), so the checker keeps its ceiling
	// whatever the other seats run, and across a fallback to another model.
	return map[crewroute.Seat]float64{crewroute.Checker: ceiling}
}

// CrewCheckCeilingAction is the sentence a check its ceiling ends says, with
// the ceiling's dollars. The multiplier is spelled from
// [CrewCheckCeilingTimes], so the sentence cannot promise a figure the guard
// does not hold.
var CrewCheckCeilingAction = "the check stopped at its spend ceiling of $%.2f, " +
	crewCeilingTimesWord() + " times its estimate, before it finished"

// The checker's ceiling: how many times its estimate, and the least it is.
// The manual quotes both (internal/manual's truth_test.go holds it to them).
const (
	CrewCheckCeilingTimes = 3.0
	CrewCheckCeilingFloor = 0.05
)

// crewCeilingTimesWord is the multiplier as a sentence says it: a small whole
// number in words, the way prose spells a count, and anything else in digits.
func crewCeilingTimesWord() string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	if n := int(CrewCheckCeilingTimes); float64(n) == CrewCheckCeilingTimes && n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.FormatFloat(CrewCheckCeilingTimes, 'f', -1, 64)
}
