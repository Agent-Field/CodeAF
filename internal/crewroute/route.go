// Package crewroute picks the crew for one task: which model sits the worker,
// the planner and the checker seat, on which provider's route, for this piece
// of work and nothing longer.
//
// ── THE ONE SENTENCE IT IMPLEMENTS ──
//
// The /crew panel says what is allowed (it persists); the words in the ask say
// how hard to try this one task. Nothing else sticks. So there is no preset
// here and no mode: a caller hands over the task, the models the person allows
// on the providers they have connected, the seats they pinned, and at most one
// word of effort, and gets back one crew for this task.
//
// ── WHY IT ROUTES ON THE CLASS OF WORK ──
//
// The evidence behind it is in docs/design/model-pool/pareto-crewing.pdf. In
// short: on narrow fixes the cheapest crew was as mergeable as a dear one and
// a strong checker added nothing; on open-ended work the cheap crew alone was
// rarely mergeable, and a strong checker — and only a checker — made the
// difference. So the policy is read off a table ([prior]) rather than written
// into branches: classify the task ([Classify]), then pick each seat to
// maximise quality minus λ times cost.
//
// ── WHAT IT DOES NOT DO ──
//
// It does not escalate on its own. A done-verdict that misses real solves and
// passes failures would make an escalation loop spend on the wrong tasks, so
// [AutoEscalate] is off, and a stronger crew is something a person asks for
// (`redo stronger`) until the checker's agreement with a human reviewer is
// measured high enough to trust.
//
// It is PURE: no disk, no network, no clock. The same request gives the same
// crew, in the same order, every time — which is what lets a decision be
// logged, replayed and argued with. A decision costs well under a millisecond.
package crewroute

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// AutoEscalate is whether a crew is ever made stronger without somebody
// asking. It is OFF, and a constant rather than a setting, because the number
// that would justify turning it on — how often the checker's verdict agrees
// with a reviewer's — is a property of the checker, not a preference, and it
// is not yet high enough for automatic escalations to pay for themselves.
const AutoEscalate = false

// Seat is one of the crew's three seats, by the name a person reads.
type Seat string

const (
	// Worker does the work: the leaves of a task, every tool turn of it.
	Worker Seat = "worker"
	// Planner cuts the work into pieces and steers the run.
	Planner Seat = "planner"
	// Checker reads finished work against what it was supposed to do.
	Checker Seat = "checker"
)

// Seats lists the three in the order a crew is read out.
var Seats = []Seat{Worker, Planner, Checker}

// Model is a catalog row as the router reads it. Prices are dollars per
// token, as the catalog publishes them.
type Model struct {
	ID              string
	Open            bool
	PromptPrice     float64
	CompletionPrice float64
	CacheReadPrice  float64
	Intelligence    float64
	Coding          float64
	Agentic         float64
	Context         int
	// Tools is whether the model takes tool calls. A crew seat is an agent
	// loop, so a model that cannot call a tool cannot sit one.
	Tools bool
}

// RouteKind is how a route bills.
type RouteKind string

const (
	// Metered is billed per token at the model's published prices.
	Metered RouteKind = "metered"
	// Plan is a subscription login — a coding plan, a ChatGPT account — whose
	// marginal cost for one more task is taken as zero.
	Plan RouteKind = "plan"
	// Local is a model on this machine, which costs nothing per token.
	Local RouteKind = "local"
	// Free is a provider's free pool for a model (OpenRouter's `:free`): no
	// price, but rate-limited hard, dropped without notice, and allowed to log
	// or train on what it is sent. It is weighed at what it is EXPECTED to cost
	// ([routeCost]), used only when the person turned free routes on, and a
	// seat on it falls through to the same model's paid route first.
	Free RouteKind = "free"
)

// freeFailPrior is the chance a free route refuses a task's first call,
// before this install has any outcome of its own for that route: free pools
// are rate-limited by design, so the prior is pessimistic.
const freeFailPrior = 0.3

// freeLimitPenalty is what a free route is charged for its limits even when it
// answers — the waits and retries of a rate-limited pool — in dollars a task.
const freeLimitPenalty = 0.001

// Route is one way to reach a model: the provider, the id to send so the call
// goes that way, and how it bills.
type Route struct {
	Provider string
	Send     string
	Kind     RouteKind
	// FailRate is this install's learned chance that the route refuses a
	// task's first call, zero when nothing is known — a free route then reads
	// [freeFailPrior].
	FailRate float64
}

// Candidate is a model the person allows, with every route a connected
// provider offers it on, the caller's preferred route first.
type Candidate struct {
	Model  Model
	Routes []Route
}

// Pin is a seat the person fixed. Model is the id they wrote, Provider the
// route they pinned when they wrote `model@provider`, and Send and Kind the
// route the caller resolved for it.
type Pin struct {
	Model    string
	Provider string
	Send     string
	Kind     RouteKind
}

// Effort is the one word about how hard to try this task.
type Effort string

const (
	// EffortKnee is the default: the knee of the measured front.
	EffortKnee Effort = ""
	// EffortBest buys the most quality the table believes in, whatever it
	// costs (λ → 0, ties to the cheaper).
	EffortBest Effort = "best"
	// EffortCheap buys quality only where it is nearly free.
	EffortCheap Effort = "cheap"
)

// ParseEffort reads a person's word for effort. Empty is the knee; anything
// else that is not one of the two words is refused by the caller's own form.
func ParseEffort(word string) (Effort, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "":
		return EffortKnee, true
	case "best", "thorough", "strong", "strongest":
		return EffortBest, true
	case "cheap", "cheapest":
		return EffortCheap, true
	}
	return EffortKnee, false
}

// The price of quality, spelled once.
//
// THE KNEE is [Knee] quality points per dollar (prior.json). It sits where the
// measured front bends: the fix class's only paid upgrade, kimi in every seat,
// buys +0.29 points for about $0.33 (under one point per dollar) and is below
// it; the open-ended class's strong checker buys +4 points for about $0.09
// (over forty per dollar), and the step from a cheap strong-ish checker to the
// strong one, +1.9 for $0.085, is above it. So the default buys the checker
// on open-ended work and nothing on a fix — the routed policy the evidence
// measured — and it does so because of the prices, not because of a branch.
//
// cheapFactor makes cheap ten times as stingy: quality must come at under
// half a cent a point. stepFactor is one escalation step: a quarter of the
// price per point, so each step buys what cost up to four times as much.
const (
	cheapFactor = 10.0
	stepFactor  = 4.0
	maxSteps    = 6
)

// Request is everything one decision reads.
type Request struct {
	Task Task
	// Class, when set, is taken as given and the classifier is not asked — a
	// replay, or a caller that already knows.
	Class Class
	// Reading, when set, is the classifier's whole answer as the caller
	// already read it — the class, why, how sure, and a fix's reach — and is
	// taken as it stands. A caller that classifies first to key its learned
	// offset hands the reading on here rather than the class alone, which
	// dropped the reach and ran every complex fix on the simple fix's worker.
	Reading *Reading
	// Candidates are the allowed models reachable on a connected provider.
	Candidates []Candidate
	// Pins are the seats the person fixed. A pinned seat always runs its pin.
	Pins   map[Seat]Pin
	Effort Effort
	// Steps is how many escalation steps above the knee to start: the learned
	// offset for this repository and class, which decays as tasks are accepted.
	Steps int
	// Pace multiplies λ as a daily cap is approached ([Pace]); zero is one.
	Pace float64
	// Stronger is the crew that ran, when the person asked to redo the task
	// stronger: every unpinned seat is picked at least as strong, and the crew
	// as a whole strictly stronger, or the decision says it cannot be.
	Stronger *Decision
	// Again is the crew that ran and NEVER STARTED — its seats' first calls
	// were refused — when the person asks to redo it: the task never ran, so it
	// is asked again at the same λ on the next-best models, not escalated.
	Again *Decision
	// Avoid are model lineages that failed to start on this install recently
	// ([Lineage]). An unpinned seat is not given one while anything else can
	// sit it; a pin is never overruled.
	Avoid map[string]bool
	// Rescue is a seat's LAST RUNG being picked — the free pools when nothing
	// paid can be reached — rather than a crew being chosen: any model that
	// can sit the seat (tools, context, a route) is taken, best first, though
	// it publishes too little for the router to trust it as a first pick. A
	// seat that runs on a thinly described model says so on its line; a seat
	// that does not run at all says nothing useful.
	Rescue bool
}

// Pick is one seat's answer.
type Pick struct {
	Seat     Seat
	Model    string
	Provider string
	Send     string
	Kind     RouteKind
	Pinned   bool
	// Measured is whether Quality came from the evidence table rather than
	// the catalog's figures.
	Measured bool
	Quality  float64
	CostUSD  float64
	// EstUSD is what this seat is expected to cost the task ([table.estCost]):
	// nothing on a plan, a local model or a free pool.
	EstUSD float64 `json:",omitempty"`
}

// Decision is one task's crew.
type Decision struct {
	Class  Class
	Why    string
	Sure   bool
	Effort Effort
	Steps  int
	// Lambda is the price of a quality point the crew was picked at.
	Lambda float64
	Crew   []Pick
	// EstUSD is the crew's estimated cost for an ordinary task of its class.
	EstUSD  float64
	Quality float64
	// OneOff is a stronger redo of a crew whose every seat was pinned: the
	// pins were stepped over for this one run and are unchanged.
	OneOff bool
	// Considered is how many candidates the decision chose among.
	Considered int
	// Ladder is, for each unpinned seat, where the seat goes when its call
	// fails to start, in order: the same model on its next routes, then the
	// next qualified models at a similar cost. The caller adds what only it
	// knows — the last crew that worked, the model the person is talking to.
	Ladder map[Seat][]Pick `json:"-"`
	// Retried are the seats that moved down their ladder during the task, in
	// the order they moved, so the line can say so.
	Retried []Retry `json:",omitempty"`
	// Rungs are what a redo or an effort word changed against the crew it
	// would otherwise have run, seat by seat.
	Rungs []Retry `json:",omitempty"`
	// Note is one plain sentence the line ends on: an effort word that changed
	// nothing, or the free routes taken because nothing paid could be.
	Note string `json:",omitempty"`
	// Stopped is the one action a task stopped on when a seat had nowhere
	// left to go and the failure said why — credit, a key, a limit. The line
	// leads with it and offers no stronger redo, which could not help.
	Stopped string `json:",omitempty"`
	// Redo is whether this crew was asked for by a redo of a task that ran
	// before. Its own acceptance is the redo's, not a later task's: it must
	// not take back the step the redo just taught.
	Redo bool `json:",omitempty"`
	// Subclass is, for a bugfix, "complex" or "simple" ([complexFix]), with
	// Reach the signals that made it complex. The log keeps it; every line a
	// person reads still says bugfix.
	Subclass string `json:",omitempty"`
	Reach    string `json:",omitempty"`
	// CostFactor is what this install's own tasks of the class have cost
	// against their estimates ([router.CrewLog.CostFactor]); EstUSD carries
	// it. Zero is none learned yet.
	CostFactor float64 `json:",omitempty"`
}

// Unspent is the actual cost a line is drawn with when there is none worth
// saying: a task that stopped before anything was spent. The line then names
// no money at all, rather than a $0.000 that reads as a free success.
const Unspent = -2.0

// Retry is one seat moved from one model to another — down its ladder during
// a task, or up a rung on a redo — and why, when a failure moved it.
type Retry struct {
	Seat Seat
	From string
	To   string
	Why  string `json:",omitempty"`
}

// WithRung is the decision with one seat moved to another pick for the rest
// of the task — a rung of its ladder, or one the caller found — the estimate
// re-added and the move recorded, with why, for the line.
func (d Decision) WithRung(seat Seat, next Pick, why string) Decision {
	out := d
	out.Crew = append([]Pick(nil), d.Crew...)
	next.Seat = seat
	out.Retried = append(append([]Retry(nil), d.Retried...), Retry{Seat: seat, From: d.Seat(seat).Model, To: next.Model, Why: why})
	out.EstUSD, out.Quality = 0, 0
	for i := range out.Crew {
		if out.Crew[i].Seat == seat {
			out.Crew[i] = next
		}
		out.EstUSD += estOf(out.Crew[i])
		out.Quality += out.Crew[i].Quality
	}
	if out.CostFactor > 0 {
		out.EstUSD *= out.CostFactor
	}
	return out
}

// Seat is one seat's pick, the zero Pick for a seat the decision has none for.
func (d Decision) Seat(seat Seat) Pick {
	for _, pick := range d.Crew {
		if pick.Seat == seat {
			return pick
		}
	}
	return Pick{}
}

// NoCandidateError is a seat nobody pinned and nothing allowed can sit.
type NoCandidateError struct{ Seat Seat }

func (e NoCandidateError) Error() string {
	return fmt.Sprintf("no allowed model on a connected provider can sit the %s seat", e.Seat)
}

// ErrStrongest is a redo that asked for a stronger crew than the strongest
// one the allowed models make.
var ErrStrongest = errors.New("this is already the strongest crew the models you allow can make")

// Decide picks the crew for one task.
func Decide(r Request) (Decision, error) {
	t := prior()
	if r.Rescue {
		lenient := *t
		lenient.rescue = true
		t = &lenient
	}
	reading := readingOf(r)
	d := Decision{Class: reading.Class, Why: reading.Why, Sure: reading.Sure, Effort: r.Effort, Steps: r.Steps, Considered: len(r.Candidates)}
	pins := r.Pins
	if r.Stronger != nil && allPinned(pins) {
		// EVERY SEAT PINNED AND A STRONGER RUN ASKED FOR: the pins stay what
		// they are, and this one run steps over them.
		pins, d.OneOff = nil, true
	}
	lambda := baseLambda(t, r)
	d.Lambda = lambda
	candidates := r.Candidates
	avoid := r.Avoid
	if r.Again != nil {
		avoid = map[string]bool{}
		for lineage := range r.Avoid {
			avoid[lineage] = true
		}
		for _, pick := range r.Again.Crew {
			if !pick.Pinned {
				avoid[Lineage(pick.Model)] = true
			}
		}
	}
	if len(avoid) > 0 {
		candidates = avoiding(candidates, avoid)
	}
	crew, err := pickCrew(t, d.Class, candidates, pins, lambda)
	if err != nil {
		return Decision{}, err
	}
	// A LEARNED OFFSET IS RUNGS, NOT A PRICE: each step is one rung up the
	// front from the crew the knee picks ([stronger]), so a repository whose
	// work was redone once starts one rung higher — never at the top.
	for i := 0; i < r.Steps && i < maxSteps && r.Stronger == nil && r.Again == nil; i++ {
		ran := Decision{Crew: crew}
		up, _, err := stronger(t, d.Class, candidates, pins, lambda, &ran)
		if err != nil {
			break
		}
		crew = up
	}
	if d.Class == Bugfix {
		d.Subclass, d.Reach = "simple", reading.Complex
		if reading.Complex != "" {
			d.Subclass = "complex"
			if r.Stronger == nil && r.Again == nil {
				crew = withReachingWorker(t, crew, candidates)
			}
		}
	}
	if r.Stronger != nil {
		crew, d.Lambda, err = stronger(t, d.Class, candidates, pins, lambda, r.Stronger)
		if err != nil {
			return Decision{}, err
		}
	}
	d.Crew = crew
	// AN EFFORT WORD SAYS WHAT IT CHANGED, or that it changed nothing: the crew
	// the knee would have picked is the one to compare with.
	var knee *Decision
	if (r.Effort == EffortBest || r.Effort == EffortCheap) && r.Stronger == nil && r.Again == nil {
		plain := r
		plain.Effort, plain.Steps = "", 0
		if kneeCrew, err := pickCrew(t, d.Class, candidates, pins, baseLambda(t, plain)); err == nil {
			knee = &Decision{Crew: kneeCrew}
			if sameCrew(kneeCrew, crew) {
				if r.Effort == EffortBest {
					d.Note = "best · already the strongest measured crew"
				} else {
					d.Note = "cheap · already the cheapest crew"
				}
			}
		}
	}
	// A REDO SAYS WHAT IT CHANGED, seat by seat, so the card can show the rung
	// it took rather than a new crew to compare by eye.
	if ran := firstOf(firstOf(r.Stronger, r.Again), knee); ran != nil {
		for _, pick := range crew {
			if was := ran.Seat(pick.Seat); was.Model != "" && Lineage(was.Model) != Lineage(pick.Model) {
				d.Rungs = append(d.Rungs, Retry{Seat: pick.Seat, From: was.Model, To: pick.Model})
			}
		}
	}
	// EACH UNPINNED SEAT CARRIES ITS NEXT PICK, so a model that fails to start
	// is replaced inside the task rather than ending it: the best candidate of
	// another lineage at the λ the crew was picked at.
	for _, pick := range crew {
		if pick.Pinned {
			continue
		}
		if ladder := ladderFor(t, d.Class, pick, candidates, d.Lambda); len(ladder) > 0 {
			if d.Ladder == nil {
				d.Ladder = map[Seat][]Pick{}
			}
			d.Ladder[pick.Seat] = ladder
		}
	}
	for i := range d.Crew {
		d.Crew[i].EstUSD = pickEst(t, d.Class, d.Crew[i])
	}
	for _, pick := range d.Crew {
		d.EstUSD += estOf(pick)
		d.Quality += pick.Quality
	}
	return d, nil
}

// pickEst is one pick's expected cost for the estimate: nothing where the
// route bills nothing, the measured seat cost otherwise.
func pickEst(t *table, class Class, pick Pick) float64 {
	switch pick.Kind {
	case Plan, Local, Free:
		return 0
	}
	if m, ok := Snapshot(pick.Model); ok && pick.CostUSD > 0 {
		// The candidate's own prices, where they differ from the snapshot's,
		// move a measured seat's cost with them.
		if flat := t.seatCost(pick.Seat, m); flat > 0 {
			return t.estCost(class, pick.Seat, m) * pick.CostUSD / flat
		}
	}
	return t.estCost(class, pick.Seat, Model{ID: pick.Model}) + unmeasuredEst(t, class, pick)
}

// unmeasuredEst is an unmeasured pick's flat cost scaled by its class and
// seat, read back from the cost it was weighed at.
func unmeasuredEst(t *table, class Class, pick Pick) float64 {
	if _, ok := t.costs[cellKey{class, pick.Seat, Lineage(pick.Model)}]; ok {
		return 0
	}
	scale := t.costScale[seatKey{class, pick.Seat}]
	if scale <= 0 {
		scale = 1
	}
	return pick.CostUSD * scale
}

// estOf is a pick's expected cost: its estimate, or — for a pick made where
// no estimate was read, a rescue — what it was weighed at.
func estOf(pick Pick) float64 {
	switch {
	case pick.EstUSD > 0:
		return pick.EstUSD
	case pick.Kind == Plan || pick.Kind == Local || pick.Kind == Free:
		return 0
	}
	return pick.CostUSD
}

// baseLambda is the price of a quality point this request starts at: the
// knee, or the effort's own, moved by the learned steps and the day's pace.
func baseLambda(t *table, r Request) float64 {
	lambda := t.Knee
	switch r.Effort {
	case EffortBest:
		return 0
	case EffortCheap:
		lambda *= cheapFactor
	}
	if r.Pace > 0 {
		lambda *= r.Pace
	}
	return lambda
}

// firstOf is the first of two decisions that is set.
func firstOf(a, b *Decision) *Decision {
	if a != nil {
		return a
	}
	return b
}

// ladderRivals is how many other models a seat's ladder offers after its
// own model's routes.
const ladderRivals = 2

// ladderLook is how many times ladderRivals the ladder weighs before it puts
// those at a similar cost first; similarCost is how much dearer than the
// seat's pick "similar" allows.
const (
	ladderLook  = 3
	similarCost = 3.0
)

// ladderFor is where a seat goes when its call fails to start, in order:
//
//  1. THE SAME MODEL ON ITS NEXT ROUTES, cheapest first. A route refusing says
//     something about the route, and the model was picked on its own merits.
//  2. THE NEXT QUALIFIED MODELS for the seat at the same λ — the picks the
//     seat would have had without this model — so the fallback costs about
//     what the pick did rather than whatever is left.
func ladderFor(t *table, class Class, pick Pick, candidates []Candidate, lambda float64) []Pick {
	var ladder []Pick
	for _, c := range candidates {
		if Lineage(c.Model.ID) != Lineage(pick.Model) {
			continue
		}
		rest := append([]Route(nil), c.Routes...)
		used := map[string]bool{pick.Send: true}
		for {
			var left []Route
			for _, r := range rest {
				if !used[r.Send] {
					left = append(left, r)
				}
			}
			if len(left) == 0 {
				break
			}
			c.Routes = left
			next := pickOf(t, class, pick.Seat, c)
			used[next.Send] = true
			ladder = append(ladder, next)
			rest = left
		}
		break
	}
	// The rivals, in ONE pass: the best few other lineages by the score
	// [bestFor] ranks on, kept in order as they are found — and then those at
	// a SIMILAR COST to the seat's pick ahead of dearer ones, so a seat that
	// cannot start moves sideways before it moves up: a failed call is not a
	// request for a stronger crew.
	keep := ladderRivals * ladderLook
	own := Lineage(pick.Model)
	type rival struct {
		pick  Pick
		score float64
	}
	var rivals []rival
	for _, c := range candidates {
		if Lineage(c.Model.ID) == own {
			continue
		}
		next, ok := eligible(t, class, pick.Seat, c)
		if !ok {
			continue
		}
		score := next.Quality - lambda*weighedCost(t, class, pick.Seat, next)
		at := len(rivals)
		for at > 0 && score > rivals[at-1].score+1e-12 {
			at--
		}
		if at >= keep {
			continue
		}
		rivals = append(rivals, rival{})
		copy(rivals[at+1:], rivals[at:])
		rivals[at] = rival{pick: next, score: score}
		if len(rivals) > keep {
			rivals = rivals[:keep]
		}
	}
	near := math.Max(pick.CostUSD, t.costFloor(class, pick.Seat)) * similarCost
	sort.SliceStable(rivals, func(i, j int) bool {
		return rivals[i].pick.CostUSD <= near && rivals[j].pick.CostUSD > near
	})
	if len(rivals) > ladderRivals {
		rivals = rivals[:ladderRivals]
	}
	for _, r := range rivals {
		ladder = append(ladder, r.pick)
	}
	return ladder
}

// sameCrew is whether two crews seat the same models.
func sameCrew(a, b []Pick) bool {
	if len(a) != len(b) {
		return false
	}
	for _, pick := range a {
		if Lineage(seatOf(b, pick.Seat).Model) != Lineage(pick.Model) {
			return false
		}
	}
	return true
}

// seatOf is a crew's pick for a seat.
func seatOf(crew []Pick, seat Seat) Pick {
	if i := seatIndex(crew, seat); i >= 0 {
		return crew[i]
	}
	return Pick{}
}

// avoiding is the candidates without the lineages named — unless that would
// leave none, because a crew on a model that failed once is better than no
// crew at all, and the task then says why it stopped.
func avoiding(candidates []Candidate, avoid map[string]bool) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if !avoid[Lineage(c.Model.ID)] {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return candidates
	}
	return out
}

// allPinned is whether every seat is pinned.
func allPinned(pins map[Seat]Pin) bool {
	for _, seat := range Seats {
		if _, ok := pins[seat]; !ok {
			return false
		}
	}
	return true
}

// pickCrew picks every seat at one λ. The seats are independent — a crew's
// quality is the sum of its seats' and so is its cost — so the best crew is
// each seat's best pick, and no combination has to be searched.
func pickCrew(t *table, class Class, candidates []Candidate, pins map[Seat]Pin, lambda float64) ([]Pick, error) {
	crew := make([]Pick, 0, len(Seats))
	for _, seat := range Seats {
		if pin, ok := pins[seat]; ok {
			crew = append(crew, pinned(t, class, seat, pin, candidates))
			continue
		}
		pick, ok := bestFor(t, class, seat, candidates, lambda, math.Inf(-1))
		if !ok {
			return nil, NoCandidateError{Seat: seat}
		}
		crew = append(crew, pick)
	}
	return crew, nil
}

// bestFor is one seat's best pick at λ among the models whose quality is
// above floor. Ties go to the cheaper pick and then to the smaller id, so the
// answer never depends on the order the candidates arrived in.
func bestFor(t *table, class Class, seat Seat, candidates []Candidate, lambda, floor float64) (Pick, bool) {
	var best Pick
	var bestScore float64
	found := false
	for _, c := range candidates {
		pick, ok := eligible(t, class, seat, c)
		if !ok || pick.Quality <= floor+1e-9 {
			continue
		}
		score := pick.Quality - lambda*weighedCost(t, class, seat, pick)
		switch {
		case !found, score > bestScore+1e-12:
		case score < bestScore-1e-12:
			continue
		case pick.Measured && !best.Measured:
			// A MEASURED MODEL WINS A TIE: evidence over a guess.
		case !pick.Measured && best.Measured:
			continue
		case pick.CostUSD < best.CostUSD-1e-12:
		case pick.CostUSD > best.CostUSD+1e-12:
			continue
		case pick.Model < best.Model:
		default:
			continue
		}
		best, bestScore, found = pick, score, true
	}
	return best, found
}

// Scored is one candidate as a seat weighed it: the model, the route it
// would ride, its quality and cost as the router read them, and the score
// (quality − λ·cost) it was ranked by.
type Scored struct {
	Model    string    `json:"m"`
	Provider string    `json:"p,omitempty"`
	Kind     RouteKind `json:"k,omitempty"`
	Quality  float64   `json:"q"`
	CostUSD  float64   `json:"c"`
	Score    float64   `json:"s"`
}

// Explain is each seat's best n candidates at the λ a decision was made at,
// best first — what a router log row carries so a crew can be explained from
// the log alone.
func Explain(class Class, lambda float64, candidates []Candidate, n int) map[Seat][]Scored {
	t := prior()
	out := map[Seat][]Scored{}
	for _, seat := range Seats {
		var ranked []Scored
		for _, c := range candidates {
			pick, ok := eligible(t, class, seat, c)
			if !ok {
				continue
			}
			cost := weighedCost(t, class, seat, pick)
			ranked = append(ranked, Scored{Model: pick.Model, Provider: pick.Provider, Kind: pick.Kind,
				Quality: round3(pick.Quality), CostUSD: round3(cost), Score: round3(pick.Quality - lambda*cost)})
		}
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
		if len(ranked) > n {
			ranked = ranked[:n]
		}
		out[seat] = ranked
	}
	return out
}

// round3 keeps a logged figure to three places.
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// seatable is whether a candidate can sit a seat at all: it takes tool calls,
// it has a route, and its context holds the seat's work. A free pool is a
// ROUTE of its model, not a model ([Free]): whether the model may sit the seat
// is decided on the model's own merits, and the route is chosen after.
func seatable(seat Seat, c Candidate) bool {
	if !c.Model.Tools || len(c.Routes) == 0 {
		return false
	}
	return c.Model.Context <= 0 || c.Model.Context >= seatContext[seat]
}

// Seatable is [seatable] for a caller that offers models for a seat — a
// picker, a rescue — so it offers exactly what the router could seat.
func Seatable(seat Seat, c Candidate) bool { return seatable(seat, c) }

// seatContext is the least context window a model needs to sit each seat, in
// tokens: the worker holds a long agent loop over a repository, the checker
// reads the work and its diff, the planner reads a brief and writes a plan. A
// model that publishes no window is not refused for it.
var seatContext = map[Seat]int{Worker: 64_000, Checker: 64_000, Planner: 32_000}

// IsFree is whether an id names a free, rate-limited route (`…:free`).
func IsFree(id string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(id)), ":free")
}

// eligible is one candidate in one seat, and whether it may sit it: seatable,
// and — for a model the table did not measure — publishing the agentic index
// an agent-loop seat needs ([agenticKnown]) and read high enough to be
// believed ([table.credible]).
func eligible(t *table, class Class, seat Seat, c Candidate) (Pick, bool) {
	if !seatable(seat, c) {
		return Pick{}, false
	}
	pick := pickOf(t, class, seat, c)
	if t.rescue {
		return pick, true
	}
	if !pick.Measured && CanonicalOf(c.Model.ID).Variant == "" &&
		(!agenticKnown(seat, c.Model) || !evidenceKnown(seat, c.Model)) {
		return Pick{}, false
	}
	return pick, t.credible(class, seat, pick.Quality, pick.Measured)
}

// weighedCost is the cost a pick is weighed at: its own, or — for a model the
// table did not measure — no less than the cheapest measured model's
// in the seat and class ([table.costFloor]). The pick still reports what it
// costs.
func weighedCost(t *table, class Class, seat Seat, pick Pick) float64 {
	if pick.Measured {
		return pick.CostUSD
	}
	if canon := CanonicalOf(pick.Model); canon.Variant != "" && IsMeasured(canon.ID) {
		// A quantised copy of a measured model borrowed its evidence at a
		// discount ([quantDiscount]); its local zero is real.
		return pick.CostUSD
	}
	return math.Max(pick.CostUSD, t.costFloor(class, seat))
}

// pickOf is one candidate in one seat on its cheapest route. Routes of equal
// cost keep the caller's order, which puts the provider the person connected
// for that vendor ahead of a router.
func pickOf(t *table, class Class, seat Seat, c Candidate) Pick {
	q, measured := t.quality(class, seat, c.Model)
	metered := t.seatCost(seat, c.Model)
	fallback := metered
	if fallback <= 0 {
		fallback = t.costFloor(class, seat)
	}
	pick := Pick{Seat: seat, Model: c.Model.ID, Quality: q, Measured: measured, CostUSD: math.Inf(1)}
	for _, route := range c.Routes {
		cost := routeCost(route, metered, fallback)
		if cost < pick.CostUSD-1e-12 {
			pick.Provider, pick.Send, pick.Kind, pick.CostUSD = route.Provider, route.Send, route.Kind, cost
		}
	}
	return pick
}

// routeCost is what one more task is EXPECTED to cost on a route: what it
// bills — the metered price, nothing on a plan, a local model or a free pool —
// plus what its refusals cost: the chance it refuses a first call times the
// paid route the seat then falls to (fallback). A free pool's chance starts
// pessimistic and it pays a charge for its limits.
//
// A FREE ROUTE IS NEVER PRICED AT ZERO. Zero is what made a free pool nobody
// measured win a worker seat and die in its first second; its expected cost
// is what a person actually pays for choosing it.
func routeCost(route Route, metered, fallback float64) float64 {
	switch route.Kind {
	case Plan, Local:
		return route.FailRate * fallback
	case Free:
		rate := route.FailRate
		if rate <= 0 {
			rate = freeFailPrior
		}
		return rate*fallback + freeLimitPenalty
	}
	// A ROUTE THAT REFUSES SOME FIRST CALLS costs what it bills plus what its
	// refusals send elsewhere, at this install's learned rate.
	return metered + route.FailRate*fallback
}

// pinned is a pinned seat's pick: the pin, always, on the route the pin names
// when it names one. A pinned model the candidates do not carry — outside the
// catalog, or on a provider this table cannot price — still sits the seat on
// the send the caller resolved; its quality is read from the table when it
// can be and its cost is what the table knows, or nothing.
func pinned(t *table, class Class, seat Seat, pin Pin, candidates []Candidate) Pick {
	pick := Pick{Seat: seat, Model: pin.Model, Provider: pin.Provider, Send: pin.Send, Kind: pin.Kind, Pinned: true}
	for _, c := range candidates {
		if Lineage(c.Model.ID) != Lineage(pin.Model) {
			continue
		}
		found := pickOf(t, class, seat, c)
		if pin.Provider != "" {
			for _, route := range c.Routes {
				if strings.EqualFold(route.Provider, pin.Provider) {
					found.Provider, found.Send, found.Kind = route.Provider, route.Send, route.Kind
					metered := t.seatCost(seat, c.Model)
					found.CostUSD = routeCost(route, metered, math.Max(metered, t.costFloor(class, seat)))
				}
			}
		}
		if math.IsInf(found.CostUSD, 1) {
			found.CostUSD = 0
		}
		if pin.Send != "" && (found.Send == "" || strings.EqualFold(found.Provider, pin.Provider)) {
			// THE PIN'S OWN SEND ON ITS OWN ROUTE: the id the person wrote (or
			// the variant it resolved to), never the candidate's first row.
			found.Send = pin.Send
			if found.Kind == "" || pin.Kind == Free {
				found.Kind = pin.Kind
			}
		}
		found.Model = pin.Model
		found.Pinned = true
		return found
	}
	if m, ok := Snapshot(pin.Model); ok {
		pick.Quality, pick.Measured = t.quality(class, seat, m)
		if pick.Kind != Plan && pick.Kind != Local {
			pick.CostUSD = t.seatCost(seat, m)
		}
	}
	if pick.Send == "" {
		pick.Send = pin.Model
	}
	return pick
}

// stronger is a redo's crew: ONE RUNG UP, on one seat.
//
// Every seat keeps what ran; then the unpinned seat whose next rung buys the
// most — quality gained less the dearer price at one step below this λ — moves
// up to that rung and no further. The next rung is the least step up the
// seat's front ([nextRung]): the next model above it by quality that no other
// model beats on both quality and cost. A redo that jumped to the top of the
// catalog skipped every mid-price model and multiplied the bill for one
// sentence of dissatisfaction; a ladder is walked a rung at a time, and a
// second redo takes the next rung. Ties go to the checker, then the worker,
// then the planner — the checker is the lever the evidence found.
func stronger(t *table, class Class, candidates []Candidate, pins map[Seat]Pin, lambda float64, ran *Decision) ([]Pick, float64, error) {
	crew := make([]Pick, 0, len(Seats))
	for _, seat := range Seats {
		if pin, ok := pins[seat]; ok {
			crew = append(crew, pinned(t, class, seat, pin, candidates))
			continue
		}
		was := ran.Seat(seat)
		if was.Model == "" {
			pick, ok := bestFor(t, class, seat, candidates, lambda, math.Inf(-1))
			if !ok {
				return nil, 0, NoCandidateError{Seat: seat}
			}
			was = pick
		}
		// A one-off over every pin starts from the pins as they ran.
		was.Pinned = false
		crew = append(crew, was)
	}
	step := lambda / stepFactor
	at, gain := -1, math.Inf(-1)
	var up Pick
	for _, seat := range []Seat{Checker, Worker, Planner} {
		i := seatIndex(crew, seat)
		if i < 0 || crew[i].Pinned {
			continue
		}
		next, ok := nextRung(t, class, seat, candidates, crew[i])
		if !ok {
			continue
		}
		g := (next.Quality - crew[i].Quality) - step*(weighedCost(t, class, seat, next)-weighedCost(t, class, seat, crew[i]))
		if g > gain+1e-12 {
			at, gain, up = i, g, next
		}
	}
	if at < 0 {
		return nil, 0, ErrStrongest
	}
	crew[at] = up
	return crew, step, nil
}

// readingOf is the reading a decision is made on: the caller's own when it
// handed one on, the class given with the reach read off the task's words
// when it gave only the class, and the classifier's otherwise. A CLASS GIVEN
// IS NOT A REACH FORGOTTEN: a bugfix named by a replay or a caller is still
// read for reach whenever the task's words are there to read.
func readingOf(r Request) Reading {
	switch {
	case r.Reading != nil && (r.Class == "" || r.Class == r.Reading.Class):
		return *r.Reading
	case r.Class == "":
		return Classify(r.Task)
	}
	reading := Reading{Class: r.Class, Why: "given", Sure: true}
	if r.Class == Bugfix {
		_, body := taskTitle(r.Task.Text)
		reading.Complex = complexFix(body)
	}
	return reading
}

// withReachingWorker is a complex fix's crew: its worker one rung up the
// seat's front ([nextRung]) from the one the knee picked — the next model
// above it by quality that nothing beats on both quality and cost, not the
// top of the catalog. A pinned worker stays the pin, and a worker already at
// the top stays where it is.
func withReachingWorker(t *table, crew []Pick, candidates []Candidate) []Pick {
	i := seatIndex(crew, Worker)
	if i < 0 || crew[i].Pinned {
		return crew
	}
	next, ok := nextRung(t, Bugfix, Worker, candidates, crew[i])
	if !ok {
		return crew
	}
	out := append([]Pick(nil), crew...)
	out[i] = next
	return out
}

// seatIndex is where a seat's pick sits in a crew, -1 when it is not there.
func seatIndex(crew []Pick, seat Seat) int {
	for i, pick := range crew {
		if pick.Seat == seat {
			return i
		}
	}
	return -1
}

// nextRung is the least step up a seat's front from the pick it has: among the
// models above it by quality, those no other model beats on both quality and
// weighed cost, and of those the one with the least quality — ties to the
// cheaper.
func nextRung(t *table, class Class, seat Seat, candidates []Candidate, from Pick) (Pick, bool) {
	var above []Pick
	for _, c := range candidates {
		pick, ok := eligible(t, class, seat, c)
		if !ok || pick.Quality <= from.Quality+1e-9 || Lineage(pick.Model) == Lineage(from.Model) {
			continue
		}
		above = append(above, pick)
	}
	var best Pick
	found := false
	for i, p := range above {
		dominated := false
		for j, q := range above {
			if i == j {
				continue
			}
			pc, qc := weighedCost(t, class, seat, p), weighedCost(t, class, seat, q)
			if q.Quality >= p.Quality-1e-9 && qc <= pc+1e-12 && (q.Quality > p.Quality+1e-9 || qc < pc-1e-12) {
				dominated = true
				break
			}
		}
		if dominated {
			continue
		}
		if !found || p.Quality < best.Quality-1e-9 ||
			(math.Abs(p.Quality-best.Quality) <= 1e-9 && weighedCost(t, class, seat, p) < weighedCost(t, class, seat, best)) {
			best, found = p, true
		}
	}
	return best, found
}

// nextUp is the cheapest model whose quality in the seat is above floor.
func nextUp(t *table, class Class, seat Seat, candidates []Candidate, floor float64) (Pick, bool) {
	var best Pick
	found := false
	for _, c := range candidates {
		pick, ok := eligible(t, class, seat, c)
		if !ok || pick.Quality <= floor+1e-9 {
			continue
		}
		if !found || pick.Quality < best.Quality-1e-9 ||
			(math.Abs(pick.Quality-best.Quality) <= 1e-9 && (pick.CostUSD < best.CostUSD || (pick.CostUSD == best.CostUSD && pick.Model < best.Model))) {
			best, found = pick, true
		}
	}
	return best, found
}

// Pace is how a daily cap moves the price of a quality point: nothing until
// half the cap is spent, then λ grows as what is left shrinks — twice as
// stingy with a quarter of the cap left, five times with a tenth — so a day
// that is running hot drifts to cheaper crews before it reaches the wall.
// atCap is the wall itself: the day's spend has reached the cap. A cap of
// zero is no cap.
func Pace(spent, cap float64) (multiplier float64, atCap bool) {
	if cap <= 0 {
		return 1, false
	}
	if spent >= cap {
		return 1, true
	}
	frac := spent / cap
	if frac <= 0.5 {
		return 1, false
	}
	return 1 / (2 * (1 - frac)), false
}

// Gap is one class of work the allowed models leave without a seat the
// evidence says it needs.
type Gap struct {
	Class Class
	Seat  Seat
	Line  string
}

// strongCheckerFloor is the open-ended checker quality below which the
// allowed models have no strong checker: between the measured cheap checkers
// (1.0 and 3.1) and the measured strong one (5.0).
const strongCheckerFloor = 4.0

// Gaps names what the allowed models cannot cover. Today that is one thing,
// because it is the one lever the evidence found: open-ended work with no
// strong checker among the models allowed.
func Gaps(candidates []Candidate) []Gap {
	t := prior()
	best := math.Inf(-1)
	for _, c := range candidates {
		if !seatable(Checker, c) {
			continue
		}
		if q, _ := t.quality(OpenEnded, Checker, c.Model); q > best {
			best = q
		}
	}
	if best >= strongCheckerFloor {
		return nil
	}
	return []Gap{{Class: OpenEnded, Seat: Checker, Line: "no strong checker among the models you allow · open-ended work will be checked weakly"}}
}

// ── how a decision reads ────────────────────────────────────────────────────

// Line is the one line a task card and a headless run's summary print:
//
//	bugfix · worker glm-5.3-flash (openrouter) · checker glm-5.3-flash · $0.021 (est $0.023)
//
// pinMark is drawn in front of a pinned seat's model; the chat surface hands
// its own glyph and a headless door hands 📌. actual below zero is not known
// yet, and the line then ends on the estimate alone (the emptiness law: an
// unknown is absent, never $0.00).
func (d Decision) Line(pinMark string, actual float64) string {
	var b strings.Builder
	if len(d.Retried) > 0 {
		// A SEAT THAT MOVED DURING THE TASK IS SAID FIRST, plainly: the crew
		// running is not the crew picked, and why — before any figure a reader
		// could take for the picked crew's success.
		//
		// EACH SEAT SAYS ITS NET MOVE, once: where it started, where it is
		// now, the FIRST reason — the cause, which a free pool at its limit
		// further down never is — and how many it tried between — never the
		// whole trail, which is the router log's to keep. A line that listed
		// every rung of a long ladder wrapped past the card.
		b.WriteString("running on fallback crew")
		for _, seat := range Seats {
			var first, last Retry
			moves := 0
			for _, r := range d.Retried {
				if r.Seat != seat {
					continue
				}
				if moves == 0 {
					first = r
				}
				last, moves = r, moves+1
			}
			if moves == 0 {
				continue
			}
			b.WriteString(" · " + string(seat) + " " + ShortModel(first.From) + " → " + ShortModel(last.To))
			var note []string
			if first.Why != "" {
				note = append(note, first.Why)
			}
			if moves > 1 {
				note = append(note, "+"+strconv.Itoa(moves-1)+" tried")
			}
			if len(note) > 0 {
				b.WriteString(" (" + strings.Join(note, "; ") + ")")
			}
		}
		b.WriteString(" · ")
	}
	b.WriteString(d.Class.Word())
	worker, planner, checker := d.Seat(Worker), d.Seat(Planner), d.Seat(Checker)
	// EACH SEAT IS SAID ONCE, WITH ITS CHANGE: a seat a redo or an effort word
	// moved reads `planner glm-5.3-flash → kimi-k3` where the seat stands,
	// never the seat and then its rung again at the end.
	rung := map[Seat]Retry{}
	for _, r := range d.Rungs {
		rung[r.Seat] = r
	}
	said := func(pick Pick) string {
		if r, ok := rung[pick.Seat]; ok {
			return ShortModel(r.From) + " → " + seatModel(pick, pinMark)
		}
		return seatModel(pick, pinMark)
	}
	b.WriteString(" · worker ")
	b.WriteString(said(worker))
	if worker.Provider != "" {
		route := worker.Provider
		if worker.Kind == Free {
			route += " · free"
		}
		b.WriteString(" (" + route + ")")
	}
	// THE PLANNER IS NAMED WHEN IT IS NOT THE WORKER. A crew whose planner is
	// the worker's model says nothing a person reading the line needs; one
	// whose planner is another model is a crew of three, and says so.
	_, plannerMoved := rung[Planner]
	if planner.Model != "" && (plannerMoved || Lineage(planner.Model) != Lineage(worker.Model)) {
		b.WriteString(" · planner ")
		b.WriteString(said(planner))
	}
	b.WriteString(" · checker ")
	b.WriteString(said(checker))
	switch {
	case actual == Unspent:
	case actual >= 0:
		b.WriteString(" · " + Money(actual) + " (est " + Money(d.EstUSD) + ")")
	default:
		b.WriteString(" · est " + Money(d.EstUSD))
	}
	if d.Note != "" {
		b.WriteString(" · " + d.Note)
	}
	return b.String()
}

// seatModel is one seat's model the way the line names it: the name after the
// vendor, with the pin mark in front when the seat was pinned.
func seatModel(pick Pick, pinMark string) string {
	name := ShortModel(pick.Model)
	if sent := ShortModel(pick.Send); pick.Pinned && sent != "" && sent != name {
		// A PIN SENT AS ANOTHER ID SAYS WHICH: `deepseek-v4-flash → -0731`.
		if strings.HasPrefix(sent, name) {
			sent = sent[len(name):]
		}
		name += " → " + sent
	}
	switch {
	case pick.Pinned && strings.TrimSpace(pinMark) != "":
		return pinMark + " " + name
	case pick.Pinned:
		// NO MARK IS NO BLANK: a surface with no glyph for a pin says the word.
		return name + " (pinned)"
	}
	return name
}

// ShortModel is a model id without its vendor: the half a person reads.
func ShortModel(id string) string {
	id = strings.TrimSpace(id)
	if at := strings.LastIndex(id, "/"); at >= 0 {
		return id[at+1:]
	}
	return id
}

// Money spells a task's dollars: three places under a dollar, where the
// difference between routed crews lives, and two above it.
func Money(usd float64) string {
	if usd >= 1 {
		return fmt.Sprintf("$%.2f", usd)
	}
	return fmt.Sprintf("$%.3f", usd)
}

// Names lists the candidate ids a decision chose among, sorted — the field a
// logged decision carries so it can be analysed after the fact.
func Names(candidates []Candidate) []string {
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.Model.ID)
	}
	sort.Strings(names)
	return names
}
