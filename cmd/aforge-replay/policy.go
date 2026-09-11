package main

import (
	"math"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE FOUR CANDIDATES ─────────────────────────────────────────────────────
//
// A policy is a thing that is TAUGHT in time order and then ASKED. That is the
// whole interface, and it is the whole interface on purpose: a candidate that
// needed a sixth method would be a candidate this instrument could not compare
// with the one that is shipped, which is the only comparison anybody wants.
//
// The three teaching methods are internal/lane's own three — a timed answer, an
// answer's usability, a published row — so the chooser that IS shipped is a
// candidate here with no adapter at all, and a candidate written next week is
// taught exactly what the live ledger is taught and not a fact more.

// Policy is one candidate chooser, judged offline.
type Policy interface {
	// Name is what it is called in the table.
	Name() string
	// Saw folds in one timed answer, as the ledger's [lane.Ledger.Note] does,
	// and says which class of call measured it.
	//
	// THE CLASS IS OFFERED TO EVERY CANDIDATE AND THE SHIPPED ONE THROWS IT
	// AWAY, which is itself one of the questions being asked here:
	// internal/lane's ledger keys a belief on (model, machine) and on nothing
	// else, so a first token measured behind a two-hundred-message turn and one
	// measured behind a two-message errand are folded into the same posterior.
	Saw(lane.Sighting, string)
	// Judged folds in whether an answer could be used, or whether the machine
	// answered at all ([lane.Ledger.NoteOutcome]).
	Judged(lane.Outcome)
	// Published folds in one row of the public sheet at a weight of 1/k, as
	// [lane.Ledger.Prime] does.
	Published(lane.Row, float64)
	// Demand names the machine this policy would have asked for, and is empty
	// for a policy with no opinion — which is a real answer and the right one
	// when nothing has been measured.
	Demand(asked) string
}

// ── 1. WHAT THE ROUTER DID ──────────────────────────────────────────────────

// servedPolicy is the baseline and learns nothing, because the thing it stands
// for did not learn either: it is the machine that answered, whoever asked for
// what. Its regret is the bill this build actually paid.
type servedPolicy struct{}

func (servedPolicy) Name() string                { return "served" }
func (servedPolicy) Saw(lane.Sighting, string)   {}
func (servedPolicy) Judged(lane.Outcome)         {}
func (servedPolicy) Published(lane.Row, float64) {}
func (servedPolicy) Demand(one asked) string     { return one.machine }

// ── 2 AND 4. THE CHOOSER AS IT IS ON dev, WITH AND WITHOUT A FLOOR ──────────

// devChooser is internal/lane's own chooser fed the log.
//
// IT IS THE REAL ONE AND NOT A COPY. The registry is the only door a package
// outside internal/lane has to the concrete ledger and chooser, so a pass
// installs a fresh pair through it and asks them the same question the transport
// asks: [lane.Chooser.Choose], with the request's own moment. A reimplementation
// here would be a second chooser to keep in step with the first, and the first
// time they drifted this instrument would be measuring the wrong one.
//
// ONE REGISTRY MEANS ONE PASS AT A TIME. [lane.Default] is the process's own set
// and there is no exported way to build a second, so each candidate is
// constructed immediately before its own walk of the log and the walks are
// sequential. That is not a limitation worth an export: the log is replayed
// four times in a few seconds either way.
type devChooser struct {
	name   string
	ledger lane.Ledger
	pick   lane.Chooser
}

// newDevChooser installs a fresh belief and returns the shipped chooser reading
// it. floor is the smallest variance a belief of this candidate's is allowed to
// hold; zero is the chooser exactly as it is on dev.
func newDevChooser(name string, floor float64) *devChooser {
	registry := lane.Default()
	registry.Reset()
	// NOTHING OF THIS MACHINE'S OWN AFTERNOON MAY LEAK IN. A replay that started
	// from the belief file, or that let the account exclusions struck off a
	// month ago take machines out of the candidate set, would be scoring the
	// log against a world the log never saw.
	registry.SetStore(nowhere{})
	registry.SetLedger(nil)
	lane.ForgetAccountExclusionsInMemory()
	beliefs := registry.Ledger()
	if floor > 0 {
		beliefs = floored{Ledger: beliefs, Hierarchy: beliefs.(lane.Hierarchy), floor: floor}
		registry.SetLedger(beliefs)
	}
	return &devChooser{name: name, ledger: beliefs, pick: registry.Chooser()}
}

func (d *devChooser) Name() string                      { return d.name }
func (d *devChooser) Saw(seen lane.Sighting, _ string)  { d.ledger.Note(seen) }
func (d *devChooser) Judged(outcome lane.Outcome)       { d.ledger.NoteOutcome(outcome) }
func (d *devChooser) Published(row lane.Row, k float64) { d.ledger.Prime(row, k) }

// Demand asks the shipped chooser and reads its answer the way the transport
// reads it: a demand first, then the head of the preference, then nothing.
func (d *devChooser) Demand(one asked) string {
	choice := d.pick.Choose(one.request())
	if len(choice.Only) > 0 {
		return choice.Only[0]
	}
	if len(choice.Order) > 0 {
		return choice.Order[0]
	}
	return ""
}

// nowhere is a belief file that is not there. A replay must never read or write
// the person's own.
type nowhere struct{}

func (nowhere) Load() ([]lane.Belief, error) { return nil, nil }
func (nowhere) Save([]lane.Belief) error     { return nil }

// floored is the shipped ledger with a FLOOR UNDER HOW CERTAIN IT IS ALLOWED TO
// GET, which is what process noise on a Kalman filter buys.
//
// THE FAULT IT IS AIMED AT. [lane.Posterior] holds the variance of its own
// estimate, and that variance shrinks with every observation — after a few dozen
// answers from one machine the filter is very sure where that machine's median
// is. The world is not that stable: a machine that has written at ninety tokens
// a second all afternoon can collapse to two, and a filter with no process noise
// needs many surprising answers to be talked out of a median it has spent an
// hour becoming certain of. Process noise is the statement that the SUBJECT
// moves between observations as well as the measurement being noisy, and the
// smallest honest form of it is a floor: however much has been seen, never claim
// to know this machine's median better than the machine's own day-to-day
// movement (halflife.go's measured figure).
//
// IT IS A WRAPPER AND NOT A PATCH TO internal/lane, deliberately. The floor is a
// question this instrument is asking, not an answer it is shipping; the wrapper
// lets the question be asked of the real chooser with nothing in the product
// changed, and the report says what it would take to ship the answer.
//
// It embeds both seams the chooser reads a ledger through — [lane.Ledger] and
// [lane.Hierarchy] — because choose.go asks for the second by type assertion,
// and a wrapper that satisfied only the first would quietly take the change
// points away from the very candidate that is about to be judged on them.
type floored struct {
	lane.Ledger
	lane.Hierarchy
	floor float64
}

func (f floored) Belief(id lane.ID) (lane.Belief, bool) {
	belief, known := f.Ledger.Belief(id)
	return f.widen(belief), known
}

func (f floored) Beliefs(model string) []lane.Belief {
	held := f.Ledger.Beliefs(model)
	for index := range held {
		held[index] = f.widen(held[index])
	}
	return held
}

// widen raises a belief's two timing variances to the floor, and touches
// nothing else. A posterior that believes NOTHING is left alone: P at zero is
// this package's signal for "no belief here" ([lane.Posterior.Known]), and
// floorng it would turn every unseen machine into a confidently-held opinion.
func (f floored) widen(belief lane.Belief) lane.Belief {
	belief.TTFT = raise(belief.TTFT, f.floor)
	belief.Rate = raise(belief.Rate, f.floor)
	return belief
}

func raise(held lane.Posterior, floor float64) lane.Posterior {
	if held.Known() && held.P < floor {
		held.P = floor
	}
	return held
}

// ── 3. A DECAYED QUANTILE OF WHAT EACH MACHINE DID ──────────────────────────

// quantilePolicy keeps, per (model, machine, role class), an exponentially
// decayed picture of how long that machine made a request of that kind wait, and
// demands the machine whose picture is best at the role's own risk quantile.
//
// WHY IT IS A CANDIDATE AT ALL. The shipped chooser holds a MEAN and a variance
// of a mean; this holds the DISTRIBUTION. The difference is the whole of what a
// tail policy is: a machine that answers in a second half the time and in a
// minute the other half has a median nothing is unsure about, and a person who
// waited the minute did not experience the second. internal/lane already knows
// this and carries [lane.Belief.Spread] beside its posterior for it; the
// question this candidate asks is whether keeping the shape outright, and
// reading it where the role actually pays, beats a median widened by a measured
// dispersion.
//
// IT IS KEYED ON THE ROLE CLASS because the two classes ask different questions
// of the same machine — a watched turn carries a long prompt and a long answer
// and an unattended errand carries neither — and a first-token wait measured
// under one is not evidence about the other.
type quantilePolicy struct {
	// halfLife is how long a measurement keeps half its weight, MEASURED FROM
	// THE LOG rather than chosen (halflife.go). Everything in this candidate
	// that is not the log itself is that one number.
	halfLife time.Duration
	ttft     map[sketchKey]*sketch
	rate     map[sketchKey]*sketch
	// answers and asks are the availability axis, decayed on the same half-life:
	// a machine that refuses four requests in five is asked five times for one
	// answer.
	answers map[sketchKey]*fading
	asks    map[sketchKey]*fading
	// seen is every machine this policy has heard of for a model, which is the
	// set it may demand from. A policy cannot demand a machine nothing has ever
	// mentioned; neither can the shipped one.
	seen map[string]map[string]bool
}

// sketchKey is the subject one picture is about.
type sketchKey struct {
	lane.ID
	class string
}

func newQuantilePolicy(halfLife time.Duration) *quantilePolicy {
	return &quantilePolicy{
		halfLife: halfLife,
		ttft:     map[sketchKey]*sketch{},
		rate:     map[sketchKey]*sketch{},
		answers:  map[sketchKey]*fading{},
		asks:     map[sketchKey]*fading{},
		seen:     map[string]map[string]bool{},
	}
}

func (q *quantilePolicy) Name() string { return "quantile" }

// Published teaches nothing. A SHEET ROW IS A MEDIAN AND THIS CANDIDATE IS ABOUT
// SHAPE: the public sheet's four percentiles are an aggregate over everybody's
// prompts from everybody's region over half an hour, and folding them in as
// though they were draws from our own path would give every machine the same
// invented shape. What it costs is a cold start, and the report says how many
// requests that leaves unanswerable.
func (q *quantilePolicy) Published(row lane.Row, _ float64) { q.note(row.ID) }

func (q *quantilePolicy) Saw(seen lane.Sighting, class string) {
	q.note(seen.ID)
	key := sketchKey{ID: seen.ID, class: class}
	q.fade(q.asks, key).add(seen.At, 1, q.halfLife)
	q.fade(q.answers, key).add(seen.At, 1, q.halfLife)
	if seen.TTFT > 0 {
		q.pick(q.ttft, key).add(seen.At, float64(seen.TTFT.Milliseconds()), q.halfLife)
	}
	// A PROBE RATES THE HANDSHAKE AND NOT THE MACHINE. One token back says
	// nothing about how fast anything writes, which is the same rule
	// [lane.Sighting.Probe] states and [lane.Sighting.Rate] enforces by
	// answering zero for an answer too short to rate.
	if rate := seen.Rate(); rate > 0 && !seen.Probe {
		q.pick(q.rate, key).add(seen.At, rate, q.halfLife)
	}
}

func (q *quantilePolicy) Judged(outcome lane.Outcome) {
	q.note(outcome.ID)
	if !outcome.Refused {
		return
	}
	for _, class := range classes {
		q.fade(q.asks, sketchKey{ID: outcome.ID, class: class}).add(outcome.At, 1, q.halfLife)
	}
}

// Demand is the machine whose expected time to a usable answer is least, read
// at the role's own risk quantile — which is the same sentence
// [rolePatience.expected] and [beyondThePatience] are both written from, applied
// to a measured shape instead of to a posterior.
func (q *quantilePolicy) Demand(one asked) string {
	best, cost := "", math.Inf(1)
	for machine := range q.seen[one.model] {
		key := sketchKey{ID: lane.ID{Model: one.model, Lane: machine}, class: one.class()}
		waits, rates := q.ttft[key], q.rate[key]
		if waits == nil || rates == nil {
			continue
		}
		ttft := waits.at(one.at, riskQuantile(one.role.Visible()), q.halfLife)
		rate := rates.at(one.at, 0.5, q.halfLife)
		if !(ttft > 0) || !(rate > 0) {
			continue
		}
		felt := lane.PerceivedSeconds(ttft/1000, rate, one.want.visible, one.want.hidden)
		asks := q.fade(q.asks, key).left(one.at, q.halfLife)
		answers := q.fade(q.answers, key).left(one.at, q.halfLife)
		if asks > 0 && answers > 0 {
			felt /= math.Max(answers/asks, 1/asks)
		}
		if felt < cost {
			best, cost = machine, felt
		}
	}
	return best
}

// classes is the two-way split a class-keyed picture is kept under. It is
// derived from the role table rather than written twice: see [asked.class].
var classes = []string{"watched", "unattended"}

func (q *quantilePolicy) note(id lane.ID) {
	if q.seen[id.Model] == nil {
		q.seen[id.Model] = map[string]bool{}
	}
	q.seen[id.Model][id.Lane] = true
}

func (q *quantilePolicy) pick(from map[sketchKey]*sketch, key sketchKey) *sketch {
	if held, made := from[key]; made {
		return held
	}
	made := newSketch()
	from[key] = made
	return made
}

func (q *quantilePolicy) fade(from map[sketchKey]*fading, key sketchKey) *fading {
	if held, made := from[key]; made {
		return held
	}
	made := &fading{}
	from[key] = made
	return made
}
