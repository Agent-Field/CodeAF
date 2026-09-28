package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/calllog"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/control"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// waitContext is in media.go: one timer, stopped on the way out, so a retry
// loop that returns early does not leave a timer behind for the length of the
// backoff it abandoned.

// ── THE DISPATCHER: ONE LOOP, ONE BUDGET, ONE OWNER ─────────────────────────
//
// This is the one place in this build that decides what a failed call does
// next, and the one place a request for a completion reaches the wire
// (`TestOneDispatcherSendsForACompletion`).
//
// WHAT IT REPLACED (docs/design/recovery/DESIGN.md §2 problem 1 and §4). Eleven
// controllers on one request path each owned a budget none of the others could
// see — three transport faults here, six or sixty paced sends here, two minutes
// or ten of pacing here, eight free moves here, four arms in hedge.go, seven
// rungs in endpoints.go, four attempts and two models in internal/session. Their
// product is nobody's number, and the call census of 2026-09-10 measured what it
// produced: chains of sixteen and seventeen identical sends to one machine,
// running eleven minutes, and ending refused anyway.
//
// THERE IS NOW ONE BOUND AND IT IS A DEADLINE IN THE PERSON'S OWN TIME
// ([control.Plan.Deadline], from `lane.Role.GiveUp` — ninety seconds for a
// conversation's turn, four and a half minutes for a task node). What comes next
// is [control.Next], a pure function whose order is the rule's order: another
// machine, the same machine once after the comeback it named itself when it is
// alone, a relaxed shape one rung at a time, none. Nothing under the plan owns a
// retry count, and none of the six constants this block used to hold exists.
const (
	baseBackoff  = 700 * time.Millisecond
	maxErrorPeek = 8 << 10
	// maxBackoffShift caps the exponent, not the patience. A patient call may
	// take its hundredth attempt, and `1 << 99` is not a long wait — it is an
	// overflow, and an overflowed duration is a negative one. Seven doublings
	// already put the computed delay past maxProviderWait, which is where every
	// wait lands anyway, so clamping here changes nothing about a bounded call
	// and makes an unbounded one arithmetic rather than undefined.
	maxBackoffShift = 8
	// maxProviderWait caps what a Retry-After may ask for. The header may be an
	// HTTP date, and a provider that names tomorrow morning is asking a call to
	// sleep for hours inside a run the user is watching. A minute is already
	// far past the point where waiting beats failing and saying so.
	maxProviderWait = time.Minute
)

// ── THE BODY IS WRITTEN PER ATTEMPT, NOT PER CALL ───────────────────────────
//
// THE MEASURED FAILURE (the census, docs/design/recovery/census-20260910.md §6).
// This loop used to be handed one encoded body above it and send those same
// bytes again on every attempt. The bytes carry `provider.order`, `provider.only`
// and `provider.ignore`, so "again" meant "to the same machine": 53 % of the
// multi-attempt chains in ten days never left the lane they started on, three of
// them spent sixteen and seventeen consecutive sends on one machine over eleven
// minutes, and every one of those ended 429 anyway. The chains that DID escape
// escaped on the one attempt that finally moved.
//
// THE LAW: A MACHINE THAT REFUSED THIS CALL IS NOT ASKED AGAIN WHILE ANOTHER ONE
// IS ADMISSIBLE. Each attempt re-encodes ([Client.encodeRequest]), and the
// encoder reads this set through [Client.dropRefusedHere], so the exclusion
// travels the same plumbing every other routing preference does rather than as a
// second opinion beside it. The same machine is re-asked only when the serving
// set under this request is one machine wide — a person's strict pin, a rescue's
// demand, an account-wide pacing that names nobody — and then only after the
// wait it asked for itself.

// refusedHere is every machine that has refused THIS CALL, and it is the whole
// of what makes the next attempt a different request rather than the same one.
//
// IT IS THE CALL'S OWN MEMORY AND NOT THE LEDGER'S. The ledger
// ([velocityLedger.pace]) already holds what the process believes about a
// machine, on its own cooldown, shared by every session in the process; this is
// narrower and shorter-lived — one call's list of who has already said no to it
// — and it is what keeps a machine out of the very next body even when the
// ledger declined to write anything at all (an upstream that named no wait, a
// stream that died before naming its server, a 4xx over a list).
//
// It is carried on [callKnobs] by pointer because the knobs travel by value
// through the repair, relax and ladder chains, and what must not be copied is
// the answer to "who has already refused this call".
type refusedHere struct {
	mu    sync.Mutex
	names []string
}

// add records one machine and reports whether it was new. An empty name is a
// refusal that implicated nobody and records nothing — the attribution law the
// ledger keeps, kept here for the same reason.
func (r *refusedHere) add(name string) bool {
	name = strings.TrimSpace(name)
	if r == nil || name == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, held := range r.names {
		if equalLane(held, name) {
			return false
		}
	}
	r.names = append(r.names, name)
	return true
}

// count is how many machines have refused this call.
func (r *refusedHere) count() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.names)
}

// list is a copy of what has refused so far, empty when nothing has.
func (r *refusedHere) list() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.names) == 0 {
		return nil
	}
	return append([]string(nil), r.names...)
}

// ── THE CALL'S ONE PLAN ─────────────────────────────────────────────────────

type callPlanKey struct{}

// dispatchNow is the clock the DEADLINE is read against, and it is deliberately
// the waiting controller's rather than [Client.clock].
//
// A deadline is an account of how long a PERSON has been waiting, which is the
// same argument [waitNow] already makes about the hazard: [Client.clock] is a
// measurement seam that a test fills with a scripted list of instants, and a
// deadline read off one would be this build deciding the shape of a wait nobody
// was having. It is a var rather than a func so a scenario can state ninety
// seconds without spending ninety of them; nothing in production replaces it.
var dispatchNow = waitNow

// withCallPlan puts one question's plan on the context so every send under it
// reads the SAME budget — the same deadline, and the same list of what has been
// tried. A race stamps it once and every arm inherits it, which is what makes
// two arms unable to demand one machine.
func withCallPlan(ctx context.Context, plan control.Plan) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, callPlanKey{}, plan)
}

// callPlanFrom is the plan this call is being made under, and whether anybody
// built one.
func callPlanFrom(ctx context.Context) (control.Plan, bool) {
	if ctx == nil {
		return control.Plan{}, false
	}
	plan, ok := ctx.Value(callPlanKey{}).(control.Plan)
	return plan, ok
}

// dispatchPlan is the budget THIS send runs under.
//
// It is the question's plan where there is one — a race built it, every arm
// shares its deadline and its moves — narrowed to what this particular request
// may do: the machines it may go to ([requestSet], which honours a demand), the
// relaxation rungs it actually has, and the model on it.
//
// AND A CALL NOBODY BUILT A PLAN FOR STILL GETS ONE. A non-streamed completion,
// a document parse, a build with no controller at all: each is a question of its
// own, and a question with no deadline is the state this whole wave exists to
// delete. It is built through `lane.PlanFor`, which is the ONE place a plan is
// built, so the deadline is the role's there as it is everywhere.
func (c *Client) dispatchPlan(ctx context.Context, request *ai.Request, knobs callKnobs) control.Plan {
	model := c.modelFor(request)
	plan, held := callPlanFrom(ctx)
	if !held {
		plan = lanes.PlanFor(lanes.Choice{}, lanes.Pace{}, RoleFrom(ctx), dispatchNow())
	}
	plan.Model = model
	// THE SET IS THE REQUEST'S OWN AND NOT THE CHOICE'S. A rescue's demand and a
	// person's strict pin are both one machine wide however many the frontier
	// named, and that is the whole of what makes the single legal repeat legal.
	// An empty answer is an OPEN set — the router picks — which is a different
	// state from a set of one and is read as one by [control.Next].
	if set := requestSet(knobs); len(set) > 0 {
		plan.Lane, plan.Alts = strings.TrimSpace(set[0]), nil
		for _, lane := range set[1:] {
			plan.Alts = append(plan.Alts, control.Alternative{Lane: strings.TrimSpace(lane)})
		}
	} else {
		plan.Lane, plan.Alts = "", nil
	}
	// AND THE RUNGS ARE THIS REQUEST'S OWN. A rung for a field the request never
	// carried is not a move, it is the same request sent twice — so a text-only
	// call with no cap and no knob has no ladder at all, and [control.Next] must
	// not offer it one (endpoints.go's [Client.relaxationPlan] is the authority
	// and this reads it rather than counting rungs a second time).
	plan.Shapes = nil
	for _, step := range c.relaxationPlan(request, knobs, model) {
		plan.Shapes = append(plan.Shapes, step.label)
	}
	if plan.Moves == nil {
		plan.Moves = control.NewMoveLog()
	}
	if plan.Deadline.IsZero() {
		plan.Deadline = dispatchNow().Add(RoleFrom(ctx).GiveUp())
	}
	return plan
}

// send performs one request with retries, and returns a response whose body has
// not been read.
//
// The request is rebuilt on each attempt rather than reused: its body is a
// reader, and a retried request carrying a drained reader would silently post an
// empty document. AND ITS BYTES ARE REBUILT WITH IT — see THE BODY IS WRITTEN
// PER ATTEMPT above.
func (c *Client) send(ctx context.Context, request *ai.Request, knobs callKnobs, body []byte, stream bool) (*http.Response, error) {
	// ANYTHING A PERSON IS STILL OWED IS SAID HERE, ON THE VERY NEXT BODY THAT
	// GOES OUT ON A STREAM THEY ARE READING (lanepin.go's [tellRetiredPins],
	// prefcarry.go's [tellUncarriedPins], routefirst.go's [tellTakeover]). Each
	// is one nil check on a call nobody is reading and on every call after its
	// sentence has been said.
	//
	// IT IS HERE AND NOT AT THE SHAPED DOOR ABOVE IT, and the move is the fix.
	// [Client.sendShaped] is entered once per call, so a fact the call learned
	// FROM ITS OWN REFUSAL had already gone past the only place that says it:
	// the widened retry a retired pin earns goes out through
	// [Client.attemptShaped], and every rung of the relaxation ladder likewise,
	// and none of them come back through the shaped door. Measured on
	// 2026-09-13: a turn demanded a machine the account excludes, collected the
	// router's refusal, retired the person's pin, and answered from another
	// machine — and the sentence saying so was queued behind a door the rest of
	// that turn never opened again. This is the door every BODY passes through,
	// which is the grain the promise was written at.
	tellRetiredPins(ctx)
	tellUncarriedPins(ctx)
	tellTakeover(ctx)
	if gate := c.config.RouteGate; gate != nil {
		if err := gate(ctx, c.modelFor(request), callTag(ctx)); err != nil {
			return nil, err
		}
	}
	var lastErr error
	// The wait is sized for the reply the request PERMITS — the caller's answer
	// plus the thinking pass's room — and not for the caller's figure alone.
	// Sized from the figure, an always-thinking model was allowed to generate
	// for longer than the transport would wait, and the empty-answer failure
	// came back as a timeout.
	ceiling, _ := c.ceilingFor(request, knobs)
	httpClient := c.clientFor(c.modelFor(request), stream, ceiling)
	// The address is part of the bound billing door. It changes only for the
	// one separately authorised plan overflow below; every ordinary retry and
	// endpoint move stays on the address the service was connected to.
	currentBase := c.config.BaseURL
	currentDoor := c.config.BillingDoor
	usingOverflow := false
	switchedDoor := false
	// providerWait is the provider's own comeback instruction from the last
	// 429 (Retry-After); it outranks our computed backoff for the one attempt
	// it was issued for, and is then spent.
	var providerWait time.Duration
	// Whether this call waits pacing out or gives up on it, and who is told
	// while it waits (patience.go). Both ride the context because the adapter
	// underneath is shared by every agent in the process.
	patient := patientRateLimits(ctx)
	notice := pacingNoticeFrom(ctx)
	// parked says this call has already announced that it is waiting on the
	// provider. It is announced ONCE per call and taken back on EVERY exit —
	// through, or given up — because a surface left holding "still waiting"
	// for a call that has landed is worse than one that was never told.
	parked := false
	// park is THE ONE SITE that says whether this call is waiting a provider's
	// pacing out, and it says it to both readers of that fact at once: the older
	// bool above, and the phase on the seam that carries everything else about
	// this call's life (callprogress.go's [CallPaced]). One site, so the two
	// cannot disagree — which is the only reason the older spelling is still
	// safe to leave standing while its one caller is moved over.
	park := func(on bool) {
		if parked == on {
			return
		}
		parked = on
		if notice != nil {
			notice(on)
		}
		streamWatchFrom(ctx).callPaced(on, dispatchNow())
	}
	defer func() { park(false) }()
	// pacedSince is when this call FIRST drew a 429, and zero until it does. It
	// is what tells [PhasePaced] from [PhaseRetrying] — whose fault the wait is —
	// and nothing else: the budget it used to be measured against is gone, and
	// the deadline it is measured against now belongs to the plan.
	var pacedSince time.Time
	attempts := 0
	var recoveryCtx context.Context
	reconnected := false
	// connectionPasses chooses the backoff after a reachability check answered
	// but the send still failed. It is not a budget: [control.Plan.Deadline] is
	// the one thing that ends this call, exactly as it is for every other fault.
	connectionPasses := 0
	// moved says the last attempt took a machine OFF the next body, so the next
	// send is a different request to a different machine and owes nobody a wait.
	// See the branch it governs below: a backoff is what we pay to ask the same
	// machine again, and it is the only thing it is for.
	moved := false
	// accountPaced says the last refusal was the ACCOUNT'S OWN CEILING: this base
	// has a pool behind the model, it asked the whole key to slow down, and it
	// named none of that pool while doing it. There is nothing to put on the next
	// body and every machine is behind the same ceiling, which is the move
	// generator's ([control.Plan.AccountRefused]).
	accountPaced := false
	// sentVeto is EVERY MACHINE THAT HAD ALREADY REFUSED THIS CALL when the body
	// now in flight was encoded — the exact list [Client.dropRefusedHere] read to
	// compose that body.
	//
	// IT IS NOT ALWAYS THE `ignore` LIST THE BYTES CARRY, and the difference is
	// deliberate rather than an approximation. A demanded name is never also
	// vetoed, and [velocityLedger.keepTheSetServable] releases one refused name
	// from the veto when the vetoes would otherwise empty the set. In both cases
	// a machine on this list is one the body asked for again ON PURPOSE — not
	// because it might have been forgiven, but because this process had nowhere
	// else to send the request. So a refusal from it means the same thing the
	// failed veto means, which is why the law below reads this list rather than
	// the object: there is no other endpoint to reach.
	//
	// IT IS SNAPSHOTTED AT THE ENCODE AND NEVER READ BACK AT THE REFUSAL. The
	// list is shared by every arm of one question ([refusedHere]), so a sibling
	// arm adding a name while this body was on the wire would make it look as
	// though this body had asked for a machine it never named.
	//
	// IT STARTS AT WHAT THE CALLER'S OWN ENCODE SAW, because a recovered send
	// re-enters this loop at attempt 0 with a body the ladder already composed
	// against these same names ([Client.sendRecovered]); starting empty cost the
	// law one extra send to the same machine on every rung.
	var sentVeto []string
	if knobs.refused != nil {
		sentVeto = knobs.refused.list()
	}
	// move is what the plan says to do next, and it is empty on the first pass
	// because the first send is not a recovery from anything.
	var move control.Move
	// The body this call is carrying, kept ONLY when somebody asked for it: the
	// old bodies pin, which puts it on the line of the model-call log, or the
	// debug record, which is where bodies are moving to (calllog.go). On every
	// ordinary run this is nil and the person's prompts never leave the process.
	if knobs.trace != nil && (calllog.Bodies() || trace.For(ctx) != nil) {
		knobs.trace.body = body
	}
	// ── THE ONE BUDGET ──────────────────────────────────────────────────────
	//
	// Rate limits are not faults — they are the provider pacing us, and
	// abandoning work over pacing is the one outcome the concurrency doctrine
	// forbids — but that is now a statement about WHICH MOVE comes next and no
	// longer about how many attempts of a private count are left. The plan is
	// the whole of how long, [control.Next] the whole of what, and neither of
	// them is a number this file holds.
	plan := c.dispatchPlan(ctx, request, knobs)
	// ── HOW MANY MACHINES THERE ARE, AS HONESTLY AS THIS PROCESS CAN SAY ────
	//
	// It used to be the attempt ceiling — `2 of 6`, where six was a statement
	// about patience and not about anything a person could count — so a walk of
	// three machines read as though half of something was left over.
	//
	// THE SET IS THE ANSWER WHERE THERE IS ONE: a demand, or the candidates the
	// chooser drew. Where there is none the pool belongs to the router and its
	// width is genuinely unknown to us, so the honest denominator is what this
	// CALL has met — the machines that have refused it, plus the one it is about
	// to ask. It grows as the walk discovers the pool, which is the truth: "the
	// third of the three I know about" is a real thing to say, and `3 of 6` is
	// not.
	width := func() int {
		if named := len(plan.Serving()); named > 0 {
			return named
		}
		return knobs.refused.count() + 1
	}
	// ── A WAIT WE ASKED FOR IS SPENT WHETHER OR NOT THE CLOCK MOVED ─────────
	//
	// [Client.wait] is a seam, and what a test usually fills it with is a
	// function that returns at once: that is how a scenario states a
	// two-minute backoff without spending two minutes. Under an attempt count
	// that was harmless, because the count was what ended the call. Under a
	// deadline it is not: a seam that makes waiting free makes the deadline
	// unreachable, and the loop it bounds becomes the unbounded one this wave
	// exists to delete.
	//
	// So the dispatcher charges itself for what it ASKED for. `owed` is the
	// part of a wait the clock did not actually take, and it is added to the
	// moment the deadline is read against. In production it is a rounding
	// error; in a scenario it is the whole of the fiction, honestly kept.
	var owed time.Duration
	spentAt := func() time.Time { return dispatchNow().Add(owed) }
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			// AND THE DEADLINE IS ASKED FIRST, because it is the only thing that
			// ends a call. `outOfPatience` used to answer here from two
			// currencies nobody above could see — six attempts or two minutes
			// for a watched call, sixty or ten for a patient one — and it is
			// deleted with them.
			if plan.Spent(spentAt()) {
				break
			}
			// TWO PASSES ALREADY OWE THE MOVE THEY ARE MAKING, and neither may ask
			// the generator for a second one.
			//
			// A billing-door switch is one: it must reach the authorised address
			// before the endpoint controller is asked for another move, or a plan
			// pause could be turned into a lane walk or an early end without ever
			// trying the metered door.
			//
			// A reachability check that answered after a pre-send failure is the
			// other: it names no machine and earns no routing move. The next send
			// stays on this road; after the first recovered pass it pays the
			// ordinary fault backoff below. Everything else asks the one move
			// generator as before.
			if !switchedDoor && !reconnected {
				// What the last refusal asked us to wait, and whether it was the
				// account's own ceiling rather than a machine's: the two inputs the
				// move generator needs that change between moves. A ceiling over the
				// whole key leaves nothing to put on the next body, which is what
				// stops an open set pretending it has somewhere to go
				// ([control.Plan.AccountRefused]).
				plan.Comeback = providerWait
				plan.AccountRefused = accountPaced
				move = control.Next(plan, plan.Moves.List())
				if move.Kind == control.MoveNone {
					break
				}
				// A SHAPE IS NOT THIS LOOP'S MOVE TO MAKE. The ladder that takes a
				// field off the request is endpoints.go's and it is entered from
				// above ([Client.sendRecovered]), because only the layer holding the
				// refusal's own body can say which rung it earned. This loop hands
				// the refusal back and that layer climbs.
				if move.Kind == control.MoveShape {
					break
				}
				plan.Moves.Add(move)
			}
		}
		attempts++
		// What the CALL has spent, for the row the completed answer writes at
		// the end of it (calllog.go). The count lives on the knobs rather than
		// here because a call that is repaired or relaxed comes back through
		// this loop with a new body and the same trace.
		knobs.trace.begin()
		if attempt > 0 && switchedDoor {
			// A BILLING-DOOR SWITCH IS NEITHER A RECONNECTION NOR A RETRY. The
			// person's opt-in already chose the next address, so it goes out at once
			// and the phase clock's door is the only status change owed here. It is
			// asked BEFORE the recovered-pass branch below for exactly that reason:
			// a door switch that fell through to it would be narrated as recovering
			// from a dropped connection and would sit out a backoff it does not owe.
			providerWait = 0
		} else if attempt > 0 && reconnected && connectionPasses > 1 {
			// THE FIRST RECOVERED PASS IS IMMEDIATE. Every later one means the
			// check answered while the request itself still could not leave, which
			// is another fault and pays the same growing pause as every other one.
			// The wait is charged to the plan even when a test makes waiting free,
			// so a fast-answering portal cannot make the deadline unreachable.
			delay := backoffFor(connectionPasses-1, 0)
			if left := plan.Left(spentAt()); left > 0 && delay > left {
				delay = left
			}
			now := c.clock()
			notePhase(ctx, c.modelFor(request), PhaseRetrying,
				"", now, now.Add(delay), "")
			waitBegan := dispatchNow()
			if err := c.wait(ctx, delay); err != nil {
				return nil, err
			}
			if took := dispatchNow().Sub(waitBegan); took < delay {
				owed += delay - took
			}
			if plan.Spent(spentAt()) {
				break
			}
		} else if attempt > 0 && !reconnected && moved {
			// A MOVE IS NOT A WAIT. The last attempt excluded the machine that
			// refused it, so the body about to go out is a different request to a
			// different machine — and sitting out a backoff first would be this
			// call serving a sentence another machine handed down. A backoff is
			// for asking the SAME machine again, which is the next branch.
			//
			// The person is told all the same, in the ordinal, with no moment
			// attached: there is nothing to count down to, because nothing is
			// being waited for.
			//
			// AND THE ORDINAL'S DENOMINATOR IS REAL. It used to be the attempt
			// ceiling — `2 of 6` where six was a constant about patience — so a
			// walk of three machines read as though half of something was left.
			// It is how many machines this request may actually go to
			// ([control.Plan.Serving]), and a set nobody named draws nothing
			// rather than a number it cannot stand behind.
			now := c.clock()
			notePhase(ctx, c.modelFor(request), PhaseRetrying,
				ordinalOf(attempts, width()), now, now, "")
			providerWait = 0
		} else if attempt > 0 && !reconnected {
			// AND THE WAIT THE MACHINE ITSELF NAMED IS THE WHOLE WAIT, not a
			// floor under our doubling. A [control.MoveWait] is the one legal
			// repeat there is — the same bytes to the same machine, because the
			// set is one wide or the ceiling is over the whole key — and the
			// comeback on it is a fact the answer carried, not a guess. Passing
			// it through [backoffFor] made it the LARGER of what was asked for
			// and 2^(attempt-1) rungs of a doubling that exists for the case
			// where nobody asked for anything: a comeback of three seconds met
			// on the fourth attempt was served as eleven, and a person who had
			// been told `back in 3s` watched a longer clock.
			//
			// The cap is still the cap: [maxProviderWait] bounds one wait
			// whatever asked for it, which is what keeps an interrupt prompt.
			// A CALL THAT HANDS A LIMIT BACK DOES NOT SIT IT OUT (patience.go's
			// [WithoutPatientRateLimits]): its caller's next move is another
			// route or another model, which beats any window.
			if handsBackRateLimits(ctx, lastErr) {
				return nil, lastErr
			}
			delay := backoffFor(attempt, providerWait)
			if move.Kind == control.MoveWait && move.Wait > 0 {
				delay = min(move.Wait, maxProviderWait)
			}
			// AND IT IS NEVER LONGER THAN WHAT IS LEFT OF THE CALL. A wait that
			// outlives the deadline is a person watching a countdown to a moment
			// this build has already decided not to reach.
			if left := plan.Left(spentAt()); left > 0 && delay > left {
				delay = left
			}
			// AND A PERSON IS TOLD HOW LONG, WHICH IS THE ONE FACT THIS LOOP
			// HAD AND THREW AWAY. Until the phase clock, a conversation parked
			// on a rate limit for two minutes said nothing at all: the only
			// seam out of here was [WithPacingNotice], a bare bool, and it is
			// nil on every chat session (internal/session's agent.go — only a
			// task node sets one). The deadline here is REAL — it is the moment
			// this request goes out again — which is what makes it something a
			// countdown may be drawn from (phase.go).
			// PACED OR MERELY TRYING AGAIN, and the difference is whose fault
			// the wait is. A rate limit is the router asking us to slow down
			// and it is the one wait with a moment attached to it, so it gets
			// its own word; everything else here is a fault we are re-asking
			// after. [pacedSince] is set on the first 429 of the call and is
			// the honest reading of "this call is being paced", where the
			// header's own figure is spent after one attempt.
			paced := PhaseRetrying
			if !pacedSince.IsZero() {
				paced = PhasePaced
			}
			now := c.clock()
			notePhase(ctx, c.modelFor(request), paced,
				ordinalOf(attempts, width()), now, now.Add(delay), "")
			// Spent. It described one moment to come back at, and coming back
			// is what we are doing; carrying it forward made a single 429 set
			// the floor for every remaining attempt of the call.
			providerWait = 0
			waitBegan := dispatchNow()
			if err := c.wait(ctx, delay); err != nil {
				return nil, err
			}
			if took := dispatchNow().Sub(waitBegan); took < delay {
				owed += delay - took
			}
		}
		reconnected, moved, switchedDoor = false, false, false
		if waited, err := c.waitConnection(ctx, c.modelFor(request), currentBase, false); err != nil {
			return nil, err
		} else if waited && knobs.trace != nil {
			knobs.trace.connectionRecovered = true
		}

		// A stream gets its own cancellable context so the idle watchdog has
		// something to pull. Every path that does not hand the body back to the
		// caller releases it here; the path that does hands the cancel to the
		// watchdog, which fires it on stall or on Close.
		attemptCtx, cancelAttempt := attemptContext(ctx, stream)
		var sent atomic.Bool
		// AND THE CONNECTION IS MEASURED AS WELL AS THE ANSWER. The hooks cost
		// nothing and they are the only thing that can separate a cold pool from
		// a slow model in `ttft_ms` — a person who read an answer for two minutes
		// and then typed again used to pay a whole handshake inside this Do, and
		// the log filed every millisecond of it as the endpoint being slow
		// (conntrace.go).
		attemptCtx = httptrace.WithClientTrace(attemptCtx, knobs.trace.openConn(&sent))

		// AND THE BYTES ARE WRITTEN HERE, immediately before the send, which is
		// the same timing [Client.providerPreferences] exists for: the machine
		// that refused a moment ago is off this body, and the ledger's own
		// cooldowns are read as they stand now rather than as they stood when the
		// call began. A caller that composed its body itself — the document path,
		// which posts a multipart form rather than a completion — carries no
		// refusal set and keeps the bytes it was given.
		if attempt > 0 && knobs.refused != nil {
			sentVeto = knobs.refused.list()
			written, err := c.encodeRequest(request, knobs)
			if err != nil {
				cancelAttempt()
				return nil, fmt.Errorf("marshal request: %w", err)
			}
			body = written
			if knobs.trace != nil && knobs.trace.body != nil {
				knobs.trace.body = body
			}
		}
		httpRequest, err := c.newHTTPRequestAt(attemptCtx, request, body, stream, currentBase)
		if err != nil {
			cancelAttempt()
			return nil, err
		}
		if err := sharedLimiter.acquire(ctx); err != nil {
			cancelAttempt()
			return nil, err
		}
		// AND THIS ATTEMPT GETS ITS OWN CONTROLLER. Every send of every request is
		// watched, and it is watched from the moment its own bytes leave — see
		// EVERY ATTEMPT IS WATCHED in armwatch.go for the 363-second attempt whose
		// row said nothing because its arm's first attempt had already spoken.
		streamWatchFrom(ctx).attempt(dispatchNow())
		attemptBegan := logNow()
		// THE ROW THAT SAYS A CALL IS IN FLIGHT, written before the wait rather
		// than after it. Without it a planning call four minutes into a
		// 65,536-token ceiling is indistinguishable from an idle process: the
		// log's newest line is the call BEFORE it, and everything a person can
		// see says "nothing since four minutes ago". Its partner is whichever
		// row ends this attempt, paired by the id they share.
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: stream,
			attempt: attempts, began: attemptBegan, phase: calllog.PhaseStart,
		})
		response, err := httpClient.Do(httpRequest)
		if err == nil && response != nil && response.StatusCode == http.StatusPaymentRequired {
			// Every call shape passes this transport door, including reflexes,
			// titles, tasks, and the affordable retry itself.
			announcePaymentRequired(c.config.BaseURL)
		}
		// Custom transports may omit Request; retain the exact sent object so
		// refusal learning never reconstructs it from mutable routing state.
		if response != nil && response.Request == nil {
			response.Request = httpRequest
		}
		if err != nil {
			sharedLimiter.release(false, 0)
			cancelAttempt()
			// EVERY ATTEMPT THAT FAILED LEAVES A ROW, not only the last one. A
			// call that was paced four times and then landed reads as one slow
			// call in any surface above this; the four rows are the whole of
			// why it was slow (calllog.go).
			c.record(recordFacts{
				ctx: ctx, request: request, knobs: knobs, stream: stream,
				attempt: attempts, began: attemptBegan, err: err,
			})
			// A cancelled or expired parent is a decision, not a fault. Retrying
			// it would burn the remaining deadline on calls that cannot land.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("execute request: %w", err)
			}
			// A TRANSPORT MAY KNOW THE CREDENTIAL IT OWNS IS FINISHED. That is
			// not a reachability fault and cannot improve under this dispatcher's
			// machine walk; preserve the typed cause for the session's person-facing
			// sentence and return after the one request that established it.
			if terminalTransportFailureFrom(err) {
				return nil, fmt.Errorf("execute request: %w", err)
			}
			lastErr = fmt.Errorf("execute request: %w", err)
			// AND A FAULT IS NOT A CEILING, whatever an earlier attempt of this
			// call learned: the bytes never reached anybody, so nothing has been
			// established about where they may go next.
			accountPaced = false
			// A TRANSPORT FAULT NAMES NOBODY, so nothing is excluded from the next
			// body: the bytes never reached a machine that could be blamed, and
			// writing one down would be this loop guessing. What the next attempt
			// still gets is a fresh encode, which re-reads every cooldown the
			// ledger has written since this call began.
			if connectionFailure(err) && !sent.Load() {
				if knobs.trace != nil {
					knobs.trace.connectionRecovered = true
				}
				// A DNS or dial failure precedes accepted generation. Wait for
				// the origin, not another provider behind that same origin. The
				// dispatcher plan remains the one bound when the check answers but
				// the send still fails.
				if recoveryCtx == nil {
					var cancelRecovery context.CancelFunc
					left := plan.Left(spentAt())
					if left <= 0 {
						return nil, &ConnectionUnavailableError{}
					}
					recoveryCtx, cancelRecovery = context.WithTimeout(ctx, left)
					defer cancelRecovery()
				}
				if _, waitErr := c.waitConnection(recoveryCtx, c.modelFor(request), httpRequest.URL.String(), true); waitErr != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					if recoveryCtx.Err() != nil {
						return nil, &ConnectionUnavailableError{}
					}
					return nil, waitErr
				}
				// A healthy check does not spend a machine or a request-shape move.
				// The first recovered pass is immediate; a further failure waits
				// at the top of the next pass under this plan's deadline.
				connectionPasses++
				reconnected = true
				continue
			}
			connectionPasses = 0
			if c.handBack(ctx, knobs, "", false) {
				return nil, lastErr
			}
			// AND NOTHING BREAKS OUT HERE ANY MORE. `attempt >= maxAttempts-1`
			// was the private three-fault budget; the deadline and
			// [control.Next] are asked at the top of the next pass, and they are
			// the whole of what ends a call. A transport fault names nobody, so
			// the move it earns is an ordinary one and it is paid for with the
			// backoff above — which is what stops a dead resolver being asked at
			// connection speed for the length of the deadline.
			continue
		}
		connectionPasses = 0
		potentialRateLimit := response.StatusCode == http.StatusTooManyRequests
		// Error bodies can stall too. They are read below before a retry, so
		// they need the same idle bound as successful streaming bodies.
		if stream {
			response.Body = newIdleWatchdog(response.Body, streamIdleTimeout, cancelAttempt)
		}
		// The provider's comeback instruction is read before the slot goes back,
		// because it is what tells the limiter how wide this 429's window is:
		// one window, one halving (limiter.go).
		var named time.Duration
		if potentialRateLimit {
			named = retryAfter(response)
		}
		// ── A REFUSAL THAT NAMED A MACHINE IS THAT MACHINE'S, AND THE OTHERS
		// HAVE SAID NOTHING ─────────────────────────────────────────────────
		//
		// THE OWNER'S RULING, 2026-09-10: "that is in OpenRouter, that does not
		// matter — if we get any such error or error in general we need to
		// recover." An account-policy 404 and a 400 the router RELAYED from the
		// endpoint it picked are neither a person's mistake nor a fact about the
		// model: they are one machine saying no, and the answer to them is the
		// same walk a 429 gets. Until this line they ended the call, and the
		// rotation that eventually moved was the SESSION's — three whole turns
		// apart, each learning the same fact over again through the ledger
		// (refusal_test.go's `TestThreeAttemptsAfterAnUpstreamRefusalReach…`).
		//
		// A REFUSAL THAT NAMED NOBODY IS NOT THIS. That one is the router
		// reading our own bytes and saying no, and every machine alive will say
		// the same thing about the same request ([taxonomy.Evidence.OurBytes]);
		// it is handed back whole, exactly as it always was. So is anything this
		// adapter can repair by itself — a knob it guessed wrong about reaches
		// [Client.sendRepaired] a layer up, and walking it would spend a machine
		// to discover a fact the memo already answers.
		var peek []byte
		if !retryableStatus(response.StatusCode) {
			if !relayedByAMachine(response.StatusCode) || c.repairable(c.modelFor(request), knobs) ||
				len(knobs.reasoning) > 0 {
				sharedLimiter.release(false, 0)
				return response, nil
			}
			read, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
			if upstream, ok := routerErrorEnvelope(read); !ok || upstream == "" {
				response.Body = rewound(read, response.Body)
				sharedLimiter.release(false, 0)
				return response, nil
			}
			peek = read
		}
		// Drain a bounded prefix before closing so the connection can be reused
		// and the eventual error still says what the provider complained about.
		// A relayed refusal above has already read it and must not read it twice.
		if peek == nil {
			peek, _ = io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
		}
		response.Body.Close()
		cancelAttempt()
		lastErr = apiError(response.StatusCode, peek)
		// AND THE COMEBACK IS READ OUT OF THE BODY WHEN NO HEADER CARRIED IT. The
		// census found `retry_after` recorded on not one row in ten days, while
		// every fourth failure was a 429: the header is what [retryAfter] reads
		// and this router routinely names its wait in the refusal envelope
		// instead. A wait we cannot see is a wait we replace with our own
		// doubling, which is how a pool that asked for three seconds was asked
		// again after seven hundred milliseconds.
		if potentialRateLimit && named <= 0 {
			named = retryAfterIn(peek)
		}
		// EVERY REFUSAL THIS LOOP DRAWS GOES THROUGH THE ONE DOOR, and what is
		// done about it is decided there from what the refusal says rather than
		// here from the status this loop happens to be holding (velocity.go's
		// [Client.refuseLane], which carries the measurement that law comes
		// from). A 5xx naming its upstream takes that lane away; a 429 naming
		// its pool is paced for the wait it asked for, which is why that wait
		// is handed over rather than left in this loop; a 429 naming nobody is
		// this account's own ceiling and writes nothing at all.
		refusal := c.refuseUpstream(request, knobs, lastErr, "", named)
		// ONLY ORDINARY PACING NARROWS THE SHARED LIMITER. A payment refusal, a
		// plan pause and a door this key cannot use all arrive under 429 too, but
		// none says the request rate is too high.
		rateLimited := refusal.Kind == refusalPaced
		if !rateLimited {
			named = 0
		}
		sharedLimiter.release(rateLimited, named)
		if rateLimited {
			providerWait = named
			if pacedSince.IsZero() {
				pacedSince = time.Now()
			}
			// The park begins on the FIRST ordinary pacing refusal this call
			// draws — and it is announced through [park], which is THE ONE SITE
			// that tells both readers of that fact at once. Saying it by hand
			// here would leave the call-watch seam believing this call never
			// waited (#868).
			park(true)
		}
		// AND THE MACHINE THAT REFUSED IS OFF THE NEXT BODY. This is the whole of
		// "never repeat" (see THE BODY IS WRITTEN PER ATTEMPT above): the door
		// above has already decided what the refusal MEANS for the process, and
		// this decides what it means for the remaining attempts of this one call,
		// which is the narrower and shorter-lived question. A refusal that
		// implicated no machine adds nothing.
		fresh := knobs.refused.add(refusal.Lane)
		// AND A PACE THAT NAMED NONE OF A POOL IS THE KEY'S OWN CEILING. Both
		// halves are needed and neither is the status alone: a base this process
		// has never seen publish a set has one machine, and "come back later"
		// from it is answered by coming back later ([Client.baseServesLanes]).
		accountPaced = rateLimited && strings.TrimSpace(refusal.Lane) == "" && c.baseServesLanes()
		// AND THE MOVE LOG IS CORRECTED TO THE MACHINE THAT ANSWERED. A move
		// NAMES THE MACHINE WE EXPECT AND NEVER THE MACHINE WE COMMAND
		// ([control.Move]) — `provider.order` is advisory once `allow_fallbacks`
		// is on, and R1 measured this router fanning straight past it — so the
		// machine a move claimed and the machine that refused are routinely two
		// different pools. Writing the claim down and leaving it there tells
		// [control.Next] that the lane we asked for has been tried when nothing
		// was ever sent to it, which takes away the one machine we actually
		// wanted on the evidence of a machine we did not.
		//
		// So the claim that never reached the wire is given back and the pool
		// that really answered is written down in its place. That is exactly
		// what [control.MoveLog.Release] is for, and it keeps "never repeat"
		// about where the bytes WENT rather than about where we hoped they
		// would go.
		if served := strings.TrimSpace(refusal.Lane); served != "" {
			asked := strings.TrimSpace(move.Lane)
			if attempt == 0 {
				asked = strings.TrimSpace(plan.Lane)
			}
			if asked != "" && !strings.EqualFold(asked, served) {
				plan.Moves.Release(control.Move{
					Kind: control.MoveMachine, Model: plan.Model,
					Lane: asked, Shape: move.Shape,
				})
			}
			plan.Moves.Add(control.Move{
				Kind: control.MoveMachine, Model: plan.Model,
				Lane: served, Shape: move.Shape,
			})
		}
		// AND THE ROW IS WRITTEN FROM WHAT THE DOOR WORKED OUT, which is why it
		// is here and not above the door.
		//
		// The classification is the only thing that can name the machine on a
		// refusal that never opened a stream: `refusal.Lane` is the pool the
		// router said was full, and it is the same name the ledger is now keyed
		// on (velocity.go). Recording the row first and then classifying would
		// be the two reading one refusal twice, which is the shape #123 closed —
		// and the row would fall back to the demand on every refusal that named
		// a machine we had not demanded.
		//
		// THE COMEBACK TIME GOES ON IT TOO, because it is the only thing that
		// makes a repeated send to a refusing machine legal
		// (docs/design/recovery/DESIGN.md §3). It was recorded on not one row in
		// the ten days to 2026-09-10, so eleven hundred paced refusals could not
		// be checked against what the provider itself had asked for.
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: stream,
			attempt: attempts, began: attemptBegan,
			status: response.StatusCode, err: lastErr, responseBody: peek,
			served: refusal.Lane, retryAfter: named,
		})
		// MONEY MOVES ONLY THROUGH THIS EXACT OPT-IN. A paused fixed-price window
		// never reaches the endpoint walk or model chain: it either takes the one
		// configured metered door, once, or returns its typed pause immediately.
		if refusal.Kind == refusalPlanPaused {
			useOverflow := c.config.OverflowOnPlanPause &&
				strings.TrimSpace(c.config.PlanOverflow) != "" && !usingOverflow &&
				claimPlanOverflow(ctx)
			if useOverflow {
				currentBase = strings.TrimRight(strings.TrimSpace(c.config.PlanOverflow), "/")
				currentDoor = strings.TrimSpace(c.config.PlanOverflowDoor)
				if phase := phaseClockFrom(ctx); phase != nil {
					phase.useDoor(currentDoor)
				}
				usingOverflow = true
				switchedDoor = true
				pacedSince = time.Time{}
				continue
			}
			detail := planPauseDetail(refusal.Reset, c.config.PlanOverflowDoor)
			now := c.clock()
			notePhase(ctx, c.modelFor(request), PhasePlanPaused, detail, now, now, "")
			return nil, &PlanPauseError{
				Reset: refusal.Reset, OverflowDoor: c.config.PlanOverflowDoor, Cause: lastErr,
			}
		}
		if refusal.Kind == refusalPayment || refusal.Kind == refusalNoPlan {
			return nil, lastErr
		}
		// ── "NOT YET" IS ANSWERED BY GOING SOMEWHERE ELSE ───────────────────
		//
		// THE OWNER'S RULING, 2026-09-10: a person must never work around a 429
		// by switching models themselves. So the three moves, in this order.
		//
		// FIRST, ANOTHER MACHINE, AT ONCE. The refusing machine is off the next
		// body and the next body goes out with no backoff at all — see [moved].
		//
		// SECOND, THE MODEL. When every machine this request may go to is being
		// held for a wait, a conversation's call gives the refusal back
		// immediately: `pacingExhausted` reads it as the pacing door it is
		// (endpoints.go) and the session offers the next model, which is always
		// faster than a window. A person never waits out a window while another
		// model exists.
		//
		// THIRD, AND ONLY WHEN THERE IS NOWHERE ELSE AT ALL, the wait — and then
		// it is the earliest window's own end, which is the moment a surface
		// draws a countdown to rather than our doubling.
		held, spent := c.pacedOut(c.modelFor(request), knobs)
		if rateLimited && spent {
			if !patient {
				return nil, lastErr
			}
			// The machine's own answer to this call wins, because it is the
			// window said out loud; the ledger's hold is what is left when this
			// refusal named nothing — a window some EARLIER call was told about,
			// which is still the honest moment to come back at.
			if wait := held.Sub(c.clock()); providerWait <= 0 && wait > 0 && wait < maxProviderWait {
				providerWait = wait
			}
		}
		// AND THE WALK IS FREE FOR AS LONG AS THE CALL HAS, which is what
		// replaced `freeMoves`. Eight was a guess at where a walk stops being
		// worth having — past that a bad minute could spend a seventeen-machine
		// roster at connection speed, which is the traffic an account-wide limit
		// is made of — and the honest answer was always the deadline: a walk
		// cannot outrun it, so it cannot outrun it for free either.
		moved = fresh && !spent
		// ONE RECOVERY OWNER, ASKED ONCE. Two exits used to answer this — the
		// walk's for a fault and the full demanded pool's for a 429 (#835) — and
		// keeping them apart is how the same question came to have two answers
		// that could disagree. See [Client.handBack].
		if c.handBack(ctx, knobs, refusal.Lane, rateLimited) {
			return nil, lastErr
		}
		// ── AN EXCLUSION THAT DID NOT TAKE IS NOWHERE ELSE TO GO ────────────
		//
		// A refusal that NAMED a machine bought this call one move, and the move
		// is only worth taking if it really was a move: an answer from a machine
		// THAT HAD ALREADY REFUSED THIS CALL is the router telling us, in the
		// only way it can, that we are back where we started — either the veto
		// changed nothing (the pool is that one machine, or this base does not
		// honour `provider.ignore`), or the body asked for that machine again
		// because nothing else in the set was still servable ([sentVeto] says
		// which cases those are). Either way there is no other endpoint to reach
		// and the honest thing is to hand the refusal back, which is what the
		// layer above knows how to answer (the ladder, then the session's one
		// model hop).
		//
		// IT IS ASKED OF THE EVIDENCE AND NO LONGER OF THE STATUS. It used to
		// read `relayed` — a 4xx that would not otherwise have been retried — so
		// the commonest named refusal in the log was exempt from it, and a
		// per-machine 429 rode the account-limiter road instead: eight sends to
		// one pool over ninety seconds on 2026-09-11 14:39, each body naming that
		// pool in `provider.ignore` and each answered by it regardless, while six
		// other machines on the same model were serving in under five seconds.
		// Who refused is the fact; the number it wore is the upstream's
		// ([taxonomy.Evidence.Named]).
		//
		// A REFUSAL THAT NAMED NOBODY IS NOT THIS. Nothing was vetoed, so no veto
		// failed; that road is the wait and then the model
		// ([control.Plan.AccountRefused]).
		if namesEndpoint(sentVeto, refusal.Lane) && !demandedThisOne(knobs, refusal.Lane) {
			return nil, lastErr
		}
		// AND THE SHORTER PATIENCE FOR A FAULT IS GONE WITH THE LONGER ONE FOR A
		// 429. `!rateLimited && attempt >= maxAttempts-1` was the last of the
		// six budgets this loop owned; the plan is asked at the top of the next
		// pass and answers for both.
	}
	if connectionPasses > 0 {
		return nil, &ConnectionUnavailableError{}
	}
	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	// The count is what this call actually spent rather than the constant it
	// was bounded by: a patient call has no constant to name, and a fault that
	// broke out after three attempts never had six.
	return nil, fmt.Errorf("after %d attempts: %w", attempts, lastErr)
}

// terminalTransportFailureFrom is a narrow structural seam for transports
// that own credentials the provider package must not import. The exported
// method lets the typed cause survive wrappers without coupling the dispatcher
// to a particular account implementation.
func terminalTransportFailureFrom(err error) bool {
	var terminal interface{ TerminalTransportFailure() bool }
	return errors.As(err, &terminal) && terminal.TerminalTransportFailure()
}

// PlanPauseError is the typed end of a request whose fixed-price window is
// temporarily unavailable. Cause preserves the vendor refusal for the journal;
// surfaces read this type so the person sees only the actionable pause sentence
// rather than a generic API error after it.
type PlanPauseError struct {
	Reset        string
	OverflowDoor string
	Cause        error
}

type planOverflowGuard struct{ used atomic.Bool }
type planOverflowGuardKey struct{}

// WithPlanOverflowGuard gives every request belonging to one turn the same
// one-shot billing decision. Repair, relaxation, hedge and model-hop re-entry
// all derive contexts from this one, so none can buy a second metered attempt
// after another road has already taken the separately authorised overflow.
func WithPlanOverflowGuard(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, held := ctx.Value(planOverflowGuardKey{}).(*planOverflowGuard); held {
		return ctx
	}
	return context.WithValue(ctx, planOverflowGuardKey{}, &planOverflowGuard{})
}

func claimPlanOverflow(ctx context.Context) bool {
	guard, _ := ctx.Value(planOverflowGuardKey{}).(*planOverflowGuard)
	// send is package-private and its shipped callers install the guard before
	// entering. Keeping the absent case usable preserves focused dispatcher rigs
	// without turning a missing test harness value into a billing refusal.
	return guard == nil || guard.used.CompareAndSwap(false, true)
}

func (e *PlanPauseError) Error() string {
	if e == nil {
		return ""
	}
	return PlanPauseSentence(e.Reset, e.OverflowDoor)
}

func (e *PlanPauseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// PlanPauseFrom recovers a plan-pause ending through the wrappers added by the
// provider and session loops.
func PlanPauseFrom(err error) (*PlanPauseError, bool) {
	var paused *PlanPauseError
	if errors.As(err, &paused) && paused != nil {
		return paused, true
	}
	return nil, false
}

// PlanPauseSentence is the one person-facing sentence for a temporarily spent
// subscription window, shared by live status, the final row, and connection.
func PlanPauseSentence(reset, overflowDoor string) string {
	detail := planPauseDetail(reset, overflowDoor)
	if detail == "" {
		return string(PhasePlanPaused)
	}
	return string(PhasePlanPaused) + " · " + detail
}

func planPauseDetail(reset, overflowDoor string) string {
	parts := make([]string, 0, 2)
	if reset = strings.TrimSpace(reset); reset != "" {
		parts = append(parts, "resets at "+reset)
	}
	if overflowDoor = strings.TrimSpace(overflowDoor); overflowDoor != "" {
		parts = append(parts, "/connect can switch to "+overflowDoor)
	}
	return strings.Join(parts, " · ")
}

// demandedThisOne reports that this request DEMANDED this one machine and
// nothing else — a rescue's arm, or a person's strict pin.
//
// IT IS WHAT KEEPS "THE VETO DID NOT TAKE" AN HONEST QUESTION. The encoder
// leaves a demand down to its last machine exactly as it stands and never vetoes
// a demanded name ([Client.dropRefusedHere]), so a machine that answers such a
// request was never taken off anything and its refusal proves nothing about
// whether a veto works. That case is the one legal same-machine move and is
// answered above, by the comeback the machine asked for itself.
//
// IT ASKS THE DEMAND AND NOTHING ELSE, which is why it goes through
// [demandedLane] rather than [requestSet]. `requestSet` answers a wider
// question — "which machines may this request go to" — and with no demand at all
// it answers with the chooser's ADVISORY candidate set, a ranking sent with
// fallbacks left on. A model this build has timed exactly one machine for has a
// candidate set of one while the router still has the whole pool, so reading
// that as a demand would exempt the narrowest belief this process can hold from
// the law above and drop it back onto the measured chain. The encoder vetoes an
// advisory name freely; only a demand is spared.
func demandedThisOne(knobs callKnobs, lane string) bool {
	lane = strings.TrimSpace(lane)
	if lane == "" {
		return false
	}
	demanded, _ := demandedLane(knobs)
	return demanded != "" && strings.EqualFold(strings.TrimSpace(demanded), lane)
}

// ── THE RUNG IS A MOVE LIKE ANY OTHER ───────────────────────────────────────

// recoverFromRefusal climbs the relaxation ladder, narrating each rung, and ends
// in an error a person can act on.
//
// IT ASKS [control.Next] FOR EACH RUNG AND WALKS NOTHING ITSELF. It used to be
// endpoints.go's own `for _, step := range c.relaxationPlan(...)`, which is the
// seventh of the eleven budgets docs/design/recovery/DESIGN.md §2 counts: a loop
// with a private length, under a deadline that could not see it, beside a move
// generator that already knew the rungs. The rungs are the plan's
// ([control.Plan.Shapes], written from [Client.relaxationPlan], which is still
// the one authority on which fields this body actually carries); which one comes
// next, and whether one comes at all, is [control.Next]'s answer like every other
// move; and the climb is recorded on the SAME move log the machine walk writes
// into, so a rung and a machine are one history rather than two.
//
// THE REFUSAL IT ANSWERS IS ABOUT THE BYTES AND NOT ABOUT A MACHINE, which is
// the one thing the generator cannot see for itself — see
// [control.Plan.ShapeRefused].
//
// The first attempt has already happened and been refused — its body is `first`
// — so every attempt this function makes is numbered from one as a RETRY, which
// is what the line says.
func (c *Client) recoverFromRefusal(
	ctx context.Context,
	request *ai.Request,
	knobs callKnobs,
	stream bool,
	first []byte,
) (*http.Response, error) {
	model := c.modelFor(request)
	// The rungs this body actually has, as DATA, from the one place that knows
	// (endpoints.go). `steps` is what each rung takes off and what it is called;
	// `plan.Shapes` is the same list as the generator reads it.
	steps := c.relaxationPlan(request, knobs, model)
	plan := c.dispatchPlan(ctx, request, knobs)
	plan.ShapeRefused = true
	total := len(steps)
	if total == 0 {
		return nil, c.refusalError(request, knobs, model, nil, 1, first)
	}
	last := first
	stripped := make([]string, 0, total)
	climbed := 0
	for {
		// AND THE DEADLINE IS ASKED FIRST, exactly as it is in [Client.send]: a
		// rung is a whole request, and a ladder that climbed past the moment this
		// build has already decided to stop at is the multiplication again.
		if plan.Spent(dispatchNow()) {
			break
		}
		move := control.Next(plan, plan.Moves.List())
		if move.Kind != control.MoveShape {
			break
		}
		plan.Moves.Add(move)
		// The relaxations ACCUMULATE. Each rung is climbed on top of the last,
		// because the refusal never says which field it objected to — a body that
		// still carries the reasoning knob has not tested whether dropping the
		// output cap was enough. The move names the rung REACHED, so the body is
		// built from every rung up to it rather than from the newest one alone,
		// which is also what makes a ladder resumed part-way honest.
		rung := int(move.Shape)
		if rung > total {
			break
		}
		relaxed := knobs
		for _, step := range steps[:rung] {
			relaxed.relaxed |= step.bit
		}
		step := steps[rung-1]
		// THE MEMO FOLLOWS THE SECOND REFUSAL. Reaching this rung means the
		// endpoint-membership retry, when there was one, was refused under the
		// original price ceiling too. That is evidence the ceiling must come off;
		// the first refusal alone could have been caused by any membership field.
		if step.bit == relaxPriceCeiling && c.velocity != nil {
			c.velocity.refuseCeiling(model)
		}
		stripped = append(stripped, step.name)
		climbed = rung
		Emit(ctx, StreamNotice, fmt.Sprintf("Retry %d/%d: %s", rung, total, step.label))
		// AND THE PHASE CLOCK CARRIES THE SAME RUNG, so the status line says
		// "trying again · 2 of 6" while the notice above says which knob went.
		// It is the same fact at two grains and it is stated once, here, from
		// the same pair of numbers (phase.go).
		notePhase(ctx, model, PhaseRetrying,
			ordinalOf(rung, total), c.clock(), time.Time{}, "")
		response, payload, err := c.attemptShaped(ctx, request, relaxed, stream)
		if err != nil {
			return nil, err
		}
		if response != nil {
			return response, nil
		}
		last = payload
	}
	return nil, c.refusalError(request, knobs, model, stripped, climbed+1, last)
}

// demandedLane is the ONE machine this request may go to, and whether a person
// is the one who said so.
//
// TWO DEMANDS AND ONE FIELD. A rescue demands the machine the primary is not on
// (hedge.go's [hedgePreference]) and a person's strict pin demands the machine
// they named (lanepin.go); both arrive on the wire as `provider.only` with
// `allow_fallbacks: false`, and both mean the same thing to this loop — the
// serving set under this request is one machine wide, so there is no other
// machine for the next body to go to. What they do NOT share is who may
// override them, which is the second return: a rescue's demand is this build's
// own tactic and the walk is free to replace it, while "and nowhere else" is a
// sentence a person wrote and nothing here may quietly widen.
//
// A demand naming SEVERAL machines is not one machine wide, so it answers empty
// and [Client.dropRefusedHere] narrows it a machine at a time instead.
func demandedLane(knobs callKnobs) (lane string, strict bool) {
	if knobs.hedgeLane != "" {
		return knobs.hedgeLane, false
	}
	if knobs.laneChoice != nil && len(knobs.laneChoice.Only) == 1 {
		return strings.TrimSpace(knobs.laneChoice.Only[0]), true
	}
	return "", false
}

// handBack is the ONE place this loop gives a refusal to the recovery owner
// above it rather than taking the next move itself.
//
// IT FOLDS THE TWO EXITS #835 LEFT BEHIND — `retryElsewhere`, which asked
// whether the race could fund an alternative, and `demandedPoolIsFull`, which
// asked whether a rescue's one machine was the pool that refused. They are one
// question asked of two halves of the same fact, and asked separately they were
// free to disagree: a 429 naming a rescue's machine went back, a 502 naming it
// did not, and neither knew what the other had decided.
//
// THE RULE, in the order it applies:
//
//	a strict pin       never hands back — the walk demands a DIFFERENT machine,
//	                   which is the one thing "and nowhere else" forbids
//	a demand refused   goes back at once: this body can never leave that queue,
//	                   and the race always has an answer for it (the walk, or a
//	                   ladder a door deferred, [hedgeRace.exhausted])
//	an account pacing  never hands back, because every machine behind the model
//	                   is under the same account and moving would multiply the
//	                   traffic that earned the limit
//	anything else      goes back when the race can fund an alternative, because
//	                   one watched request has one recovery owner
func (c *Client) handBack(ctx context.Context, knobs callKnobs, refused string, rateLimited bool) bool {
	demanded, strict := demandedLane(knobs)
	if strict {
		return false
	}
	// The refusal names the machine we demanded, or names nobody at all — and
	// when only one machine was asked, nobody named is that machine.
	if demanded != "" && (refused == "" || equalLane(refused, demanded)) {
		return true
	}
	if rateLimited {
		return false
	}
	return streamWatchFrom(ctx).walkAvailable()
}

// retryAfterIn is the comeback a refusal named in its BODY, zero when it named
// none there.
//
// The header is read first ([retryAfter]) because it is the protocol's own
// answer; this is the router's, and on the rows the census counted it is the
// only one that ever arrives. Two spellings reach us — a seconds figure beside
// the error and one nested in its metadata — and both are read as seconds,
// which is the unit the envelope uses. A figure that is absurd is left to
// [backoffFor]'s cap exactly as a header's is: this only says what was asked.
func retryAfterIn(payload []byte) time.Duration {
	if len(payload) == 0 {
		return 0
	}
	var envelope struct {
		RetryAfter float64 `json:"retry_after"`
		Error      struct {
			RetryAfter float64 `json:"retry_after"`
			Metadata   struct {
				RetryAfter float64 `json:"retry_after"`
			} `json:"metadata"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return 0
	}
	for _, seconds := range []float64{
		envelope.RetryAfter,
		envelope.Error.RetryAfter,
		envelope.Error.Metadata.RetryAfter,
	} {
		if seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
	}
	return 0
}

// ── WHAT USED TO BE HERE ────────────────────────────────────────────────────
//
// `outOfPatience` answered "has this call spent everything it is willing to
// spend on being paced" from two private currencies — six attempts or two
// minutes for a watched call, sixty or ten for a patient one — and `patienceOf`
// turned one of them into the denominator a person read. Both are deleted.
// [control.Plan.Spent] is the one place a call gives up, on one deadline in the
// person's own time, and the ordinal's denominator is how many machines this
// request may really go to.

// backoffFor is how long to wait before one retry.
//
// Exponential and jittered, so several leaves that were rate-limited together
// do not all come back at the same instant and trigger it again. The provider's
// own Retry-After raises that wait when it named a later moment — it knows its
// own pacing better than the formula does — but only up to maxProviderWait.
// The header may be an HTTP date, and a date is how a wait becomes hours: the
// old code took it literally and applied it, and then went on applying it to
// every later attempt of the same call.
//
// ONE WAIT IS NEVER LONGER THAN maxProviderWait, whatever asked for it — the
// header, or the doubling. A patient call (patience.go) has no attempt ceiling
// to keep the exponent small, so the cap is what keeps its waits a minute
// apart instead of an hour, and it is what makes an interrupt land promptly:
// the longest a stopped call can still be sitting in a timer is one of these.
func backoffFor(attempt int, providerWait time.Duration) time.Duration {
	if attempt > maxBackoffShift {
		attempt = maxBackoffShift
	}
	delay := time.Duration(float64(baseBackoff) * float64(int(1)<<uint(attempt-1)))
	delay += time.Duration(rand.Int63n(int64(delay / 2)))
	if providerWait > maxProviderWait {
		providerWait = maxProviderWait
	}
	if providerWait > delay {
		delay = providerWait
	}
	if delay > maxProviderWait {
		delay = maxProviderWait
	}
	return delay
}

// clientFor picks how this call is bounded in time.
//
// A completion is bounded in total: the answer arrives in one piece, so the
// adaptive budget is a real statement about when the call has gone wrong. A
// stream is not bounded in total at all, because http.Client.Timeout covers
// body reads and the body reads are the answer — that deadline killed every
// stream at the budget however healthily it was delivering. What bounds a
// stream is silence: the header deadline on the streaming transport and the
// idle watchdog send wraps the body in.
//
// A completion's total deadline is the adaptive budget, and once the lane
// has finished a reply for us it is also held under the lane's measured wall
// ([Client.completionWall]) — the same figure a stream on that lane is cut
// at, so a wedged endpoint costs a headless worker minutes rather than the
// whole run.
//
// AND AN ENDPOINT THAT ANSWERS WHOLE IS BOUNDED IN TOTAL WHATEVER IT WAS ASKED
// FOR. A gateway that takes `stream: true` and generates the entire reply before
// it sends a header has no silence to measure and no first token to wait for, so
// the streaming transport's header deadline would be a deadline on the
// generation rather than on a stall. [Client.unstreamable] is that endpoint
// having said so on the wire, and from then on its calls are bounded the way an
// answer with no inside can be — by the adaptive budget and, on a lane we have
// measured, by that lane's wall.
func (c *Client) clientFor(model string, stream bool, maxTokens int) http.Client {
	if stream && !c.unstreamable.Load() {
		client := *c.stream
		client.Timeout = 0
		return client
	}
	client := *c.http
	client.Timeout = adaptiveCompletionTimeout(maxTokens, c.config.Timeout)
	if wall, measured := c.completionWall(model); measured && wall < client.Timeout {
		client.Timeout = wall
	}
	return client
}

// attemptContext gives a streamed attempt a cancel of its own. Only a stream
// needs one: its body outlives send, so the thing that stops it has to be
// handed on to the watchdog rather than deferred here. A non-streamed attempt
// keeps the caller's context and gets a cancel that does nothing, which lets
// every exit path call it unconditionally.
func attemptContext(ctx context.Context, stream bool) (context.Context, context.CancelFunc) {
	if !stream {
		return ctx, func() {}
	}
	return context.WithCancel(ctx)
}

// retryableStatus separates "try again" from "this will never work" ON THE
// STATUS ALONE. A 429 is the provider pacing us and a 5xx is it failing; both
// are worth asking again.
//
// IT IS NO LONGER THE WHOLE ANSWER FOR A 4XX, and that is the change of
// docs/design/recovery/DESIGN.md §3: "a 4xx is a request we built wrong" is true
// only of a 4xx the ROUTER answered for itself. One it RELAYED is an endpoint's
// verdict on this request, and the model's other endpoints have said nothing —
// see [relayedByAMachine] and the walk in [Client.send].
func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// relayedByAMachine reports whether a status is one a router relays on an
// endpoint's behalf, which is the half of the question the STATUS can answer.
// The other half — whether this particular body actually named one — is
// [routerErrorEnvelope]'s, and both have to be yes.
//
// 401, 402 and 403 are deliberately not here. A key that is wrong, an account
// with no credit and a key without permission are facts about THIS PROCESS, and
// every machine behind every model will answer them identically; walking the
// pool to hear it four more times is the shape this whole design deletes,
// pointed the wrong way.
func relayedByAMachine(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestTimeout,
		http.StatusConflict, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// ordinalOf spells which of how many attempts this is, the way a person says
// it: "2 of 6". An unknown total draws nothing rather than a bare number with
// no scale beside it.
//
// AND A COUNT PAST ITS OWN TOTAL DRAWS NOTHING EITHER. "4 of 3" is not a scale,
// it is arithmetic leaking, and it is reachable now that the total is how many
// machines a request may go to rather than a constant nobody could exceed: a
// transport fault names no machine, so it spends a pass of the loop without
// spending one of the set.
func ordinalOf(attempt, total int) string {
	if attempt <= 0 || total <= 0 || attempt > total {
		return ""
	}
	return strconv.Itoa(attempt) + " of " + strconv.Itoa(total)
}
