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

// deadPathFloor is the shortest silence that may be read as a DEAD PATH rather
// than as a slow lane.
//
// It is the one claim the controller cannot make, because it is not about time
// at all: a stream that has carried neither a token nor a heartbeat says
// nothing about how fast the endpoint behind it writes — nothing ever reached
// it, or nothing ever came back — and charging the lane for it would demote a
// machine on the evidence of somebody's wifi. Three seconds is the floor under
// it because a TLS handshake and a cold connection can honestly take longer
// than a fast lane's whole believed wait.
const deadPathFloor = 3 * time.Second

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
	return Watching(PlanFor(choice, belief, RoleTalk, now))
}

// PlanFor turns a routing answer and a belief into a waiting one.
//
// ROUTING AND WAITING ARE TWO QUESTIONS. The choice says WHICH LANE and this
// says WHEN TO ACT, and a choice that expressed no preference at all still
// yields a plan — with a ceiling, with a floor, and with whatever alternatives
// the frontier named. That is the whole of "a cold ledger may not switch the
// clock off".
func PlanFor(choice Choice, belief Belief, role Role, now time.Time) control.Plan {
	head := headOf(choice)
	first, gap := survivals(belief)
	return control.Plan{
		Lane:    head,
		Ceiling: role.Ceiling(),
		Floor:   ActionFloor,
		Lambda:  role.Lambda(),
		Margin:  Hysteresis.Seconds(),
		First:   first,
		Gap:     gap,
		Alts:    alternatives(choice, head),
		Began:   now,
	}
}

// headOf is the lane this request was expected to land on: the pin if there is
// one, else the head of the order.
func headOf(choice Choice) string {
	if len(choice.Only) > 0 {
		return choice.Only[0]
	}
	if len(choice.Order) > 0 {
		return choice.Order[0]
	}
	return ""
}

// survivals is one lane's belief as the two distributions a wait is judged
// against: how long its first word takes, and how long a gap between two words
// may be.
//
// THE SPREAD HAS A FLOOR AND IT IS NOT OPTIONAL. A posterior's variance is the
// variance of the ESTIMATE, which shrinks toward nothing as evidence
// accumulates. What a wait is judged against is how variable ONE DRAW is, and a
// controller handed the estimate's spread would believe a tail impossible and
// would never hedge the lane that has one.
func survivals(belief Belief) (first, gap control.Survival) {
	if belief.TTFT.Known() {
		first = control.Survival{
			Mu:    belief.TTFT.X - math.Log(1000),
			Sigma: predictiveSpread(belief.TTFT.P),
		}
	}
	if belief.Rate.Known() {
		// A gap is one over a rate, so its log is the rate's negated and its
		// spread is the same.
		gap = control.Survival{Mu: -belief.Rate.X, Sigma: predictiveSpread(belief.Rate.P)}
	}
	return first, gap
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
// already scored them at. An empty list is a real state and the reason
// [control.Report] exists.
func alternatives(choice Choice, head string) []control.Alternative {
	numbers := make(map[string]Scored, len(choice.Frontier))
	for _, scored := range choice.Frontier {
		numbers[strings.ToLower(scored.ID.Lane)] = scored
	}
	seen := map[string]bool{strings.ToLower(head): true}
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

// Allows asks the budget for one arm AND COUNTS IT when the answer is yes,
// which is [Budget.Allow]'s contract: two arms decided at one moment must not
// both be allowed on the strength of one allowance.
func (p purse) Allows(usd float64, now time.Time) bool { return p.budget.Allow(now, usd) }

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
	if w.tokens == 0 && !w.alive.After(w.began) && now.Sub(w.began) >= deadPathFloor {
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
	first, gap := survivals(belief)
	w.controller().Serving(lane, first, gap, now)
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
