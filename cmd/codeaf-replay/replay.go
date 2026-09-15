package main

import (
	"math"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/callrows"
	"github.com/Agent-Field/codeaf/internal/lane"
)

// ── THE WALK ────────────────────────────────────────────────────────────────
//
// One pass over the log per candidate, and inside a pass, one stream of moments
// in the order they really happened. A policy is taught a sighting exactly when
// the answer landed and asked for a machine exactly when the request went out —
// never the other way round, which is the single mistake that makes an offline
// evaluation flatter every policy that is good at hindsight.

// moment is one thing that happened. Exactly one of its four payloads is set,
// for the reason internal/lane's journal record says one thing: a line naming
// two observations is a line nobody can replay.
type moment struct {
	at time.Time
	// ask is a request going out, and the only kind of moment a policy is
	// QUESTIONED at.
	ask *asked
	// saw, judged and published are the three things a policy is TAUGHT.
	saw       *lane.Sighting
	class     string
	judged    *lane.Outcome
	published *lane.Row
	weight    float64
}

// momentsOf builds the whole ordered stream: the log's requests and what they
// taught, and the lane journal's own sightings where one has been kept.
func momentsOf(rows []callrows.Row, requests []asked, journal []seen) []moment {
	stream := make([]moment, 0, len(rows)+len(requests)+len(journal))
	for index := range requests {
		stream = append(stream, moment{at: requests[index].at, ask: &requests[index]})
	}
	for _, row := range rows {
		if !row.Finished() || row.Model == "" {
			continue
		}
		machine := answered(row)
		if machine == "" {
			continue
		}
		id := lane.ID{Model: lane.BareModel(row.Model), Lane: machine}
		one := drawOf(row)
		class := classOf(roleOf(row.Tag))
		if one.refused {
			outcome := lane.Outcome{ID: id, Refused: true, Reason: refusalWord(row), At: row.At}
			stream = append(stream, moment{at: row.At, judged: &outcome})
			continue
		}
		if one.ttft > 0 {
			sighting := lane.Sighting{
				ID:           id,
				TTFT:         time.Duration(row.TTFTms) * time.Millisecond,
				Gen:          time.Duration(row.Millis-row.TTFTms) * time.Millisecond,
				Tokens:       row.CompletionTokens,
				PromptTokens: row.PromptTokens,
				CachedTokens: row.CachedTokens,
				At:           row.At,
			}
			stream = append(stream, moment{at: row.At, saw: &sighting, class: class})
		}
		// A CUT STREAM TEACHES SPEED AND NOTHING ABOUT USABILITY. Its first token
		// and its writing rate were measured; whether the answer would have been
		// any good was never found out, and an accepted outcome invented for it
		// would be this build marking its own homework.
		if !one.censored {
			outcome := lane.Outcome{ID: id, Accepted: true, At: row.At}
			stream = append(stream, moment{at: row.At, judged: &outcome})
		}
	}
	for index := range journal {
		row := journal[index].row
		stream = append(stream, moment{at: journal[index].at, published: &row, weight: journal[index].weight})
	}
	sort.SliceStable(stream, func(i, j int) bool { return stream[i].at.Before(stream[j].at) })
	return stream
}

// refusalWords is the short machine word an outcome carries for the belief, by
// the status that produced it.
//
// IT IS A TABLE AND NOT A CLASSIFIER, and the difference is a law in this tree
// (internal/taxonomy's `TestOnlyTheTaxonomyTurnsAStatusIntoAMove`). A classifier
// reads a live error and DECIDES — retry, hop, give the turn back. This reads a
// JSON line off a finished day and hands internal/lane the same word its own
// availability axis was fed at the time, so that a candidate replaying the log
// learns what the live ledger learned and not something a second reading of the
// status column invented.
var refusalWords = map[int]string{
	429: "rate",
	404: "routing",
	400: "shape",
}

// refusalWord reads the sentence's own status before the column's, for the
// reason [callrows.SaidStatus] exists.
func refusalWord(row callrows.Row) string {
	if word, named := refusalWords[callrows.SaidStatus(row.Error)]; named {
		return word
	}
	if word, named := refusalWords[row.Status]; named {
		return word
	}
	return "refused"
}

// ── WHAT A PASS MEASURES ────────────────────────────────────────────────────

// answers is what one candidate said, one entry per replayable request, and the
// entry is empty for a request it had no opinion about.
//
// A PASS DECIDES AND TALLIES NOTHING. Four candidates scored on four different
// sets of requests cannot be compared at all — the one that gives an opinion
// only about the easy half would win every table by declining the hard half — so
// a pass records its answers and the bill is drawn afterwards, over the requests
// EVERY candidate answered and the world could price for every one of them.
type answers []string

// tally is one candidate's bill for one role class, over the common set.
type tally struct {
	scored int
	regret []float64
	// silent is how many of the common set's requests this candidate had no
	// opinion about — zero by construction, since a request it was silent about
	// is not in the common set. It is kept over the WHOLE set instead, beside
	// unpriced, so the table can say how much of the log each candidate declined
	// to be judged on.
	silent   int
	unpriced int
	asked    int
	// switches is how often this candidate changed machine between two
	// consecutive requests of one errand, and forfeited the prompt tokens those
	// switches threw away — the cost of changing your mind, which a regret alone
	// cannot see.
	switches  int
	forfeited int
	// held is how often it demanded the machine that really served, which is how
	// much of the log it is even disagreeing about.
	held int
}

// pass walks the whole stream once for one candidate and records what it said.
func pass(policy Policy, stream []moment, total int) answers {
	said := make(answers, total)
	for index := range stream {
		one := stream[index]
		switch {
		case one.saw != nil:
			policy.Saw(*one.saw, one.class)
		case one.judged != nil:
			policy.Judged(*one.judged)
		case one.published != nil:
			policy.Published(*one.published, one.weight)
		case one.ask != nil:
			said[one.ask.index] = policy.Demand(*one.ask)
		}
	}
	return said
}

// bill draws every candidate's account over the ONE set of requests all of them
// answered and the world can price for all of them.
//
// THE COMMON SET IS THE WHOLE OF THE COMPARISON. A mean regret over a candidate's
// own favourite requests is a number about that candidate's appetite and not
// about its judgement; the table's means are therefore over requests where every
// row of it has something to say, and the columns beside them say how much each
// one declined.
func bill(requests []asked, said map[string]answers, measured *world, look settings, light *spotlight) map[string]map[string]*tally {
	bills := map[string]map[string]*tally{}
	for name := range said {
		bills[name] = map[string]*tally{}
	}
	last := map[string]map[string]string{}
	for name := range said {
		last[name] = map[string]string{}
	}
	for _, one := range requests {
		if len(measured.machines(one.model)) < look.min {
			continue
		}
		read := one.role.Visible()
		bestMachine, bestFelt := measured.best(one.model, one.at, one.want, read, one.id)
		if bestMachine == "" || math.IsInf(bestFelt, 0) {
			continue
		}
		felt := map[string]float64{}
		common := true
		for name, answered := range said {
			held := bills[name][one.class()]
			if held == nil {
				held = &tally{}
				bills[name][one.class()] = held
			}
			held.asked++
			demand := answered[one.index]
			if demand == "" {
				held.silent++
				common = false
				continue
			}
			if demand == one.machine {
				held.held++
			}
			// A SWITCH IS COUNTED PER ERRAND AND NOT PER MODEL, because what a
			// switch forfeits is a prompt cache, and a prompt cache belongs to a
			// conversation rather than to a model.
			errand := one.run + "\x00" + one.node + "\x00" + one.model
			if before, ran := last[name][errand]; ran && before != demand {
				held.switches++
				held.forfeited += one.cached
			}
			last[name][errand] = demand
			cost := measured.felt(lane.ID{Model: one.model, Lane: demand}, one.at, one.want, read, one.id)
			felt[name] = cost
			if math.IsInf(cost, 0) {
				held.unpriced++
				common = false
			}
			if light != nil {
				if spotted := light.at[one.index]; spotted != nil {
					spotted.demands[name] = shown{machine: demand, felt: cost}
				}
			}
		}
		if !common {
			continue
		}
		for name, cost := range felt {
			regret := cost - bestFelt
			if regret < 0 {
				// The best machine is the best the world could price; a candidate
				// that beats it has found evidence the oracle could not, which
				// happens only through rounding on a tie. Nothing is owed below
				// zero.
				regret = 0
			}
			held := bills[name][one.class()]
			held.scored++
			held.regret = append(held.regret, regret)
		}
	}
	return bills
}

// ── THE MOMENTS THAT HURT ───────────────────────────────────────────────────

// worst is one request the log paid dearly for, and what every candidate would
// have done at exactly that moment.
//
// THE MOMENTS ARE DERIVED AND NEVER NAMED. A report that hard-coded three
// timestamps would answer the three questions somebody had on one afternoon and
// go quietly wrong the next day; these are simply the requests where the machine
// that really answered was furthest from the best one available, which is the
// definition that FOUND those three in the first place.
type worst struct {
	index    int
	at       time.Time
	model    string
	class    string
	role     lane.Role
	machine  string
	asked    string
	regret   float64
	best     string
	bestAt   float64
	servedAt float64
	demands  map[string]shown
}

// shown is one candidate's answer at one moment: what it demanded and what that
// would have cost.
type shown struct {
	machine string
	felt    float64
}

// spotlight is the handful of moments every candidate is shown at, indexed by
// THE REQUEST ITSELF so that a pass can fill its own column in as it walks past.
//
// The index is the key and a timestamp is not, because a timestamp is not
// unique in this log and is not unique in exactly the shape this table exists to
// show: a hedge fans several arms at one model in one millisecond, and the pool
// incidents are a handful of requests inside a few seconds. Keyed on the moment,
// two requests collide and one candidate's row would describe a request the
// heading does not, priced against a different answer shape.
type spotlight struct {
	worst []*worst
	at    map[int]*worst
}

// spotlightOf picks the requests where what actually served was furthest from
// what was available, ONE PER MACHINE PER MINUTE so that a single paced pool
// refusing eight times in forty seconds is one finding rather than eight.
func spotlightOf(requests []asked, measured *world, look settings, most int) *spotlight {
	type seenAt struct {
		machine string
		minute  time.Time
	}
	best := map[seenAt]*worst{}
	for _, one := range requests {
		if len(measured.machines(one.model)) < look.min || one.machine == "" {
			continue
		}
		read := one.role.Visible()
		bestMachine, bestFelt := measured.best(one.model, one.at, one.want, read, one.id)
		if bestMachine == "" || math.IsInf(bestFelt, 0) {
			continue
		}
		felt := measured.felt(lane.ID{Model: one.model, Lane: one.machine}, one.at, one.want, read, one.id)
		if math.IsInf(felt, 0) {
			continue
		}
		key := seenAt{machine: one.machine, minute: one.at.Truncate(time.Minute)}
		held := best[key]
		if held != nil && held.regret >= felt-bestFelt {
			continue
		}
		best[key] = &worst{
			index: one.index,
			at:    one.at, model: one.model, class: one.class(), role: one.role,
			machine: one.machine, asked: one.demanded, regret: felt - bestFelt,
			best: bestMachine, bestAt: bestFelt, servedAt: felt,
			demands: map[string]shown{},
		}
	}
	ranked := make([]*worst, 0, len(best))
	for _, one := range best {
		ranked = append(ranked, one)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].regret != ranked[j].regret {
			return ranked[i].regret > ranked[j].regret
		}
		return ranked[i].at.Before(ranked[j].at)
	})
	if len(ranked) > most {
		ranked = ranked[:most]
	}
	light := &spotlight{worst: ranked, at: map[int]*worst{}}
	for _, one := range ranked {
		light.at[one.index] = one
	}
	return light
}

// ── READING THE BILL ────────────────────────────────────────────────────────

func mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func sum(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total
}
