package provider

import (
	"encoding/json"
	"strings"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── A CEILING ONLY AN EXCLUDED MACHINE FITS UNDER ───────────────────────────
//
// The price ceiling is list × [latencyPriceCeiling], and a model's list price is
// its CHEAPEST machine's tariff — which is very often the first-party machine,
// and the first-party machine is exactly the kind an account's paid-training
// switch takes away. So for this account the ceiling can admit one machine and
// that machine can be one the account will never reach: the ceiling is then a
// demand in all but name, and nothing this process held knew it.
//
// ── THE MEASURED FAILURE (2026-09-10, the real binary, deepseek-v4.1-flash) ──
//
// A fresh process on a home that had already learned DeepSeek was excluded —
// the pin on it was stood down before it was sent, as designed — still paid a
// 404 on every turn. The primary carried no demand and no veto, only
// `order: [Fireworks, DeepInfra]` and `max_price {prompt 0.1875, completion
// 0.75}`, which is 1.25 × DeepSeek's own 0.15 / 0.60; the router's funnel
// answered `Initial Endpoints 8 → Filter by Max Price 1 → Filter by Guardrails
// 0`. The walk rescued each turn onto DeepInfra, so nothing failed where a
// person could see it — and nothing learned anything either: the refusal named
// no machine, so the account's set never grew, and the ladder whose price rung
// would have dropped the ceiling was deferred to a walk that won.
//
// ── THE LAW ─────────────────────────────────────────────────────────────────
//
// A CEILING THAT ADMITS ONLY MACHINES THE SERVING SET RULES OUT IS NOT SENT.
// That is the price rung's own answer reached without paying for it, and it
// keeps the promise the price rung was split out to keep (#678): the ceiling
// comes off only when there is NO wider set under it to try, which the sheet
// can say before the request goes and the ladder could only find out after.
//
// AND A REFUSAL THAT NAMES NO MACHINE STILL TEACHES WHICH MACHINES IT WAS
// ABOUT, when the router's own count and this process's sheet agree exactly on
// the set this request's filters left. That is what makes the law above bite on
// the turn after the first refusal rather than only after somebody happens to
// demand the machine.

// ceilingOnlyAdmitsTheUnserved reports whether every machine the sheet prices
// under this ceiling is one the serving set rules out for this model — the
// account's exclusions and this model's own refusals both, through the one
// reader of that set ([lanes.Serves]).
//
// ABSENCE, NEVER A GUESS, which is [Client.priceCeiling]'s own rule stated
// again. A model with no sheet, or a ceiling the sheet puts nobody under, says
// nothing about reachability, and the ceiling goes out exactly as before; the
// ladder is still there for whatever the router says about it.
func ceilingOnlyAdmitsTheUnserved(model string, ceiling *maxPrice) bool {
	if ceiling == nil {
		return false
	}
	ledgerModel := laneModel(model)
	admitted := 0
	for _, row := range lanes.Default().Sheet().Rows(ledgerModel) {
		if !underCeiling(row.Facts, ceiling) {
			continue
		}
		if lanes.Serves(ledgerModel, row.ID.Lane) {
			return false
		}
		admitted++
	}
	return admitted > 0
}

// learnExcludedFromTheSet files the machines an account-policy refusal was
// about when the refusal itself named none — a request with no demand, whose
// set was only this process's ceiling and vetoes over the router's roster.
//
// IT LEARNS ONLY ON AN EXACT AGREEMENT, and it learns nothing otherwise. The
// router says how many machines the request's own filters left
// (`input_endpoint_count`) and that its account policy removed every one of
// them; the sheet says which machines those filters leave, as this process
// sees the roster. When the two counts are equal they are the same set, and
// each machine in it is excluded exactly as a demanded one would have been.
// When they differ — a sheet that has not caught up, a parameter filter the
// router applied first, an account list this process cannot see — the refusal
// stays what it was before this existed: a list that emptied the set, which
// implicates nobody. A body without the metadata (the sentence alone) never
// teaches this, because the count is the whole of the evidence.
//
// A WRONG LESSON IS BOUNDED. An exclusion keeps a machine from being demanded
// or ranked, never from answering, and the first answer it serves takes it back
// ([lanes.ClearAccountExclusion] from [Client.noteServed]).
func learnExcludedFromTheSet(model string, sent *providerPrefs, body []byte) {
	if sent == nil || len(sent.Only) > 0 || !accountExcluded(body) {
		return
	}
	count := accountRefusedCount(body)
	if count <= 0 {
		return
	}
	var set []string
	for _, row := range lanes.Default().Sheet().Rows(laneModel(model)) {
		lane := strings.TrimSpace(row.ID.Lane)
		if lane == "" || foldedIn(sent.Ignore, lane) || !underCeiling(row.Facts, sent.MaxPrice) {
			continue
		}
		set = append(set, lane)
	}
	if len(set) != count {
		return
	}
	for _, lane := range set {
		lanes.ExcludeForAccount(lane, "the account's own settings exclude it")
	}
}

// accountRefusedCount is how many machines the router says the request's own
// filters left before its account policy removed them all, zero when the body
// does not say or says the policy left some standing.
func accountRefusedCount(body []byte) int {
	var decoded struct {
		Error struct {
			Metadata struct {
				Input   int `json:"input_endpoint_count"`
				Reasons []struct {
					Count int `json:"endpoint_count"`
				} `json:"ineligibility_reasons"`
			} `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &decoded) != nil {
		return 0
	}
	meta := decoded.Error.Metadata
	if meta.Input <= 0 || len(meta.Reasons) == 0 {
		return 0
	}
	// "An endpoint may have matched multiple reasons", so the reasons are not
	// summed: one reason covering the whole input is the evidence that every
	// machine in it was removed by the account rather than by something else.
	for _, reason := range meta.Reasons {
		if reason.Count >= meta.Input {
			return meta.Input
		}
	}
	return 0
}

// underCeiling reports whether a machine's own tariff fits a ceiling, which is
// the router's own test: both prices at or under, in the ceiling's dollars per
// million. No ceiling admits everything.
func underCeiling(facts lanes.Facts, ceiling *maxPrice) bool {
	if ceiling == nil {
		return true
	}
	const perMillion = 1_000_000
	return facts.PriceIn*perMillion <= ceiling.Prompt && facts.PriceOut*perMillion <= ceiling.Completion
}

// foldedIn reports whether a list names a lane, spelled however the wire likes.
func foldedIn(list []string, lane string) bool {
	for _, held := range list {
		if equalLane(strings.TrimSpace(held), lane) {
			return true
		}
	}
	return false
}
