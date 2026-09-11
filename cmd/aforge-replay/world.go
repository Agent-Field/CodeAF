package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/callrows"
	"github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── WHAT A MACHINE WAS DOING AT A MOMENT ────────────────────────────────────
//
// A replay's whole difficulty is the counterfactual: the log says what the
// machine we ASKED did, and a candidate policy wants to be scored on a machine
// we did not ask. There is exactly one honest source for that, and it is the
// same file — the other requests, on the same model, to that machine, around
// that moment. Ten days of this log carry between two and twenty-six machines
// per model and about a thousand finished requests a day, so for most requests
// there really is an answer from most of the alternatives within a quarter of an
// hour either side.
//
// THE WINDOW IS THE ESTIMATOR AND IT IS REPORTED. Nothing here pretends the
// counterfactual is observed. What is claimed is narrower and checkable: a
// machine's answers a few minutes either side of a moment are the best available
// account of what it would have done AT that moment, and the report says how
// good that account is by scoring the machine we DID ask both ways — from its
// own row, and from the window — and printing the disagreement (report.go's
// calibration line).

// draw is one measured answer from one machine: what it did, and whether it did
// it at all.
type draw struct {
	at time.Time
	// id is the attempt this draw came from, and it is here for ONE reason: a
	// machine must never be priced using the very answer being scored. The row
	// that served a request sits inside that request's own window, so leaving it
	// in prices the machine that answered on its own worst moment while every
	// alternative is priced on moments that have nothing to do with it — a bias
	// against whatever actually happened, which is exactly the finding this
	// instrument would then be manufacturing.
	id string
	// ttft is the wait before the first token in milliseconds and rate the
	// output tokens a second over the writing window. Both are zero on a draw
	// that measured neither — a refusal, or an answer too short to rate.
	ttft float64
	rate float64
	// refused is a machine that did not answer: a paced pool, an account 404, an
	// upstream refusal inside an opened 200. It is the AVAILABILITY axis, kept
	// apart from speed for the reason internal/lane keeps it apart — a pool that
	// is full now is usually fine in five minutes, and a machine that writes at
	// two tokens a second is not.
	refused bool
	// censored is an answer this build cut off rather than one the machine
	// finished: a hedge's losing arm, a stream the guard ended, a caller that
	// walked away. ITS SPEED IS MEASURED AND ITS FINISHING IS NOT, so it counts
	// as evidence of how fast the machine was writing and as no evidence at all
	// about whether it would have got to the end. Every number computed with one
	// in it is therefore a LOWER BOUND on the wait, and the report says how many
	// there were.
	censored bool
}

// world is every machine's measured history, indexed for the one question this
// program asks of it: what was this machine doing on this model at this moment.
type world struct {
	draws  map[lane.ID][]draw
	served map[string]map[string]bool
	window time.Duration
	// censored and refusals are the headline honesty counters: how much of the
	// evidence is a lower bound, and how much of it is a machine that said no.
	censored int
	refusals int
	clean    int
	// priced and stale are this instrument's account of its own reach: how many
	// times a machine was priced, and how old the evidence was on the occasions
	// the window held none. They are counted as the pricing happens rather than
	// derived afterwards, because the only question a reader has about a regret
	// is what it was computed from.
	priced int
	stale  []time.Duration
}

// newWorld reads every finished row of the log into the machine that ANSWERED
// it.
//
// IT IS KEYED ON `served` AND NEVER ON `lane`, which is the census's own law
// (`TestLaneHealthIsKeyedOnTheMachineThatServedAndNeverOnTheOneAskedFor`) and is
// load-bearing twice over here: the router honours a preference only when
// fallbacks are off, so a log keyed on the machine we asked for is a log of our
// own intentions rather than of anybody's behaviour — and the three incidents
// this instrument was written for are all rows where the two differ.
func newWorld(rows []callrows.Row, window time.Duration) *world {
	built := &world{draws: map[lane.ID][]draw{}, served: map[string]map[string]bool{}, window: window}
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
		one.id = row.ID
		built.draws[id] = append(built.draws[id], one)
		if built.served[id.Model] == nil {
			built.served[id.Model] = map[string]bool{}
		}
		built.served[id.Model][machine] = true
		switch {
		case one.refused:
			built.refusals++
		case one.censored:
			built.censored++
		default:
			built.clean++
		}
	}
	for id := range built.draws {
		sort.SliceStable(built.draws[id], func(i, j int) bool {
			return built.draws[id][i].at.Before(built.draws[id][j].at)
		})
	}
	return built
}

// viaMachine is how a router's own refusal names the machine that refused it —
// `API error (429): Provider returned error (via Wafer: …)`.
//
// IT IS READ BECAUSE THE COLUMN IS SOMETIMES EMPTY AND THE SENTENCE NEVER IS.
// A refusal that never opened a stream can arrive with no `served` at all, and
// filing it under the machine we merely ASKED for would credit a refusal to a
// machine that was never reached — which is precisely the misreading that let
// eight identical requests go to one paced pool on 2026-09-11.
var viaMachine = regexp.MustCompile(`via ([A-Za-z0-9 .\-]+?)[:)]`)

// answered is the machine this row is evidence about: the one that served it,
// or the one its own refusal names.
func answered(row callrows.Row) string {
	if machine := strings.TrimSpace(row.Served); machine != "" {
		return machine
	}
	if found := viaMachine.FindStringSubmatch(row.Error); found != nil {
		return strings.TrimSpace(found[1])
	}
	return ""
}

// ratedFloor is the shortest answer worth reading a WRITING RATE off, in output
// tokens.
//
// It is not a threshold anybody chose: an answer of a handful of tokens is one
// network round trip dressed as a generation, and its apparent rate measures the
// handshake. Thirty-two is the same floor internal/lane's own ledger teaches a
// rate above, so this program and the belief it is judging agree about which
// answers said anything about speed.
const ratedFloor = 32

// drawOf reads one finished row as evidence about the machine that answered it.
func drawOf(row callrows.Row) draw {
	one := draw{at: row.At}
	switch {
	case row.Exhaust():
		// A hedge's losing arm is NOT a refusal and never was. The question it
		// was sent for was answered, by this build's own design; what its row
		// cannot say is how long this machine would have taken.
		one.censored = true
	case row.Failed():
		one.refused = true
	}
	if strings.Contains(strings.ToLower(row.Error), "context canceled") || row.AppliedWord != "" {
		// A stream OUR OWN bounds ended, or a caller that walked away. The same
		// reading as a hedge's loser: measured speed, unmeasured finishing.
		one.censored, one.refused = true, false
	}
	if one.refused {
		return one
	}
	one.ttft = float64(row.TTFTms)
	if writing := row.Millis - row.TTFTms; writing > 0 && row.CompletionTokens >= ratedFloor {
		one.rate = float64(row.CompletionTokens) / (float64(writing) / 1000)
	}
	return one
}

// machines is every machine seen to answer for a model, in a stable order.
func (w *world) machines(model string) []string {
	seen := w.served[model]
	if len(seen) == 0 {
		return nil
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// inside is every draw this machine made within the window of a moment, refusals
// and all. It is what the AVAILABILITY reading is taken over: how often this
// machine was answering at the time, which is a fact about the window and not
// about any one answer.
//
// exclude is the attempt being scored: its own row is never evidence about it.
func (w *world) inside(id lane.ID, at time.Time, exclude string) []draw {
	all := w.draws[id]
	first := sort.Search(len(all), func(i int) bool { return !all[i].at.Before(at.Add(-w.window)) })
	last := sort.Search(len(all), func(i int) bool { return all[i].at.After(at.Add(w.window)) })
	kept := make([]draw, 0, last-first)
	for _, one := range all[first:last] {
		if one.id != "" && one.id == exclude {
			continue
		}
		kept = append(kept, one)
	}
	return kept
}

// nearestAnswers is the closest ANSWERS this machine gave either side of a
// moment, and how far away the nearest of them was.
//
// THE WINDOW IS A PREFERENCE AND NOT A WALL, for two reasons that are really one.
// A machine that answered twenty minutes before a request and twenty after it was
// plainly alive, and refusing to price it would leave the table comparing
// candidates only on requests where every one of them happened to pick a busy
// machine — a seventh of the log, chosen by the candidates themselves. AND A
// MACHINE THAT ONLY REFUSED IN THE WINDOW IS THE CASE THAT MATTERS MOST: a paced
// pool has no answer inside its own bad half-hour, and a reading that called it
// "cannot say" instead of "you will be refused four times in five" would make the
// eight identical requests to one full pool on 2026-09-11 14:39 invisible to this
// instrument. So the SPEED comes from its nearest answer and the AVAILABILITY
// from the window it was actually in, which is exactly the pair choose.go divides.
func (w *world) nearestAnswers(id lane.ID, at time.Time, exclude string) ([]draw, time.Duration) {
	all := w.draws[id]
	first := sort.Search(len(all), func(i int) bool { return !all[i].at.Before(at) })
	var found []draw
	age := time.Duration(0)
	for _, step := range []int{-1, 1} {
		index := first
		if step < 0 {
			index = first - 1
		}
		for index >= 0 && index < len(all) {
			one := all[index]
			index += step
			if one.refused || one.ttft <= 0 || one.rate <= 0 || (one.id != "" && one.id == exclude) {
				continue
			}
			far := gap(one.at, at)
			if far > stopBelieving {
				break
			}
			found = append(found, one)
			if age == 0 || far < age {
				age = far
			}
			break
		}
	}
	return found, age
}

// stopBelieving is how far from a moment an answer stops being evidence about
// it, and it is the BELIEF'S OWN FORGETTING rather than a figure chosen here:
// internal/lane ages a posterior on [lane.HalfLife], so after [forgotten] of
// those an observation is worth about a thousandth of a fresh one and the live
// build has already stopped acting on it. Pricing a machine from an answer older
// than that would be this instrument claiming to know something the thing it is
// judging has deliberately forgotten — and on this log the unbounded version
// reached for answers a median of nineteen hours away, which is a different day
// and a different afternoon's pool.
const stopBelieving = forgotten * lane.HalfLife

// gap is how far apart two moments are, in either direction.
func gap(one, other time.Time) time.Duration {
	if one.Before(other) {
		return other.Sub(one)
	}
	return one.Sub(other)
}

// ── WHAT ASKING A MACHINE WOULD HAVE COST ───────────────────────────────────

// shape is what one request was asking for: the work, not the machine.
//
// THE WORK COMES FROM THE REQUEST AND THE SPEED FROM THE MACHINE, which is the
// whole of what makes two machines comparable. Scoring a candidate by how long
// its own nearest answer happened to take would compare a one-line title with a
// six-thousand-token task step and call the difference routing.
type shape struct {
	visible int
	hidden  int
}

// felt is how long this request is expected to take to a usable answer on one
// machine, in seconds, computed from the machine's own rows.
//
// IT IS [rolePatience.expected] READ OFF THE LOG INSTEAD OF OFF A BELIEF, and it
// is deliberately the same quantity, term for term, because a chooser judged by
// a different number from the one it optimises is a chooser judged by nothing:
//
//	the first token      the machine's own first-token waits in the window, read
//	                     at the role's risk quantile — the median for a role
//	                     nobody watches, whose many small calls are summed, and
//	                     the ninetieth for a role somebody reads, who remembers
//	                     the minute and not the three seconds (rolePatience.riskZ)
//	the writing          the median rate over the same draws. The risk is taken
//	                     on the first token ALONE, exactly as expected does it,
//	                     rather than on both — the joint tail of two quantiles is
//	                     a number neither the chooser nor anybody else computes
//	the request's work   [lane.PerceivedSeconds] over this request's own visible
//	                     and hidden tokens, so a fast machine is fast for the work
//	                     that was actually asked for
//	how often it         divided by the share of the window's draws that were
//	answers at all       answers, because a machine that refuses four requests in
//	                     five is asked five times for one answer and the person
//	                     sits through all five — the same fold choose.go applies
//	                     with [lane.Belief.Serving]
//
// It answers +∞ for a machine with nothing to read in the window, which is the
// honest reading of "we cannot say" and is what keeps such a machine out of the
// best-in-hindsight without pretending it was bad.
func (w *world) felt(id lane.ID, at time.Time, want shape, read bool, exclude string) float64 {
	felt, _ := w.feltWithAge(id, at, want, read, exclude)
	return felt
}

// feltWithAge is [world.felt] with the age of the evidence it used beside it,
// which is what the report's staleness line counts.
func (w *world) feltWithAge(id lane.ID, at time.Time, want shape, read bool, exclude string) (float64, time.Duration) {
	window := w.inside(id, at, exclude)
	answers := rateable(window)
	age := time.Duration(0)
	if len(answers) == 0 {
		answers, age = w.nearestAnswers(id, at, exclude)
	}
	if len(answers) == 0 {
		return math.Inf(1), 0
	}
	w.priced++
	if age > 0 {
		w.stale = append(w.stale, age)
	}
	waits := make([]float64, 0, len(answers))
	rates := make([]float64, 0, len(answers))
	for _, one := range answers {
		waits = append(waits, one.ttft)
		rates = append(rates, one.rate)
	}
	ttft := quantile(waits, riskQuantile(read))
	rate := quantile(rates, 0.5)
	felt := lane.PerceivedSeconds(ttft/1000, rate, want.visible, want.hidden)
	if len(window) > 0 {
		felt /= serving(len(rateable(window))+refusedNone(window), len(window))
	}
	return felt, age
}

// rateable is the draws that measured both halves of an answer. A refusal
// measured neither, and an answer of a handful of tokens rated the handshake.
func rateable(draws []draw) []draw {
	kept := make([]draw, 0, len(draws))
	for _, one := range draws {
		if !one.refused && one.ttft > 0 && one.rate > 0 {
			kept = append(kept, one)
		}
	}
	return kept
}

// refusedNone is how many of these draws were answers this program could not
// RATE but which the machine nevertheless gave — an answer too short to time, a
// stream cut before it had written enough.
//
// THEY COUNT TOWARD AVAILABILITY AND NOT TOWARD SPEED, which is the distinction
// internal/lane draws in the same place: a machine that answered is a machine
// that answered, whether or not the answer said anything about how fast it
// writes. Folding them in with the refusals would price a machine that gives
// many short answers as a machine that turns requests away.
func refusedNone(draws []draw) int {
	unrated := 0
	for _, one := range draws {
		if !one.refused && (one.ttft <= 0 || one.rate <= 0) {
			unrated++
		}
	}
	return unrated
}

// riskQuantile is where this role's expectation is taken, and it is the same
// rule [rolePatience.riskZ] states rather than a second one. A role nobody reads
// makes many small calls whose SUM is what matters, and the estimator of a sum
// is the typical draw; a role somebody reads pays each draw separately and
// remembers the bad one, so its expectation is taken where an unlucky draw
// lands. The ninetieth is the quantile `z90` names in internal/lane, spelled
// here as the quantile because a sample has no log-normal to take a z of.
func riskQuantile(read bool) float64 {
	if read {
		return 0.9
	}
	return 0.5
}

// serving is the share of a window's requests this machine answered, and it is
// FLOORED AT ONE IN N rather than at a constant: a machine seen n times cannot
// honestly be claimed to answer less often than once in n, and a floor derived
// from the evidence is a floor that tightens as the evidence grows. Without one,
// a machine that refused every draw in a window would divide by zero and arrive
// as infinitely bad rather than as badly measured.
func serving(answers, draws int) float64 {
	if draws <= 0 {
		return 1
	}
	share := float64(answers) / float64(draws)
	if floor := 1 / float64(draws); share < floor {
		return floor
	}
	return share
}

// best is the machine a chooser with hindsight would have demanded, and what
// asking it would have cost.
func (w *world) best(model string, at time.Time, want shape, read bool, exclude string) (string, float64) {
	name, cost := "", math.Inf(1)
	for _, machine := range w.machines(model) {
		felt := w.felt(lane.ID{Model: model, Lane: machine}, at, want, read, exclude)
		if felt < cost {
			name, cost = machine, felt
		}
	}
	return name, cost
}

// quantile is the value at a share of the way through a sample, by nearest rank.
// Nearest rank rather than an interpolation because the samples here are small —
// a machine may have three answers in a window — and interpolating between two
// measurements invents a third.
func quantile(values []float64, at float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(math.Ceil(at*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
