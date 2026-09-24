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
// On 22 real GitHub issues, every crew scored blind by two reviewers, the
// cheapest crew — glm-5.3-flash in every seat — fixed narrow bugs as well as
// kimi-k3 in every seat did (6.57 against 6.86 out of ten, a difference whose
// interval straddles zero) at a fifteenth of the price, and a kimi checker
// added nothing to a fix. On open-ended work the same cheap crew merged NONE
// of eight tasks, and giving it a strong checker — and only a checker — merged
// all eight. Routing fixes to the cheap crew and open-ended work to the cheap
// crew with a strong checker scored the same as always paying for the strong
// checker, 20 of 22 mergeable, for 40% less. That is the whole policy, read
// off a table ([prior]) rather than written into branches: classify the task
// ([Classify]), then pick each seat to maximise quality minus λ times cost.
//
// ── WHAT IT DOES NOT DO ──
//
// It does not escalate on its own. CodeAF's own done-verdict caught 41% of the
// real solves and passed 83% of the failures on the same tasks, so a loop that
// escalated on it would spend on the wrong tasks; [AutoEscalate] is off, and a
// stronger crew is something a person asks for (`redo stronger`) until a
// checker's agreement with reviewers is measured at about 0.7.
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
	"strings"
)

// AutoEscalate is whether a crew is ever made stronger without somebody
// asking. It is OFF, and a constant rather than a setting, because the number
// that would justify turning it on — how often the checker's verdict agrees
// with a reviewer's, measured at 0.41 recall and 0.83 false-pass on real work —
// is a property of the checker, not a preference. The Pandora-index rule the
// evidence supports (stop when the verdict is trusted, escalate when it is
// not) needs an agreement near 0.7 before its escalations pay for themselves.
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
)

// Route is one way to reach a model: the provider, the id to send so the call
// goes that way, and how it bills.
type Route struct {
	Provider string
	Send     string
	Kind     RouteKind
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
	reading := Reading{Class: r.Class, Why: "given", Sure: true}
	if r.Class == "" {
		reading = Classify(r.Task)
	}
	d := Decision{Class: reading.Class, Why: reading.Why, Sure: reading.Sure, Effort: r.Effort, Steps: r.Steps, Considered: len(r.Candidates)}
	pins := r.Pins
	if r.Stronger != nil && allPinned(pins) {
		// EVERY SEAT PINNED AND A STRONGER RUN ASKED FOR: the pins stay what
		// they are, and this one run steps over them.
		pins, d.OneOff = nil, true
	}
	lambda := baseLambda(t, r)
	d.Lambda = lambda
	crew, err := pickCrew(t, d.Class, r.Candidates, pins, lambda)
	if err != nil {
		return Decision{}, err
	}
	if r.Stronger != nil {
		crew, d.Lambda, err = stronger(t, d.Class, r.Candidates, pins, lambda, r.Stronger)
		if err != nil {
			return Decision{}, err
		}
	}
	d.Crew = crew
	for _, pick := range crew {
		d.EstUSD += pick.CostUSD
		d.Quality += pick.Quality
	}
	return d, nil
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
	steps := r.Steps
	if steps > maxSteps {
		steps = maxSteps
	}
	for i := 0; i < steps; i++ {
		lambda /= stepFactor
	}
	if r.Pace > 0 {
		lambda *= r.Pace
	}
	return lambda
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
		if !seatable(c) {
			continue
		}
		pick := pickOf(t, class, seat, c)
		if pick.Quality <= floor+1e-9 {
			continue
		}
		score := pick.Quality - lambda*pick.CostUSD
		switch {
		case !found, score > bestScore+1e-12:
		case score < bestScore-1e-12:
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

// seatable is whether a candidate can sit a seat at all: it takes tool calls
// and it has a route.
func seatable(c Candidate) bool { return c.Model.Tools && len(c.Routes) > 0 }

// pickOf is one candidate in one seat on its cheapest route. Routes of equal
// cost keep the caller's order, which puts the provider the person connected
// for that vendor ahead of a router.
func pickOf(t *table, class Class, seat Seat, c Candidate) Pick {
	q, measured := t.quality(class, seat, c.Model)
	metered := t.seatCost(seat, c.Model)
	pick := Pick{Seat: seat, Model: c.Model.ID, Quality: q, Measured: measured, CostUSD: math.Inf(1)}
	for _, route := range c.Routes {
		cost := routeCost(route, metered)
		if cost < pick.CostUSD-1e-12 {
			pick.Provider, pick.Send, pick.Kind, pick.CostUSD = route.Provider, route.Send, route.Kind, cost
		}
	}
	return pick
}

// routeCost is what one more task costs on a route: the metered price on a
// metered route, and nothing on a plan or a local model.
func routeCost(route Route, metered float64) float64 {
	if route.Kind == Plan || route.Kind == Local {
		return 0
	}
	return metered
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
					found.CostUSD = routeCost(route, t.seatCost(seat, c.Model))
				}
			}
		}
		if math.IsInf(found.CostUSD, 1) {
			found.CostUSD = 0
		}
		if pin.Send != "" && found.Send == "" {
			found.Send, found.Kind = pin.Send, pin.Kind
		}
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

// stronger is a redo's crew: the λ steps down until the crew is strictly
// stronger than the one that ran, no unpinned seat weaker; and when no λ gets
// there because the cheap picks are already the table's best, the seat whose
// next-stronger model costs least is moved up by one model.
func stronger(t *table, class Class, candidates []Candidate, pins map[Seat]Pin, lambda float64, ran *Decision) ([]Pick, float64, error) {
	before := 0.0
	for _, seat := range Seats {
		if _, ok := pins[seat]; !ok {
			before += ran.Seat(seat).Quality
		}
	}
	for i := 0; i <= maxSteps; i++ {
		lambda /= stepFactor
		if i == maxSteps {
			lambda = 0
		}
		crew, err := pickCrew(t, class, candidates, pins, lambda)
		if err != nil {
			return nil, 0, err
		}
		after, weaker := 0.0, false
		for _, pick := range crew {
			if pick.Pinned {
				continue
			}
			after += pick.Quality
			if pick.Quality < ran.Seat(pick.Seat).Quality-1e-9 {
				weaker = true
			}
		}
		if !weaker && after > before+1e-9 {
			return crew, lambda, nil
		}
	}
	// NO PRICE OF A POINT BUYS MORE: step one seat up by one model. The
	// checker first, because it is the lever the evidence found, then the
	// worker, then the planner.
	crew, err := pickCrew(t, class, candidates, pins, 0)
	if err != nil {
		return nil, 0, err
	}
	for _, seat := range []Seat{Checker, Worker, Planner} {
		if _, ok := pins[seat]; ok {
			continue
		}
		floor := ran.Seat(seat).Quality
		up, ok := nextUp(t, class, seat, candidates, floor)
		if !ok {
			continue
		}
		for i := range crew {
			if crew[i].Seat == seat {
				crew[i] = up
			} else if !crew[i].Pinned && crew[i].Quality < ran.Seat(crew[i].Seat).Quality {
				crew[i] = ran.Seat(crew[i].Seat)
			}
		}
		return crew, 0, nil
	}
	return nil, 0, ErrStrongest
}

// nextUp is the cheapest model whose quality in the seat is above floor.
func nextUp(t *table, class Class, seat Seat, candidates []Candidate, floor float64) (Pick, bool) {
	var best Pick
	found := false
	for _, c := range candidates {
		if !seatable(c) {
			continue
		}
		pick := pickOf(t, class, seat, c)
		if pick.Quality <= floor+1e-9 {
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
		if !seatable(c) {
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
	b.WriteString(string(d.Class))
	worker, checker := d.Seat(Worker), d.Seat(Checker)
	b.WriteString(" · worker ")
	b.WriteString(seatModel(worker, pinMark))
	if worker.Provider != "" {
		b.WriteString(" (" + worker.Provider + ")")
	}
	b.WriteString(" · checker ")
	b.WriteString(seatModel(checker, pinMark))
	switch {
	case actual >= 0:
		b.WriteString(" · " + Money(actual) + " (est " + Money(d.EstUSD) + ")")
	default:
		b.WriteString(" · est " + Money(d.EstUSD))
	}
	return b.String()
}

// seatModel is one seat's model the way the line names it: the name after the
// vendor, with the pin mark in front when the seat was pinned.
func seatModel(pick Pick, pinMark string) string {
	name := ShortModel(pick.Model)
	if pick.Pinned && pinMark != "" {
		return pinMark + " " + name
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
