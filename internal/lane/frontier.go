package lane

import (
	"math"
	"sort"
	"strings"
)

// ── THE FRONTIER IS THE CANDIDATE SET ───────────────────────────────────────
//
// With seventeen lanes on one model, most of them are beaten outright: some
// other lane starts sooner AND writes faster AND costs less AND is more often
// right. A lane like that can never be the answer to any request, whatever λ
// is, so it should never be sampled, never probed and never hedged to. Pruning
// them first is what makes the exploration budget land only where it could
// change a decision, and it usually leaves three to five.
//
// THE COMPARISON IS AT THE p75, NOT THE MEAN. A lane nobody has measured much
// has a wide posterior, and at the p75 that width keeps it in the set: it might
// be good. Pruning on the mean would quietly make this a router that only ever
// uses what it already knows.
//
// Two steps happen here and they are different kinds of judgement. THE GATE is
// deterministic and is about capability: a lane that cannot take a tool call,
// cannot write the requested number of tokens, cannot read the prompt, serves
// four-bit weights, is half down, or returns answers this build could not use,
// is dropped outright — never sampled and found wanting. THE PRUNE is about
// dominance and nothing else: it removes lanes no request could want, and it
// removes them without knowing what this request wants.
//
// This file may not read a clock — see the note in choose.go. The beliefs it is
// handed have already been aged to [Request.Now] by the chooser.

const (
	// quartileZ is the standard normal deviate of the third quartile. Every
	// number this file compares is taken at it, on the pessimistic side of each
	// axis: the p75 of the first-token wait, and the p75 of the time per token,
	// which is the p25 of the RATE.
	quartileZ = 0.6744897501960817

	// UptimeFloor is the share of the last five minutes a lane must have been
	// answering to be worth choosing, in per cent. A lane below it is not slow,
	// it is intermittently absent, and the router's own fallback handles that
	// better than a preference for it would.
	UptimeFloor = 95.0

	// PriceCeilingMultiple is how far above the cheapest acceptable lane's own
	// output tariff another lane may charge, with nobody waiting.
	//
	// IT IS THE SAME 1.25 THE TRANSPORT ALREADY SENDS as `max_price`
	// (internal/provider's latencyPriceCeiling) and it is the same reasoning: a
	// lane 25% dearer is buying a latency edge somebody can feel, and a lane 4×
	// dearer is buying nothing a person notices. The difference is that this
	// ceiling is relative to the CHEAPEST LANE SERVING THIS MODEL rather than
	// to the model's published list price, because the list price is a figure
	// no endpoint is obliged to match.
	PriceCeilingMultiple = 1.25
)

// lowQuantization reports whether a lane serves weights too coarse to be
// chosen unasked. Four-bit weights are a different model wearing the same name
// — measurably worse at exactly the long-horizon work this build does — so they
// are opt-in rather than a lane that quietly wins on price.
func lowQuantization(quant string) bool {
	switch strings.ToLower(strings.TrimSpace(quant)) {
	case "fp4", "int4", "nf4", "q4", "int4_w4a16", "fp4_e2m1":
		return true
	}
	return false
}

// gateOptions are the answers the gate cannot get from the request.
type gateOptions struct {
	// allowLowQuantization lets four-bit lanes into the candidate set. It is
	// the settings row's answer and it defaults to no.
	allowLowQuantization bool
}

// capable reports whether a lane could serve this request at all.
//
// EVERY TEST HERE IS "KNOWN AND FAILING", never "unknown and assumed bad". The
// sheet does not publish every field for every lane, and a gate that read a
// zero context length as "cannot read this prompt" would refuse the whole
// candidate set the first time a router stopped publishing a column. The one
// exception is tools, where the sheet's word is a flag and its absence is a
// lane that does not take tool calls — which is the fact the flag exists to
// state.
func capable(belief Belief, req Request, opts gateOptions) bool {
	facts := belief.Facts
	if req.Tools && !facts.Tools {
		return false
	}
	if req.MaxTokens > 0 && facts.MaxOut > 0 && facts.MaxOut < req.MaxTokens {
		return false
	}
	if need := req.PromptTokens + req.MaxTokens; need > 0 && facts.Context > 0 && facts.Context < need {
		return false
	}
	if !opts.allowLowQuantization && lowQuantization(facts.Quant) {
		return false
	}
	if facts.Uptime5m > 0 && facts.Uptime5m < UptimeFloor {
		return false
	}
	// QUALITY IS A GATE AND NEVER A WEIGHT. A lane whose believed share of
	// usable answers is under what this request needs leaves the candidate set
	// until the belief recovers; it is never traded off against a cheaper price,
	// because that trade is how a router learns to ship wrong answers cheaply.
	if req.QualityNeed > 0 && belief.Quality.Known() && belief.Quality.Mean() < req.QualityNeed {
		return false
	}
	return true
}

// scoredAt is one capable lane with its p75 numbers, before any request-shaped
// scalar has been applied. Score is left at zero here: this file ranks nothing.
func scoredAt(belief Belief, req Request, cached int) Scored {
	return Scored{
		ID: belief.ID,
		// The pessimistic quartile of each: the wait is asked at its long side
		// and the rate at its slow one, which are the same three-quarter point
		// seen from the two directions a person notices.
		TTFT:    belief.TTFT.Quantile(quartileZ),
		Rate:    belief.Rate.Quantile(-quartileZ),
		Price:   PriceWithCache(belief.Facts, req, cached),
		Quality: belief.Quality.Mean(),
	}
}

// frontierFor is the candidate set for one request: the lanes that could serve
// it, minus the ones no request could want.
//
// cached answers how many of this request's prompt tokens a lane is believed to
// be holding already, which is what makes the price path-dependent (see
// [PriceWithCache]). It is a function rather than a map so that the chooser's
// prefix memory stays the chooser's.
func frontierFor(beliefs []Belief, req Request, opts gateOptions, cached func(ID) int) []Scored {
	candidates := make([]Scored, 0, len(beliefs))
	facts := make([]Facts, 0, len(beliefs))
	for _, belief := range beliefs {
		if belief.ID.Zero() || !capable(belief, req, opts) {
			continue
		}
		held := 0
		if cached != nil {
			held = cached(belief.ID)
		}
		candidates = append(candidates, scoredAt(belief, req, held))
		facts = append(facts, belief.Facts)
	}
	// A stable order before anything else, so that two runs of the same choice
	// with the same beliefs agree down to the ties. The ledger promises no
	// order at all.
	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return candidates[order[a]].ID.Lane < candidates[order[b]].ID.Lane
	})
	sorted := make([]Scored, 0, len(candidates))
	sortedFacts := make([]Facts, 0, len(candidates))
	for _, index := range order {
		sorted = append(sorted, candidates[index])
		sortedFacts = append(sortedFacts, facts[index])
	}
	sorted = underPriceCeiling(sorted, sortedFacts, req)
	return paretoFront(sorted, req.QualityNeed)
}

// underPriceCeiling drops lanes that charge more than this request can justify.
//
// THE RULE, and it has two halves because λ is what decides which applies:
//
//   - With nobody waiting (λ = 0) the ceiling is [PriceCeilingMultiple] times
//     the cheapest capable lane's own output tariff. A dearer lane is buying
//     speed that is worth nothing here, so it is not worth its money.
//   - With somebody waiting (λ > 0) the ceiling widens by what λ can justify:
//     the most a lane may cost above the cheapest is the seconds it could
//     possibly save, priced through λ — and no lane can save more than the
//     whole of the cheapest lane's perceived wait. That bound is deliberately
//     generous, because the score below already pays the difference honestly;
//     the ceiling is here to refuse the absurd, not to make the decision.
//
// A model whose lanes publish no output tariff at all has no ceiling, for the
// reason the transport's own ceiling has none: refusing lanes on a number
// nobody published is worse than paying an unknown price.
func underPriceCeiling(candidates []Scored, facts []Facts, req Request) []Scored {
	cheapest := 0.0
	cheapestIndex := -1
	for index, lane := range facts {
		if lane.PriceOut <= 0 {
			continue
		}
		if cheapestIndex < 0 || lane.PriceOut < cheapest {
			cheapest, cheapestIndex = lane.PriceOut, index
		}
	}
	if cheapestIndex < 0 {
		return candidates
	}
	ceiling := cheapest * PriceCeilingMultiple
	if lambda := valueOfTime(req); lambda > 0 {
		answer := req.Visible + req.Hidden
		floor := candidates[cheapestIndex]
		perceived := PerceivedSeconds(floor.TTFT/1000, floor.Rate, req.Visible, req.Hidden)
		if answer > 0 && perceived > 0 && !math.IsInf(perceived, 1) {
			if headroom := cheapest + (perceived/lambda)/float64(answer); headroom > ceiling {
				ceiling = headroom
			}
		}
	}
	kept := make([]Scored, 0, len(candidates))
	for index, candidate := range candidates {
		if facts[index].PriceOut > 0 && facts[index].PriceOut > ceiling {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}

// paretoFront is the subset of candidates that no other candidate beats on
// every axis at once. It returns them in the order they were given.
//
// FOUR AXES, all at the p75: the first-token wait, the time per token, the
// cache-aware price of this request, and the believed share of usable answers.
// A lane is dropped only when another lane is at least as good on all four and
// strictly better on one — which is the definition of a lane that could not be
// the answer to ANY request, whatever λ turns out to be.
//
// unknownQuality is what a lane nobody has judged counts as on the quality
// axis, and the caller passes the request's own need. It is the neutral
// reading: an unmeasured lane neither wins nor loses on quality, so it is
// pruned on speed and price like everything else instead of being condemned for
// a measurement nobody took — and [Scored.Quality] keeps reporting the honest
// zero, because the emptiness law is about what a person is shown.
func paretoFront(candidates []Scored, unknownQuality float64) []Scored {
	quality := func(candidate Scored) float64 {
		if candidate.Quality <= 0 {
			return unknownQuality
		}
		return candidate.Quality
	}
	// The time per token, which is the axis a rate belongs on: a lane that
	// never finishes is worse than any finite one, and saying so here keeps the
	// comparison below free of special cases.
	perToken := func(candidate Scored) float64 {
		if candidate.Rate <= 0 {
			return math.Inf(1)
		}
		return 1 / candidate.Rate
	}
	survivors := make([]Scored, 0, len(candidates))
	for _, candidate := range candidates {
		beaten := false
		for _, rival := range candidates {
			if rival.ID == candidate.ID {
				continue
			}
			atLeastAsGood := rival.TTFT <= candidate.TTFT &&
				perToken(rival) <= perToken(candidate) &&
				rival.Price <= candidate.Price &&
				quality(rival) >= quality(candidate)
			betterSomewhere := rival.TTFT < candidate.TTFT ||
				perToken(rival) < perToken(candidate) ||
				rival.Price < candidate.Price ||
				quality(rival) > quality(candidate)
			if atLeastAsGood && betterSomewhere {
				beaten = true
				break
			}
		}
		if !beaten {
			survivors = append(survivors, candidate)
		}
	}
	return survivors
}
