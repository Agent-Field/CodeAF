package provider

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE RACE: THE TRANSPORT HALF OF THE CONTROLLER ──────────────────────────
//
// `internal/lane/control` says WHEN a wait has gone on long enough to act on.
// This file is what happens next: another request to another lane, on a child
// context, with the first arm to earn the answer keeping it and the others
// cancelled — because cancelling is what stops the bill on the twenty-odd lanes
// that honour it.
//
// FIVE RULES HOLD THE WHOLE THING TOGETHER.
//
//  1. EVERY TOKEN-GENERATING CALL IS WATCHED. Not the calls the router had an
//     opinion about — every one. A cold ledger has no opinion, which is the
//     state of every model somebody picks after launch, and it is the case that
//     most needs a clock: the reported defect was a three-minute wait with
//     nothing watching it because the race refused a choice that named no
//     alternative. A call with nowhere to go still has a ceiling, still reports
//     and still writes down what it did.
//  2. MANY ARMS, ONE PURSE. A question may become more than two requests. What
//     bounds it is money — [lanes.Budget], asked before every arm — and
//     [maxArms] as the absolute cap on one question, never a boolean that says
//     a rescue has already been spent.
//  3. THE PERSON HEARS ONE VOICE. Every arm streams, and one of them is ever
//     the SPEAKER. The others' deltas are held, and are replayed only if one of
//     them wins — after a plain notice, because text that was on the screen and
//     is being replaced is something a person must be told about rather than
//     left to notice.
//  4. A PIN IS ASKED, NEVER OVERRIDDEN. Where an unpinned call hedges, a pinned
//     one raises the offer in offer.go — and with nobody to ask, it borrows
//     once and says so.
//  5. A HEDGE IS A MEASUREMENT. Every arm is folded back into the ledger
//     whichever way the race went, with one exception: an act taken when
//     NOTHING at all had arrived charges nothing to the lane's belief, because
//     nothing about that lane was ever observed.
//
// WHAT IS NOT HERE, DELIBERATELY. The continuation hedge — carrying the partial
// answer to the alternative as a prefill so it continues rather than restarts —
// is out of this version. It is safe only for plain text (a tool call split
// across two lanes is a bug), it needs a runtime check that the continuation
// does not repeat the partial, and it changes what the person reads.
// [hedgeRace.flip] is the seam it would land at.

// maxArms is how many requests one question may ever become: the original and
// three rescues.
//
// IT IS THE LADDER'S SECOND RUNG, BOUNDED. A stall is answered by asking
// another lane; a rescue that is itself refused walks to the next gate-passing
// lane in frontier order, because a lane refusing a request is a fact about
// that lane and not about the model — and relaxing the request, or changing the
// model, before the model's own remaining machines have been tried is answering
// a different question from the one somebody asked (the ladder, in
// docs/ARCHITECTURE.md). Four is where it stops: past three failed lanes the
// evidence is about the model or the request rather than about the endpoints,
// and every step of the walk is budgeted besides.
const maxArms = 4

// heldEvents bounds what is remembered for an arm that is not speaking. A
// silent arm takes the voice the moment it writes a word a person can read, so
// this is reached only when the race has stopped making progress at all; past
// it the person loses some replayed text and never the answer, which is the
// right way round.
const heldEvents = 512

// hedgeNotice is what a person is told when the answer changes lanes mid-flow.
// It is shown only when there was text on the screen to replace: a rescue that
// fires before the first token replaces nothing and says nothing.
const hedgeNotice = "that lane went quiet — this answer is coming from another one"

// waitNow is the clock the waiting controller runs on, and it is deliberately
// NOT the client's seamed [Client.clock].
//
// That seam exists so a test can state a two-second first token without waiting
// two seconds, and what it usually holds is a scripted list of instants. The
// controller is not a measurement: it is an account of how long a PERSON has
// been waiting, and a policy that read a scripted clock would decide the shape
// of a wait nobody was having — while spending, out of the read loop, the ticks
// a measurement under test was counting. This is the same argument
// [logNow] makes about the model-call log, and the two are deliberately the
// same shape.
func waitNow() time.Time { return time.Now() }

// ── THE RACE ────────────────────────────────────────────────────────────────

// hedgeArm is one request in flight.
type hedgeArm struct {
	index  int
	lane   string
	cancel context.CancelFunc
	watch  *streamWatch
}

// armResult is how an arm ended.
type armResult struct {
	index     int
	response  *ai.Response
	relearned bool
	err       error
}

// hedgeRace is one question, and every request it becomes.
type hedgeRace struct {
	client   *Client
	build    control.Factory
	plan     control.Plan
	choice   lanes.Choice
	model    string
	expected int
	session  string
	observer StreamObserver
	report   *HedgeReport
	phase    *phaseClock
	budget   *lanes.Budget
	base     context.Context
	messages []ai.Message
	options  []ai.Option
	results  chan armResult
	// wake is how a controller whose deadline has moved tells the beat to sleep
	// for a different length of time. It has room for one signal because one is
	// all a re-arm ever needs.
	wake chan struct{}

	mu sync.Mutex
	// arms are the requests in flight, primary first.
	arms []*hedgeArm
	// speaker is the arm the person is hearing, winner the arm that took the
	// answer (-1 until one does), and spoken whether any text has been shown.
	speaker int
	winner  int
	spoken  bool
	started bool
	// held is what each arm that is not speaking has produced, replayed if it
	// wins and dropped if it does not. IT IS PER ARM because the walk below can
	// have two silent arms at once, and one list would interleave two answers
	// and replay the mixture.
	held map[int][]StreamEvent
	// tried is every lane this request has already been sent to, so the walk
	// never asks the same machine twice.
	tried map[string]bool
	// refused is set the moment the purse says no. A REFUSAL IS FINAL for this
	// question: a race that re-asked on the next delta would turn one refusal
	// into a poll.
	refused bool
	// asked is the offer this request raised, empty until a pinned lane stalls
	// and once only — a second question about one answer is nagging.
	asked  string
	asking bool
	// reported is whether the wait has already been said out loud. The HUD says
	// "all lanes slow · still waiting" once and then lets the phase clock's own
	// beat carry it.
	reported bool
	// note is the one sentence this question needs the row to carry, and it is
	// empty on all but the handful that decide something with nobody watching.
	note    string
	decided chan struct{}
}

// raceFor builds the watched call, which is every call.
//
// EVERY GATE IS HERE AND NOWHERE ELSE, and there are only two of them. An arm
// of an existing race is never itself raced — that is what the watch already in
// the context means — and a build with no controller installed runs the bare
// stream loop, which is the legal empty state `internal/lane`'s seam documents
// and `law_test.go` says a shipped build is not.
//
// THERE IS NO GATE ON THE CHOICE, and its absence is the fix. The old one asked
// whether the router had named an alternative, so a cold ledger — the state of
// every model somebody picks after launch — produced no routing opinion AND no
// clock. Routing and waiting are two questions: this call still has a ceiling,
// still reports, and still writes down what it did.
func (c *Client) raceFor(ctx context.Context, observer StreamObserver, build control.Factory) (*hedgeRace, bool) {
	if build == nil || streamWatchFrom(ctx) != nil {
		return nil, false
	}
	choice, _ := laneChoiceFromContext(ctx)
	race := &hedgeRace{
		client:   c,
		build:    build,
		choice:   choice,
		model:    strings.TrimSpace(c.config.Model),
		expected: expectedAnswerFrom(ctx),
		session:  streamSessionFrom(ctx),
		observer: observer,
		report:   HedgeReportFrom(ctx),
		phase:    phaseClockFrom(ctx),
		budget:   currentHedgeBudget(),
		speaker:  0,
		winner:   -1,
		held:     map[int][]StreamEvent{},
		tried:    map[string]bool{},
		results:  make(chan armResult, maxArms),
		wake:     make(chan struct{}, 1),
		decided:  make(chan struct{}),
	}
	if head := lanes.HeadOf(choice); head != "" {
		race.tried[strings.ToLower(head)] = true
	}
	race.plan = c.planFor(ctx, choice, race.model, race.expected)
	return race, true
}

// ── RUNNING IT ──────────────────────────────────────────────────────────────

// run sends the request, watches it, and returns whichever arm won.
func (r *hedgeRace) run(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, bool, error) {
	ctx, stopAll := context.WithCancel(ctx)
	defer stopAll()
	r.base, r.messages, r.options = ctx, messages, options

	beat := make(chan struct{})
	defer close(beat)
	r.start(0, "")
	guard.Go("provider.hedge.silence", func() { r.beat(beat) })

	decided := r.decided
	seen := map[int]armResult{}
	for {
		select {
		case <-decided:
			decided = nil
			if result, ok := seen[r.won()]; ok {
				return r.settle(result, seen)
			}
		case result := <-r.results:
			seen[result.index] = result
			if result.err != nil {
				// THE VOICE MOVES OFF A DEAD ARM. An arm that has failed will
				// never speak again, and leaving it as the speaker holds every
				// other arm's text unreplayed until one of them finishes —
				// which is a person watching nothing while an answer arrives.
				r.passVoice(seen)
				// A RESCUE THAT WAS ITSELF REFUSED WALKS ON. The primary may
				// still be stalled with nothing on the screen, and the next
				// machine behind this model is a cheaper answer than relaxing
				// the request or changing the model would be.
				r.walk(result.index)
			}
			if result.err == nil {
				// FINISHING IS COMMITTING. An arm that reached the end of its
				// stream has the whole answer, whatever its token count said.
				r.commit(result.index)
			}
			if won := r.won(); won >= 0 {
				if result, ok := seen[won]; ok {
					return r.settle(result, seen)
				}
				continue
			}
			if len(seen) == r.count() {
				// Nobody committed and everybody is done, which means every arm
				// failed. The primary's failure is the one to report: it is the
				// request the caller actually made.
				if primary, ok := seen[0]; ok {
					return r.settle(primary, seen)
				}
				return r.settle(result, seen)
			}
		}
	}
}

// start puts one arm in flight. lane is empty for the primary and names the
// machine for a rescue, which demands it outright.
func (r *hedgeRace) start(index int, lane string) {
	armCtx, cancel := context.WithCancel(r.base)
	now := waitNow()
	plan := r.plan
	plan.Began = now
	if lane != "" {
		plan.Lane = lane
		// A rescue inherits the errand and its ceiling, and it is a rescue
		// rather than a pin: the person's own machine is the one it is leaving.
		plan.Pinned = false
	}
	plan.Alts = r.untriedAlts()
	watch := &streamWatch{race: r, arm: index, control: r.build(plan), began: now}
	watch.deadline = watch.control.Deadline()
	if !watch.deadline.IsZero() {
		watch.armed = watch.deadline.Sub(now)
	}
	arm := &hedgeArm{index: index, lane: lane, cancel: cancel, watch: watch}
	armCtx = withStreamWatch(armCtx, arm.watch)
	if lane != "" {
		armCtx = withHedgeLane(armCtx, lane)
	}
	r.mu.Lock()
	r.arms = append(r.arms, arm)
	r.mu.Unlock()
	r.rearm()

	observer := r.observerFor(index)
	guard.Go("provider.hedge.arm", func() {
		response, relearned, err := r.client.completeWithMessagesStreaming(armCtx, observer, r.messages, r.options...)
		cancel()
		r.results <- armResult{index: index, response: response, relearned: relearned, err: err}
	})
}

// untriedAlts is where an arm started now could go: the plan's alternatives
// minus the machines this question has already been sent to.
func (r *hedgeRace) untriedAlts() []control.Alternative {
	r.mu.Lock()
	defer r.mu.Unlock()
	alts := make([]control.Alternative, 0, len(r.plan.Alts))
	for _, alt := range r.plan.Alts {
		if !r.tried[strings.ToLower(alt.Lane)] {
			alts = append(alts, alt)
		}
	}
	return alts
}

// beat is the silence half of every arm's controller: one goroutine per
// question, sleeping until the next moment worth waking for.
//
// IT ARMS A TIMER RATHER THAN POLLING, which is what [control.Controller.Deadline]
// exists for. A ticker would wake sixty times for every deadline it is watching
// and would still be wrong about a lane whose first token is expected in four
// hundred milliseconds. The floor under a wake is what stops a controller with
// a deadline in the past from spinning.
func (r *hedgeRace) beat(stop <-chan struct{}) {
	const minWake = 2 * time.Millisecond
	for {
		wake, watching := r.nextWake()
		var alarm <-chan time.Time
		if watching {
			sleep := wake.Sub(waitNow())
			if sleep < minWake {
				sleep = minWake
			}
			timer := time.NewTimer(sleep)
			alarm = timer.C
			defer timer.Stop()
		}
		select {
		case <-stop:
			return
		case <-r.base.Done():
			return
		case <-r.wake:
		case now := <-alarm:
			r.quietAll(now)
		}
	}
}

// nextWake is the earliest moment any arm wants waking at, and false when
// nothing is scheduled at all.
func (r *hedgeRace) nextWake() (time.Time, bool) {
	r.mu.Lock()
	arms := append([]*hedgeArm(nil), r.arms...)
	done := r.winner >= 0
	r.mu.Unlock()
	if done {
		return time.Time{}, false
	}
	var next time.Time
	for _, arm := range arms {
		arm.watch.mu.Lock()
		at := arm.watch.deadline
		arm.watch.mu.Unlock()
		if at.IsZero() {
			continue
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	return next, !next.IsZero()
}

// rearm tells the beat that a deadline has moved. It never blocks: one pending
// signal is all a re-arm ever needs, because the beat recomputes the earliest
// deadline from scratch when it wakes.
func (r *hedgeRace) rearm() {
	if r == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// quietAll asks every arm's controller what it makes of the silence.
func (r *hedgeRace) quietAll(now time.Time) {
	r.mu.Lock()
	arms := append([]*hedgeArm(nil), r.arms...)
	done := r.winner >= 0
	r.mu.Unlock()
	if done {
		return
	}
	for _, arm := range arms {
		arm.watch.quiet(now)
	}
}

// ── WHAT IS DONE ABOUT A WAIT ───────────────────────────────────────────────

// act is the one door every verdict comes through, so that the ladder's order
// and the purse are impossible to route around.
func (r *hedgeRace) act(from int, act control.Act) {
	if r == nil {
		return
	}
	switch act.Kind {
	case control.Hedge:
		r.hedge(from, act, act.Lane)
	case control.Ask:
		r.ask(from, act)
	case control.Report:
		// THE CONTROLLER DECIDES AND THE WIRE OBEYS. A report is a report: that
		// a wait reaching the ceiling with an affordable alternative in hand is
		// a rescue rather than a report is `internal/lane/control`'s ruling and
		// is taken there, so the act that arrives here is the act that happened.
		// Rewriting a verdict at this layer would put a request on the wire that
		// the row still called a report.
		r.tellTheWait(act)
	case control.Escalate:
		// THE CONTROLLER NEVER CHANGES A MODEL. Rung four of the ladder is
		// endpoints.go's and there is exactly one of it: what an escalate says
		// is that this question's own rungs are spent, which the arm's ordinary
		// end then hands upward. All that is done here is to say so.
		r.tellTheWait(act)
	case control.Commit:
		r.commit(from)
	}
}

// hedge is another arm: budgeted, to a machine nobody has asked yet, and never
// past [maxArms].
func (r *hedgeRace) hedge(from int, act control.Act, alt string) {
	alt = r.claim(alt, false)
	if alt == "" {
		return
	}
	if !r.budget.Allow(waitNow(), r.estimate(alt, r.expected)) {
		r.mu.Lock()
		r.refused = true
		delete(r.tried, strings.ToLower(alt))
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	index := len(r.arms)
	primary := r.armAt(from)
	r.mu.Unlock()
	r.report.note(func(report *HedgeReport) {
		report.hedged, report.reason = true, act.Reason
		report.fault = primary.watch.fault
		if report.action == "" {
			report.action, report.silence = actionWord(act.Kind), act.Silence
		}
	})
	// AND WHOEVER IS DRAWING THIS IS TOLD NOW, not at the end. A rescue that is
	// only reported once it has landed is a rescue a person watched as an
	// unexplained pause; the one sentence this build says about a slow answer is
	// said while something is already being done about it (internal/tui3's
	// laneRider).
	r.report.started(alt)
	// AND THE CLOCK SAYS SO IN THE SAME BREATH. "stalled 9s · switching to
	// parasail" is one sentence: the first half is why, and a person shown only
	// the second half would not know what it was about (phase.go).
	r.phase.switching(strings.ToLower(alt), primary.watch.quietFor(waitNow()))
	r.start(index, alt)
}

// claim reserves the machine a rescue would go to, and answers empty when there
// is no room, no money or nowhere left to go.
//
// IT IS ONE FUNCTION SO THAT THE GATES CANNOT DRIFT. Every act that puts a
// request on the wire — the controller's hedge, the answered offer, the walk
// past a refusal — asks exactly this question, and it is asked under the lock
// so that two arms deciding at once cannot both take the last machine.
func (r *hedgeRace) claim(preferred string, past bool) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.winner >= 0 || len(r.arms) >= maxArms {
		return ""
	}
	// past is the walk, and only the walk: a lane that FAILED must not be left
	// unanswered because a lane that was merely SLOW had its rescue refused a
	// moment earlier. Every other caller stops at a refusal, which is what
	// keeps one refusal from becoming a poll.
	if r.refused && !past {
		return ""
	}
	if lane := strings.TrimSpace(preferred); lane != "" && !r.tried[strings.ToLower(lane)] {
		r.tried[strings.ToLower(lane)] = true
		return lane
	}
	for _, alt := range r.plan.Alts {
		if lane := strings.TrimSpace(alt.Lane); lane != "" && !r.tried[strings.ToLower(lane)] {
			r.tried[strings.ToLower(lane)] = true
			return lane
		}
	}
	return ""
}

// ask is a pinned lane's rescue: the person who named the machine is asked
// rather than overridden (offer.go).
//
// WITH NOBODY TO ASK IT BORROWS, ONCE, AND SAYS SO. "No reader" is
// [OnPhase] having no registered reader — the same seam the surface uses, so
// the two ideas of headless cannot drift apart — and a run that waited forever
// on a machine that had gone quiet would be the reported defect at its worst,
// because nobody is watching to notice.
func (r *hedgeRace) ask(from int, act control.Act) {
	r.mu.Lock()
	already, pinned := r.asking, r.plan.Lane
	r.asking = true
	r.mu.Unlock()
	if already {
		return
	}
	alt, affordable := r.affordableAlt()
	if alt == "" || !affordable {
		// Nothing to offer is nothing to ask about, and the wait is real: say
		// so instead.
		r.tellTheWait(act)
		return
	}
	if !phaseListening() {
		r.mu.Lock()
		r.note = "pinned lane " + pinned + " was silent for " + quietWords(act.Silence) +
			" — borrowing " + alt + " for this answer"
		r.mu.Unlock()
		r.report.note(func(report *HedgeReport) {
			if report.action == "" {
				report.action, report.silence = "borrow", act.Silence
			}
		})
		r.hedge(from, act, alt)
		return
	}
	token := raiseOffer(pinned, alt, waitNow(), func() { r.hedge(from, act, alt) })
	r.mu.Lock()
	r.asked = token
	r.mu.Unlock()
	r.report.note(func(report *HedgeReport) {
		if report.action == "" {
			report.action, report.silence, report.reason = actionWord(act.Kind), act.Silence, act.Reason
		}
	})
	r.phase.asking(pinned, token)
}

// withdraw takes the offer down. The pin came good — or the request ended — and
// a question about an answer that has arrived is a question about nothing.
func (r *hedgeRace) withdraw() {
	r.mu.Lock()
	token := r.asked
	r.asked = ""
	r.mu.Unlock()
	if token == "" {
		return
	}
	withdrawOffer(token)
	r.phase.withdrew()
}

// tellTheWait is the visible half of [control.Report]: there is nowhere better
// to go and the wait is real. Saying nothing was the old behaviour and it is
// the one thing this design will not do.
func (r *hedgeRace) tellTheWait(act control.Act) {
	r.mu.Lock()
	already := r.reported
	r.reported = true
	r.mu.Unlock()
	if already {
		return
	}
	r.report.note(func(report *HedgeReport) {
		if report.action == "" {
			report.action, report.silence, report.reason = actionWord(act.Kind), act.Silence, act.Reason
		}
	})
	r.phase.allSlow(r.waitWords(act))
}

// waitWords is what a person is told when nothing can be done.
//
// THE THREE SENTENCES ARE THREE DIFFERENT FACTS. Every machine behind this
// model has been asked and none answered; every one of them was weighed and
// none is believed better than the one already running; or there was never more
// than one machine to begin with, which is what a call to an endpoint that is
// not a router looks like. Saying "all lanes slow" about a request that had one
// lane would be inventing a comparison nobody made.
func (r *hedgeRace) waitWords(act control.Act) string {
	// THE SURFACE OWNS THE SENTENCE and this owns only the exception to it.
	// `all lanes slow · still waiting` is one line, spelled in `internal/tui3`
	// where the words a person reads live, and a machine word posted beside it
	// would be the same fact said twice. What this layer knows that the surface
	// cannot is that the ladder itself is spent — every lane of this model has
	// been asked — and that is a different sentence.
	if act.Kind == control.Escalate {
		return "every lane tried"
	}
	return ""
}

// quietWords is a silence as a person says it.
func quietWords(quiet time.Duration) string {
	if quiet <= 0 {
		return "0s"
	}
	if quiet < time.Second {
		return "under a second"
	}
	return strconv.Itoa(int(quiet.Round(time.Second)/time.Second)) + "s"
}

// affordableAlt is the best machine a rescue could go to and whether the purse
// would allow it. Both halves are needed together: a countdown drawn over a
// rescue nobody can afford is the surface lying about the machinery.
func (r *hedgeRace) affordableAlt() (string, bool) {
	r.mu.Lock()
	alt := ""
	for _, candidate := range r.plan.Alts {
		if lane := strings.TrimSpace(candidate.Lane); lane != "" && !r.tried[strings.ToLower(lane)] {
			alt = lane
			break
		}
	}
	refused, full := r.refused, len(r.arms) >= maxArms
	r.mu.Unlock()
	if alt == "" || refused || full {
		return "", false
	}
	return alt, r.budget.Affordable(waitNow(), r.estimate(alt, r.expected))
}

// walk is the second rung: the rescue we sent was refused or broke, nobody has
// committed, and there is another machine behind this model that the frontier
// says is worth asking.
//
// IT IS NOT A SECOND HEDGE. A hedge is a bet against SLOWNESS and it is fired
// by the controller; this is a response to a lane that has FAILED, and the
// request it replaces is already gone. That is why it is not gated by the
// purse's earlier refusal — one refusal must not spend the rescue a slow lane
// is still owed — and why it is gated by the budget and by [maxArms] instead.
//
// THE PRIMARY IS NEVER WALKED FROM. An arm that is still streaming has not
// failed, and the controller is the only thing allowed to give up on it.
func (r *hedgeRace) walk(from int) {
	if r == nil || r.base == nil || r.base.Err() != nil {
		return
	}
	alt := r.claim("", true)
	if alt == "" {
		return
	}
	r.mu.Lock()
	index := len(r.arms)
	r.mu.Unlock()
	if !r.budget.Allow(waitNow(), r.estimate(alt, r.expected)) {
		return
	}
	r.report.started(alt)
	r.phase.switching(strings.ToLower(alt), "")
	r.start(index, alt)
}

// ── THE ONE VOICE ───────────────────────────────────────────────────────────

// voice hands the person's ear to the first arm that writes something they can
// read, and withdraws any question that was open about the wait.
//
// A THINKING DELTA DOES NEITHER. Nothing has arrived that anybody can read, so
// the arm has not earned the voice and the offer has not been answered by the
// machine it was about.
func (r *hedgeRace) voice(arm int) {
	if r == nil {
		return
	}
	r.withdraw()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.winner >= 0 || r.speaker == arm {
		return
	}
	if speaker := r.armAt(r.speaker); speaker != nil && speaker.watch.written() > 0 && r.spoken {
		// Somebody is already being read. The one-voice rule holds and this
		// arm's text stays held until it wins outright.
		return
	}
	r.flip(arm)
}

// armAt is the arm with this index, nil when there is none. It runs with the
// lock held.
func (r *hedgeRace) armAt(index int) *hedgeArm {
	for _, arm := range r.arms {
		if arm.index == index {
			return arm
		}
	}
	return nil
}

// passVoice hands the person's ear to an arm that is still alive, when the one
// they were listening to has failed. It is a no-op while the speaker is still
// running and while there is nobody else.
func (r *hedgeRace) passVoice(seen map[int]armResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.winner >= 0 {
		return
	}
	if done, ok := seen[r.speaker]; !ok || done.err == nil {
		return
	}
	for _, arm := range r.arms {
		if _, ended := seen[arm.index]; ended || arm.index == r.speaker {
			continue
		}
		r.flip(arm.index)
		return
	}
}

// commit hands the answer to one arm and cancels the others. It is idempotent:
// the first caller wins and every later one is a no-op.
//
// CANCELLING IS WHAT STOPS THE BILL on the lanes that honour it, and it is the
// second half of the same inequality: past the point where finishing here costs
// less than starting again, whatever else is in flight is money for nothing.
func (r *hedgeRace) commit(arm int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.winner >= 0 {
		r.mu.Unlock()
		return
	}
	r.winner = arm
	r.flip(arm)
	losers := make([]context.CancelFunc, 0, len(r.arms))
	for _, one := range r.arms {
		if one.index != arm {
			losers = append(losers, one.cancel)
		}
	}
	close(r.decided)
	r.mu.Unlock()
	r.withdraw()
	// Outside the lock, because a cancel wakes the loser's read loop, which
	// will want this lock on its way out.
	for _, cancel := range losers {
		cancel()
	}
}

// flip changes who the person is hearing. It runs with the lock held.
func (r *hedgeRace) flip(to int) {
	if r.speaker == to {
		return
	}
	r.speaker = to
	if r.spoken {
		// There was text on the screen and it is being replaced. A person is
		// told that in the one channel that reaches the room in order.
		r.observer(StreamEvent{Kind: StreamNotice, Delta: hedgeNotice, Session: r.session})
	}
	held := r.held[to]
	// EVERY OTHER ARM'S HELD TEXT IS DROPPED HERE AND NOT LATER. It is an
	// answer nobody is going to read, and keeping it would let a third arm
	// replay it after this one had already spoken.
	r.held = map[int][]StreamEvent{}
	for _, event := range held {
		if event.Kind == StreamStarted {
			continue
		}
		if event.Kind == StreamDelta {
			r.spoken = true
		}
		r.observer(event)
	}
}

// observerFor is one arm's door to the person.
func (r *hedgeRace) observerFor(arm int) StreamObserver {
	return func(event StreamEvent) { r.emit(arm, event) }
}

// emit is where the one-voice rule lives.
//
// The lock is held across the caller's observer on purpose: the arms are
// separate goroutines and the events a person reads must stay in the order they
// were produced. The observer is documented as trivial — the one live consumer
// hands the event to a buffered channel — and this is the second place that
// contract is load-bearing.
func (r *hedgeRace) emit(arm int, event StreamEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch event.Kind {
	case StreamStarted:
		// One turn, one beginning, whichever arm opened first.
		if r.started {
			return
		}
		r.started = true
	case StreamFinished:
		// A clean end from whoever the person is hearing ends the turn. An arm
		// that is not the speaker has either lost the race or not yet won it,
		// and in both cases the answer being read is somebody else's.
		if arm != r.speaker {
			return
		}
	case StreamFailed:
		// A LOSER THAT FAILS MUST NOT END A TURN THAT IS STILL BEING ANSWERED.
		// While a second arm is in flight the failure of one of them is an
		// event about the race, which the person is told about only if it ends
		// with no answer at all.
		if arm != r.speaker || (len(r.arms) > 1 && r.winner != arm) {
			return
		}
	}
	if arm != r.speaker {
		if len(r.held[arm]) < heldEvents {
			r.held[arm] = append(r.held[arm], event)
		}
		return
	}
	if event.Kind == StreamDelta {
		r.spoken = true
	}
	r.observer(event)
}

// hasUntriedLane reports whether the frontier still holds a gate-passing lane
// this request has not been sent to, and there is room to send one.
func (r *hedgeRace) hasUntriedLane() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.winner >= 0 || len(r.arms) >= maxArms {
		return false
	}
	for _, alt := range r.plan.Alts {
		if lane := strings.TrimSpace(alt.Lane); lane != "" && !r.tried[strings.ToLower(lane)] {
			return true
		}
	}
	return false
}

// hears reports whether this arm is the one the person is listening to.
func (r *hedgeRace) hears(arm int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.speaker == arm
}

// won is the arm that took the answer, -1 while the race is open.
func (r *hedgeRace) won() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.winner
}

// count is how many arms are in flight.
func (r *hedgeRace) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.arms)
}

// asked is the machine the preference NAMED for one arm: the lane a rescue
// demanded, or the head of the order for the request the caller made.
func (r *hedgeRace) askedLane(arm int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if one := r.armAt(arm); one != nil && one.lane != "" {
		return one.lane
	}
	return r.plan.Lane
}

// spend is how many requests this question became, what the OTHER arms have
// cost so far, and the one sentence the row has to carry.
//
// IT IS COMPUTED LIVE AND IT IS AN ESTIMATE, because a row is written when its
// own stream ends and the arms beside it may still be running. From the row of
// the arm that answered, "the others" is exactly the waste; from the row of one
// that did not, it is what the rest of the question cost. Both are the figure a
// person reading one line wants, and neither can be the router's own: a
// cancelled stream never sends a usage frame.
func (r *hedgeRace) spend(arm int) (arms int, waste float64, note string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, other := range r.arms {
		if other.index != arm {
			waste += r.estimateLocked(laneOf(other), other.watch.written())
		}
	}
	return len(r.arms), waste, r.note
}

// settle writes the ledger and the report, and hands back the winner's answer.
func (r *hedgeRace) settle(result armResult, seen map[int]armResult) (*ai.Response, bool, error) {
	r.withdraw()
	r.mu.Lock()
	arms := append([]*hedgeArm(nil), r.arms...)
	hedged := len(arms) > 1
	model := r.model
	r.mu.Unlock()
	// The model as the ANSWER spelled it, because a router may pin a variant
	// the config never named and a belief keyed on the wrong id is a belief
	// about nothing.
	if result.response != nil && strings.TrimSpace(result.response.Model) != "" {
		model = strings.TrimSpace(result.response.Model)
	}

	for _, arm := range arms {
		// THE ANSWER THAT WAS USED IS NOTED ONCE, BY THE ORDINARY PATH. Every
		// finished stream already reaches the belief through
		// [Client.noteVelocity] — that is how a machine with no sheet learns
		// anything at all — and the winner of a race is a finished stream like
		// any other. Noting it here too gave a raced lane TWO sightings of one
		// answer, which is a lane believed twice as measured as it is and, worse,
		// a lane whose belief moves twice as fast for having been in a race. So
		// this loop is about the LOSERS, which are the streams no other path can
		// see.
		if arm.index == result.index {
			continue
		}
		tokens := 0
		if done, ok := seen[arm.index]; ok && done.response != nil && done.response.Usage != nil {
			tokens = done.response.Usage.CompletionTokens
		}
		// A HEDGE IS A MEASUREMENT, whichever way it landed: the loser's
		// first-token wait is the only cheap evidence there is about the lane
		// nobody chose. A path fault and an anonymous stream are the two things
		// [streamWatch.sighting] refuses, and both refusals are the ledger
		// staying honest rather than an omission.
		if sighting, ok := arm.watch.sighting(model, tokens); ok {
			lanes.Default().Ledger().Note(sighting)
		}
	}
	// The two denominators of the budget: every dollar this adapter is seen to
	// spend, and every request it is seen to make. Both are noted on every
	// watched call, hedged or not — the rate limit is counted in REQUESTS rather
	// than in minutes, so a request that never reports leaves the allowance
	// looking emptier than it is.
	now := waitNow()
	r.budget.NoteRequest(now)
	r.budget.NoteSpend(r.spent(seen), now)
	winner, loser, primary := "", "", ""
	var waste, second float64
	for _, arm := range arms {
		if arm.index == result.index {
			winner = laneOf(arm)
		} else {
			// THE LOSER NAMED IS THE LAST ONE ASKED and the waste is EVERY arm
			// that did not answer. One name cannot describe a walk of three,
			// and the row would rather carry the nearest miss than an invented
			// list; the money, though, is the sum, because a waste column that
			// reported one of three cancelled arms would understate what the
			// rescue cost by exactly the amount that makes it worth knowing.
			loser = laneOf(arm)
			waste += r.estimate(loser, arm.watch.written())
		}
		if arm.index == 0 {
			primary = laneOf(arm)
		}
		if arm.index > 0 {
			// EVERY RESCUE IS CHARGED, not only the first one. The walk can put
			// a third and a fourth request on the wire, and a budget that only
			// ever saw the second would let a walk spend the session's whole
			// share while believing it had spent one arm's.
			second += r.armCost(seen, arm)
		}
	}
	if hedged {
		r.budget.NoteHedge(second, now)
	}
	r.report.note(func(report *HedgeReport) {
		report.winner, report.loser, report.primary = winner, loser, primary
		report.waste, report.arms = waste, len(arms)
	})
	return result.response, result.relearned, result.err
}

// armCost is what one arm cost: the router's own figure when that arm finished
// and sent a usage frame, and the estimate when it was cancelled before one.
func (r *hedgeRace) armCost(seen map[int]armResult, arm *hedgeArm) float64 {
	if done, ok := seen[arm.index]; ok && done.response != nil && done.response.Usage != nil && done.response.Usage.Cost != nil {
		return *done.response.Usage.Cost
	}
	return r.estimate(laneOf(arm), arm.watch.written())
}

// laneOf is which lane an arm was served by, falling back to the lane it
// DEMANDED when no chunk ever named one.
//
// The fallback is exact rather than a guess: a rescue sends `only`, so an arm
// with a demanded lane went there or nowhere. The primary has no fallback,
// because it sends an ORDER and the router is free to honour any of it — and a
// stream that was cancelled before it said who was answering is a stream nobody
// may put a name to. That is the same attribution law the ledger keeps, and it
// is why a rescued-before-the-first-token request reports no loser.
func laneOf(arm *hedgeArm) string {
	if served := arm.watch.lane(); served != "" {
		return served
	}
	return strings.TrimSpace(arm.lane)
}

// spent is what this race actually billed, by the router's own figure where
// there is one. A cancelled arm sends no usage frame, so what is counted is the
// answer that arrived plus the estimate of what was thrown away.
func (r *hedgeRace) spent(seen map[int]armResult) float64 {
	var total float64
	for _, result := range seen {
		if result.response != nil && result.response.Usage != nil && result.response.Usage.Cost != nil {
			total += *result.response.Usage.Cost
		}
	}
	return total
}

// estimate is what a request of this many tokens is expected to cost on one
// lane, from the numbers the choice was made on.
//
// It is the frontier's own figure scaled by how much of the answer arrived,
// because that is the only price anybody here has: the choice priced the whole
// request on every candidate lane, and a stream cancelled a third of the way
// through cost about a third of it. It is never billed and never shown as
// money — it feeds the purse's share and the log's waste column, both of which
// are about proportions.
func (r *hedgeRace) estimate(lane string, tokens int) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.estimateLocked(lane, tokens)
}

// estimateLocked is the same arithmetic for a caller that already holds the
// lock. It reads only the choice, which never changes after the race is built.
func (r *hedgeRace) estimateLocked(lane string, tokens int) float64 {
	lane = strings.TrimSpace(lane)
	if lane == "" {
		return 0
	}
	for _, scored := range r.choice.Frontier {
		if !equalLane(scored.ID.Lane, lane) {
			continue
		}
		if r.expected <= 0 || tokens <= 0 || tokens >= r.expected {
			return scored.Price
		}
		return scored.Price * float64(tokens) / float64(r.expected)
	}
	return 0
}

// ── THE LANE A RESCUE DEMANDS ───────────────────────────────────────────────

type hedgeLaneContextKey struct{}

// withHedgeLane marks a request as a rescue, bound to exactly one lane.
func withHedgeLane(ctx context.Context, lane string) context.Context {
	return context.WithValue(ctx, hedgeLaneContextKey{}, lane)
}

// hedgeLaneFrom is the lane this request must go to, empty on every ordinary
// call.
func hedgeLaneFrom(ctx context.Context) string {
	lane, _ := ctx.Value(hedgeLaneContextKey{}).(string)
	return lane
}

// hedgePreference puts the rescue's demand on the wire.
//
// A RESCUE NAMES ITS LANE AND TAKES NO FALLBACK. The whole point of the second
// request is that it goes somewhere else; a router free to fall back could
// answer it from the lane that is already stalling, and the race would be two
// requests to the same machine. `only` is the field that says so, and the
// preference is otherwise left exactly as the encoder built it.
func hedgePreference(prefs *providerPrefs, knobs callKnobs) *providerPrefs {
	if knobs.hedgeLane == "" {
		return prefs
	}
	no := false
	if prefs == nil {
		return &providerPrefs{Only: []string{knobs.hedgeLane}, AllowFallbacks: &no}
	}
	hedged := *prefs
	hedged.Only = []string{knobs.hedgeLane}
	hedged.Order = nil
	hedged.AllowFallbacks = &no
	return &hedged
}
