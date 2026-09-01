package lane

import (
	"math"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── THE WATCH: ONE CONTROLLER, DRIVEN BY ONE STREAM ─────────────────────────
//
// Everything before this point picks a lane before the request goes out. The
// watch is the half that can still act after it has gone, and since
// `docs/design/waiting/DESIGN.md` it decides nothing itself: it is the ADAPTER
// between the shape a stream loop has — tokens, heartbeats, a beat asking
// whether anything has arrived — and [control.Controller], which holds the one
// inequality every phase of every call is judged by.
//
// WHAT THAT REPLACED, AND WHY EACH ONE WENT. This file used to hold four
// absolutes and each of them was a belief wearing an invariant's clothes:
//
//   - a fifteen-second gap that alarmed on its own, which said the same thing
//     about a lane writing six tokens a second and one writing two hundred;
//   - sixty-four tokens of commitment, which was right for a four-hundred token
//     reply and wrong for a four-thousand token one;
//   - an eight-second ceiling on a derived deadline, which existed only when a
//     belief did — so the one case that most needed a clock, a model nobody has
//     measured, was the one case that got none;
//   - one hedge per request, which is a counter where a purse belongs.
//
// They are all one line now: act when the expected remaining wait exceeds what
// acting would cost, and at the role's ceiling whatever the two say. The
// ceiling is the only absolute left, and it is the only one that is about a
// person rather than about a machine.
//
// The watch is still PURE: it holds no clock, no lock and no channel, and every
// method takes the moment as an argument. Whoever drives it — one stream loop
// and one beat, one call at a time — owns the serialization, and
// `internal/provider/hedge.go` is that owner.

// DeadPathFloor is the shortest silence that may be read as a DEAD PATH rather
// than as a slow lane.
//
// It is the one claim the controller cannot make, because it is not about time
// at all: a stream that has carried neither a token nor a heartbeat says
// nothing about how fast the endpoint behind it writes — nothing ever reached
// it, or nothing ever came back — and charging the lane for it would demote a
// machine on the evidence of somebody's wifi. Three seconds is the floor under
// it because a TLS handshake and a cold connection can honestly take longer
// than a fast lane's whole believed wait.
const DeadPathFloor = 3 * time.Second

// Watch follows one stream from the moment it is sent.
type Watch struct {
	// plan is what the controller is built from, and it is held so that
	// [Watch.SetExpectedTokens] can still reach it before the stream starts.
	plan control.Plan
	// asked is the controller, built once and lazily for that reason.
	asked control.Controller
	began time.Time
	// tokens is every delta that has arrived and visible the ones a person can
	// read. They are kept because the transport hands over totals and the
	// controller is fed the difference.
	tokens  int
	visible int
	// alive is the last sign of life of ANY kind — a heartbeat or a delta — and
	// it is the whole of the dead-path claim.
	alive time.Time
	// last is the act that last fired, so a caller can log the numbers it was
	// decided on rather than only the outcome.
	last  control.Act
	fault bool
}

// Watching builds a watch over a plan. It is the door a caller that knows the
// role, the purse and the frontier uses; [NewWatch] is the older one.
func Watching(plan control.Plan) *Watch {
	return &Watch{plan: plan, began: plan.Began, alive: plan.Began}
}

// NewWatch starts a watch over one stream from the choice that sent it.
//
// IT ASSUMES A PERSON IS READING, because a race is a rescue and a rescue is
// the errand it is rescuing. A caller that knows better builds its own [Plan]
// with [PlanFor] and hands it to [Watching]; nothing here may guess a role.
func NewWatch(choice Choice, belief Belief, now time.Time) *Watch {
	return Watching(PlanFor(choice, PaceOf(belief), RoleTalk, now))
}

// Pace is what is believed about the machine expected to serve, as the two
// distributions a wait is judged against: how long its first word takes, and
// how long a gap between two visible ones may be. Both are in SECONDS, and an
// unknown one is a real state that leaves the ceiling as the only bound.
type Pace struct {
	First control.Survival
	Gap   control.Survival
}

// PlanFor turns a routing answer into a waiting one, and it is the ONE place a
// plan is built.
//
// ROUTING AND WAITING ARE TWO QUESTIONS. The choice says WHICH LANE and this
// says WHEN TO ACT, and a choice that expressed no preference at all still
// yields a plan — with a ceiling, with a floor, and with whatever alternatives
// the frontier named. That is the whole of "a cold ledger may not switch the
// clock off".
//
// IT TAKES A [Pace] RATHER THAN A BELIEF, and the difference is where the
// belief came from. A test scripts one with [PaceOf]; the transport asks
// [PaceFor], which prefers the four-level chain — the world's pace plus the
// provider's offset plus the model's — over a flat belief about a pair nobody
// has measured. Both answer the same two questions, so the plan is built once
// and not twice.
func PlanFor(choice Choice, pace Pace, role Role, now time.Time) control.Plan {
	head := HeadOf(choice)
	return control.Plan{
		Lane:    head,
		Ceiling: role.Ceiling(),
		Floor:   ActionFloor,
		Lambda:  role.Lambda(),
		Margin:  Hysteresis.Seconds(),
		First:   pace.First,
		Gap:     pace.Gap,
		Alts:    alternatives(choice, head),
		Began:   now,
	}
}

// PaceFor is what THIS PROCESS believes about one pair right now.
//
// IT ASKS FOR THE BETTER DOOR AND FALLS BACK TO THE PLAINER ONE. A four-level
// chain answers for a pair nobody has measured — which is the whole of cold
// start — and a flat belief answers only for a pair it has seen. The ledger is
// one object that may be both ([Hierarchy] beside [Ledger]), so a build whose
// ledger is only the plainer kind still gets a plan, with a ceiling under it
// either way.
func PaceFor(id ID, now time.Time) Pace {
	if id.Lane == "" || id.Model == "" {
		return Pace{}
	}
	ledger := Default().Ledger()
	if chains, ok := ledger.(Hierarchy); ok {
		pace := Pace{
			First: chains.Wait(id, now).Survival(SpreadFloor, millisecondsInASecond),
			Gap:   reciprocal(chains.Rate(id, now).Survival(SpreadFloor, 1)),
		}
		if pace.First.Known() || pace.Gap.Known() {
			return pace
		}
	}
	belief, ok := ledger.Belief(id)
	if !ok {
		return Pace{}
	}
	return PaceOf(belief)
}

// millisecondsInASecond is how many of the first-token chain's own units make
// the second [control] waits in. It is spelled rather than written as 1000 in
// the middle of a conversion, because a unit error here is a deadline off by
// three orders of magnitude and nothing would look wrong.
const millisecondsInASecond = 1000

// reciprocal turns a belief about tokens a second into one about the seconds
// between two tokens. The log of a reciprocal is the negated log and the spread
// is unchanged, which is the whole conversion.
func reciprocal(rate control.Survival) control.Survival {
	if !rate.Known() {
		return control.Survival{}
	}
	return control.Survival{Mu: -rate.Mu, Sigma: rate.Sigma}
}

// HeadOf is the lane this request was expected to land on: the pin if there is
// one, else the head of the order. It is exported because the transport asks it
// the same question before it can ask what is believed about the answer, and two
// spellings of "which lane did we mean" is how a plan comes to be built against
// one machine and drawn against another.
func HeadOf(choice Choice) string {
	if len(choice.Only) > 0 {
		return choice.Only[0]
	}
	if len(choice.Order) > 0 {
		return choice.Order[0]
	}
	return ""
}

// PaceOf is one lane's FLAT belief as a [Pace].
//
// THE SPREAD HAS A FLOOR AND IT IS NOT OPTIONAL. A posterior's variance is the
// variance of the ESTIMATE, which shrinks toward nothing as evidence
// accumulates. What a wait is judged against is how variable ONE DRAW is, and a
// controller handed the estimate's spread would believe a tail impossible and
// would never hedge the lane that has one.
func PaceOf(belief Belief) Pace {
	var pace Pace
	if belief.TTFT.Known() {
		pace.First = control.Survival{
			Mu:    belief.TTFT.X - math.Log(millisecondsInASecond),
			Sigma: predictiveSpread(belief.TTFT.P),
		}
	}
	if belief.Rate.Known() {
		// A gap is one over a rate, so its log is the rate's negated and its
		// spread is the same.
		pace.Gap = control.Survival{Mu: -belief.Rate.X, Sigma: predictiveSpread(belief.Rate.P)}
	}
	return pace
}

// predictiveSpread is a predictive standard deviation in nats, floored.
func predictiveSpread(variance float64) float64 {
	return math.Max(math.Sqrt(variance), SpreadFloor)
}

// expected is a point estimate of an interval, in seconds, as the distribution
// whose MEAN is exactly that. The frontier scores lanes with medians, and a
// median read as a mean would price every alternative as cheaper than it is.
func expected(seconds float64) control.Survival {
	if seconds <= 0 {
		return control.Survival{}
	}
	return control.Survival{Mu: math.Log(seconds) - SpreadFloor*SpreadFloor/2, Sigma: SpreadFloor}
}

// alternatives is where acting could go, best first, with what the frontier
// already scored them at.
//
// AN EMPTY LIST IS A REAL STATE and the reason [control.Report] exists: a call
// with nowhere better to go still has a ceiling, and what it does there is say
// so. It is what `routing off`, an endpoint that is not a router, and a ledger
// that has heard of one machine all look like from here.
//
// THE LANES THE CHOICE RULED OUT ARE NOT ALTERNATIVES. Ignore names the lanes
// this process is SURE about rather than the ones it is merely unlucky with,
// and a rescue that went to one would be sending somebody's answer to the
// machine the belief just refused.
func alternatives(choice Choice, head string) []control.Alternative {
	numbers := make(map[string]Scored, len(choice.Frontier))
	for _, scored := range choice.Frontier {
		numbers[strings.ToLower(scored.ID.Lane)] = scored
	}
	seen := map[string]bool{strings.ToLower(head): true}
	for _, refused := range choice.Ignore {
		seen[strings.ToLower(strings.TrimSpace(refused))] = true
	}
	alts := make([]control.Alternative, 0, len(choice.Frontier))
	add := func(lane string) {
		key := strings.ToLower(strings.TrimSpace(lane))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		scored := numbers[key]
		alts = append(alts, control.Alternative{
			Lane:  lane,
			First: expected(scored.TTFT / 1000),
			Rate:  scored.Rate,
			Extra: scored.Price,
		})
	}
	// The order is a ranking and the frontier is a set; the ranking wins where
	// there is one, and the rest of the frontier follows it.
	for _, lane := range choice.Order {
		add(lane)
	}
	for _, scored := range choice.Frontier {
		add(scored.ID.Lane)
	}
	return alts
}

// Spending is a budget as the rail the controller asks before it acts.
//
// It is a small adapter and not a method on [Budget] because the direction of
// the dependency matters: the controller may not know what a budget is, and the
// budget may not know what a controller is. A nil budget refuses, which is how
// hedging is switched off and has always been.
func Spending(budget *Budget) control.Purse { return purse{budget} }

type purse struct{ budget *Budget }

// Allows ASKS AND DOES NOT SPEND, which is [Budget.Affordable] and deliberately
// not [Budget.Allow].
//
// The two halves of a rescue are two different moments. The controller asks
// whether an arm is affordable while it is still deciding — a reading, and one
// it may take several times over one silence — and the race takes the allowance
// at the instant the arm really goes out, which is the decision. A controller
// that reserved would leave allowances held by every request that recovered on
// its own, and one that counted here as well would charge the budget twice for
// one arm.
func (p purse) Allows(usd float64, now time.Time) bool { return p.budget.Affordable(now, usd) }

// ── DRIVING IT ──────────────────────────────────────────────────────────────

// controller is the one this watch was built with, from the one factory.
//
// It is built lazily so that everything a caller sets between the constructor
// and the first byte — the expected length, most of all — is in the plan the
// controller sees. A build with nothing installed is a legal build and gets an
// inert watch rather than a panic.
func (w *Watch) controller() control.Controller {
	if w.asked == nil {
		if build := Controller(); build != nil {
			w.asked = build(w.plan)
		} else {
			w.asked = idle{}
		}
	}
	return w.asked
}

// Plan is what this watch is waiting against.
func (w *Watch) Plan() control.Plan { return w.plan }

// SetExpectedTokens says roughly how long this answer is going to be, which is
// the other half of the commitment arithmetic. It has to be said before the
// stream starts, which is where the transport says it.
func (w *Watch) SetExpectedTokens(n int) {
	if n > 0 && w.asked == nil {
		w.plan.Expected = n
	}
}

// Read folds in one moment of the stream and returns the verdict with the
// numbers it was made on. It is the surface everything else here is written in
// terms of.
func (w *Watch) Read(reading control.Reading) control.Act {
	if reading.Visible > 0 || reading.Hidden > 0 || reading.Beat {
		w.sign(reading.At)
	}
	return w.record(w.controller().Note(reading), reading.At)
}

// Quiet says nothing has arrived by now, and asks the same question.
func (w *Watch) Quiet(now time.Time) control.Act {
	return w.record(w.controller().Quiet(now), now)
}

// Last is the act that fired, empty until one has.
func (w *Watch) Last() control.Act { return w.last }

// Phase is which distribution is governing right now.
func (w *Watch) Phase() control.Phase { return w.controller().Phase() }

// record keeps what the caller needs after the fact: the act, and whether it
// was about the PATH rather than about the lane.
func (w *Watch) record(act control.Act, now time.Time) control.Act {
	if act.Kind == control.None {
		return act
	}
	if w.tokens == 0 && !w.alive.After(w.began) && now.Sub(w.began) >= DeadPathFloor {
		w.fault = true
		act.Reason = "no heartbeat"
	}
	w.last = act
	return act
}

// Serving says which lane the stream itself named, with what is believed about
// it, so that everything after the first chunk is judged against the machine
// that is really answering.
func (w *Watch) Serving(lane string, belief Belief, now time.Time) {
	pace := PaceOf(belief)
	w.controller().Serving(lane, pace.First, pace.Gap, now)
}

// Heartbeat records a sign of life that is not a token.
//
// IT IS NOT A QUESTION. A comment line is proof about the PATH and about
// nothing else, so it moves the dead-path claim and nothing in the controller —
// and a caller that has no verdict to honour cannot drop one. The beat is what
// asks; this only answers "somebody is still on the other end".
func (w *Watch) Heartbeat(t time.Time) { w.sign(t) }

// sign records a sign of life. It only ever moves forward: two drivers feed
// this and neither owns the other's clock.
func (w *Watch) sign(t time.Time) {
	if t.After(w.alive) {
		w.alive = t
	}
}

// Token records that n tokens have now arrived — of which visible are tokens a
// person can read — and says whether the stream should be acted on.
//
// THE TWO COUNTS ARE NOT INTERCHANGEABLE. Visible text is progress and starts
// the wait again. A hidden delta is the endpoint writing where nobody can read,
// so it moves the phase and leaves the silence exactly where it was.
func (w *Watch) Token(n, visible int, t time.Time) Verdict {
	reading := control.Reading{At: t, Visible: visible - w.visible, Hidden: (n - w.tokens) - (visible - w.visible)}
	w.tokens, w.visible = n, visible
	return verdictOf(w.Read(reading))
}

// Silence records that nothing has arrived by t, and says whether that silence
// has gone on long enough to act on.
func (w *Watch) Silence(t time.Time) Verdict { return verdictOf(w.Quiet(t)) }

// verdictOf is the older, narrower answer: a hedge or nothing. Everything the
// ladder gained since — an offer, a report, an escalation, a commitment — is
// read off [Watch.Last] by the callers that know what to do with it.
func verdictOf(act control.Act) Verdict {
	if act.Kind != control.Hedge {
		return Verdict{}
	}
	return Verdict{Hedge: true, Reason: act.Reason}
}

// DeadlineAt is the next moment worth waking for, so a beat arms a timer rather
// than polls. It is never in the past.
func (w *Watch) DeadlineAt() time.Time { return w.controller().Deadline() }

// Deadline is that moment as a wait from the request going out, which is what
// the phase clock draws a countdown against.
func (w *Watch) Deadline() time.Duration {
	if wait := w.DeadlineAt().Sub(w.began); wait > 0 {
		return wait
	}
	return 0
}

// Alt is the lane an act would go to, empty when there is nobody worth acting
// on.
func (w *Watch) Alt() string {
	if len(w.plan.Alts) == 0 {
		return ""
	}
	return w.plan.Alts[0].Lane
}

// Hedged reports whether this request has already put a second arm on the wire.
//
// IT IS NO LONGER A BOOLEAN THAT REFUSES THE NEXT ONE. A request may earn more
// than one arm and what bounds them is the purse; this only says whether one
// has gone out.
func (w *Watch) Hedged() bool { return w.controller().Acted(control.Hedge) }

// Asked reports whether an offer has been raised on this request. A pin is
// asked, never overridden, and it is asked once.
func (w *Watch) Asked() bool { return w.controller().Acted(control.Ask) }

// PathFault reports whether the act this watch raised was about the path rather
// than about the lane, which is the flag that keeps a belief honest.
func (w *Watch) PathFault() bool { return w.fault }

// idle is the controller a build with none installed gets: it watches, it says
// nothing, and it never invents a moment.
type idle struct{}

func (idle) Note(control.Reading) control.Act                              { return control.Act{} }
func (idle) Quiet(time.Time) control.Act                                   { return control.Act{} }
func (idle) Serving(string, control.Survival, control.Survival, time.Time) {}
func (idle) Deadline() time.Time                                           { return time.Time{} }
func (idle) Phase() control.Phase                                          { return control.PhaseSilent }
func (idle) Acted(control.Kind) bool                                       { return false }
