package provider

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE HEDGE: THE TRANSPORT HALF OF THE WATCH ──────────────────────────────
//
// `internal/lane`'s watch says WHEN a stream has become worth giving up on.
// This file is what happens next: one more request to the alternative lane, on
// a child context, with the first of the two to commit winning and the other
// cancelled — because cancelling is what stops the bill on the twenty-odd lanes
// that honour it.
//
// FOUR RULES HOLD THE WHOLE THING TOGETHER.
//
//  1. A REQUEST WITH NO LANE CHOICE IS UNTOUCHED. No choice in the context, no
//     chooser wired into the registry, or `routing: off` — and this file does
//     nothing at all: [Client.completeWithMessagesStreaming] runs exactly the
//     bytes it ran before any of this existed. That is what the "no choice"
//     test in hedge_test.go pins, and it is why the race is a wrapper rather
//     than a set of branches inside the read loop.
//  2. ONE HEDGE, ONCE, AND ONLY WITH A BUDGET. The watch fires at most one
//     verdict; the budget can still refuse it, and when it does, the answer
//     stays where it is rather than being asked for again later.
//  3. THE PERSON HEARS ONE VOICE. Both arms stream, but only one of them is
//     ever the SPEAKER. The other's deltas are held, and are replayed only if
//     it wins — after a plain notice, because text that was on the screen and
//     is now being replaced is something a person must be told about rather
//     than left to notice.
//  4. A HEDGE IS A MEASUREMENT. Both arms are folded back into the lane ledger
//     whichever way the race went, with one exception: a verdict that was about
//     the PATH (no heartbeat, no byte) charges nothing to the lane's belief,
//     because nothing about that lane was ever observed.
//
// WHAT IS NOT HERE, DELIBERATELY. The continuation hedge — carrying the
// partial answer to the alternative as a prefill so it continues rather than
// restarts — is out of this version. It is safe only for plain text (a tool
// call split across two lanes is a bug), it needs a runtime check that the
// continuation does not repeat the partial, and it changes what the person
// reads. [hedgeRace.flip] is the seam it would land at: everything it needs is
// the held prefix and the winner's first twenty characters.

// hedgeCommit is how many tokens an arm must produce before it is trusted with
// the answer. Sixty-four is the design's figure: enough that a lane which
// stalls on its first breath does not win the race by starting, and few enough
// that the person waits a fraction of a second for the decision.
const hedgeCommit = 64

// maxArms is how many requests one question may ever become: the original and
// three rescues.
//
// IT IS THE LADDER'S SECOND RUNG, BOUNDED. A stall is answered by asking the
// alternative lane; a rescue that is itself refused walks to the next
// gate-passing lane in frontier order, because a lane refusing a request is a
// fact about that lane and not about the model — and relaxing the request, or
// changing the model, before the model's own remaining machines have been tried
// is answering a different question from the one somebody asked (the ladder, in
// docs/ARCHITECTURE.md). Four is where it stops: past three failed lanes the
// evidence is about the model or the request rather than about the endpoints,
// and every step of the walk is budgeted besides.
const maxArms = 4

// heldEvents bounds what is remembered for an arm that is not speaking. A
// silent arm commits at [hedgeCommit] tokens and starts speaking, so this is
// reached only when the race has stopped making progress at all; past it the
// person loses some replayed text and never the answer, which is the right way
// round.
const heldEvents = 512

// hedgeNotice is what a person is told when the answer changes lanes mid-flow.
// It is shown only when there was text on the screen to replace: a hedge that
// fires before the first token replaces nothing and says nothing.
const hedgeNotice = "that lane went quiet — this answer is coming from another one"

// ── WHAT THE LEDGER IS TOLD AFTERWARDS ──────────────────────────────────────

// HedgeReport is what one request's rescue cost and bought, written into the
// caller's own slot exactly like [ServedEndpoint].
//
// It is a slot rather than a field on the response for the reason the served
// endpoint is: the SDK's response type is the OpenAI shape, and hedging is this
// adapter's own bookkeeping. `internal/session`'s usage ledger reads it to
// write `hedged`, `lane` and `hedge_waste_usd` on the row.
type HedgeReport struct {
	mu sync.Mutex
	// hedged is whether a second request went out at all.
	hedged bool
	// winner is the lane whose answer the caller got, loser the lane that was
	// cancelled — both empty when no stream named one.
	winner string
	loser  string
	// reason is the watch's own machine word ("first token late", "no
	// heartbeat", "drift", "gap"). It is for the log and never for a person.
	reason string
	// waste is what the cancelled arm is estimated to have cost. It is an
	// ESTIMATE and says so: a cancelled stream delivers no usage frame, so
	// what is known is how many tokens it had written and what the lane
	// charges for them.
	waste float64
	// primary is the lane the FIRST request was served by, whichever arm went
	// on to win. It is a different fact from the winner and the loser, and it
	// is the one a surface needs: "this answer started on cloudflare and
	// finished on coreweave" cannot be said from a pair whose names swap places
	// depending on who won.
	primary string
	// fault records that the verdict was about the path rather than the lane,
	// which is what keeps the belief out of it.
	fault bool
	// onStart is told the moment a rescue goes out, with the lane it is going
	// to. It is the ONE thing on this slot that is not read afterwards, and it
	// exists because the only interesting state of a rescue is the one that is
	// over before the call returns: while the second request is in flight and
	// nobody has committed, which is the sentence a person reads on the status
	// line. A caller that registers nothing is told nothing, and nothing here
	// waits on it.
	onStart func(alt string)
}

// Hedged reports whether a second request went out.
func (h *HedgeReport) Hedged() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hedged
}

// Lanes are the lane that answered and the lane that was cancelled.
func (h *HedgeReport) Lanes() (winner, loser string) {
	if h == nil {
		return "", ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.winner, h.loser
}

// Primary is the lane the first request was served by, empty when no stream
// named one. See the field for why it is not [HedgeReport.Lanes]'s loser.
func (h *HedgeReport) Primary() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.primary
}

// OnHedgeStart registers what to do the moment a rescue goes out — once, with
// the lane it is going to. It is called from the race's own goroutine and must
// not block; a nil function unregisters.
//
// IT IS SET BEFORE THE CALL AND NEVER DURING ONE. The slot belongs to the
// caller and is stamped on the context before the request goes out
// ([WithHedgeReport]), which is the only moment at which nothing is reading it.
func (h *HedgeReport) OnHedgeStart(fn func(alt string)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onStart = fn
}

// started tells the caller a rescue is in flight, outside the lock so that a
// slow reader cannot stall the race that is trying to rescue an answer.
func (h *HedgeReport) started(alt string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	fn := h.onStart
	h.mu.Unlock()
	if fn != nil {
		fn(alt)
	}
}

// Reason is the watch's machine word for why the hedge went out.
func (h *HedgeReport) Reason() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reason
}

// Waste is the estimated dollars the cancelled arm cost.
func (h *HedgeReport) Waste() float64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.waste
}

// PathFault reports whether the rescue was of a dead path rather than of a slow
// lane. A ledger row that carried this as an ordinary demotion would be
// blaming an endpoint for somebody's network.
func (h *HedgeReport) PathFault() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fault
}

func (h *HedgeReport) note(fn func(*HedgeReport)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(h)
}

type hedgeReportContextKey struct{}

// WithHedgeReport asks the adapter to write down, in the caller's own slot,
// what hedging did to the calls made under ctx.
func WithHedgeReport(ctx context.Context, slot *HedgeReport) context.Context {
	if slot == nil {
		return ctx
	}
	return context.WithValue(ctx, hedgeReportContextKey{}, slot)
}

// HedgeReportFrom returns the slot in force for ctx, nil when none was opened —
// which every method here answers correctly, so a caller never tests.
func HedgeReportFrom(ctx context.Context) *HedgeReport {
	slot, _ := ctx.Value(hedgeReportContextKey{}).(*HedgeReport)
	return slot
}

// ── THE BUDGET THIS PROCESS HEDGES UNDER ────────────────────────────────────

// hedgeBudget is the one budget the whole process hedges under.
//
// It is a package variable rather than a field on the Client because the limit
// it holds is about a SESSION and not about an adapter: two clients — the chat
// and a tool loop's own — must not each be allowed six hedges a minute and a
// tenth of the bill.
var (
	hedgeBudgetMu sync.RWMutex
	hedgeBudget   = lanes.DefaultBudget()
)

// currentHedgeBudget is the budget in force.
func currentHedgeBudget() *lanes.Budget {
	hedgeBudgetMu.RLock()
	defer hedgeBudgetMu.RUnlock()
	return hedgeBudget
}

// SetHedgeBudget replaces the process's hedge budget. A nil argument restores
// the default rather than leaving a hole; a budget of zero hedges a minute is
// how hedging is switched off outright.
func SetHedgeBudget(budget *lanes.Budget) {
	hedgeBudgetMu.Lock()
	defer hedgeBudgetMu.Unlock()
	if budget == nil {
		budget = lanes.DefaultBudget()
	}
	hedgeBudget = budget
}

// ── DRIVING ONE ARM'S WATCH FROM THE READ LOOP ──────────────────────────────

// streamWatch is what the read loop reports to: a lane watch, the arm it
// belongs to, and the lock that makes it safe to drive from the loop and from
// the silence beat at the same time.
//
// It is the ONLY thing the read loop knows about hedging. Every hook in
// client.go is a nil-safe method call on this type, so a call with no choice
// pays one nil check per delta and nothing else.
type streamWatch struct {
	race *hedgeRace
	arm  int
	now  func() time.Time

	mu    sync.Mutex
	watch *lanes.Watch
	// tokens is how many deltas of real progress this arm has delivered, and
	// commitAt is the count at which it takes the answer. Zero is "no
	// commitment point": before a hedge goes out there is no race to win.
	//
	// IT COUNTS EVERY DELTA AND NOT ONLY THE VISIBLE ONES, which is the
	// opposite of the rule the lane watch keeps, and the two answer different
	// questions. The watch asks "is it too late to leave?" — and a run of
	// thought is nothing on the screen, so it buys no commitment there. This
	// asks "which arm is the person hearing?" — and an arm that stalled and has
	// since written sixty-four deltas of anything has RECOVERED, so it keeps the
	// answer rather than being paid for twice while a rescue finishes.
	tokens   int
	commitAt int
	// visible is the same count restricted to tokens a person can read. It is
	// what the lane watch's commitment rule is told.
	visible int
	// served is the lane the stream named, first and first, and began, first
	// and last are the timing this arm's sighting is built from.
	served string
	began  time.Time
	first  time.Time
	last   time.Time
	gap    time.Duration
}

type streamWatchContextKey struct{}

func withStreamWatch(ctx context.Context, watch *streamWatch) context.Context {
	return context.WithValue(ctx, streamWatchContextKey{}, watch)
}

// streamWatchFrom is the watch driving this call, nil on every call that is not
// an arm of a race.
func streamWatchFrom(ctx context.Context) *streamWatch {
	watch, _ := ctx.Value(streamWatchContextKey{}).(*streamWatch)
	return watch
}

// heartbeat is a router comment line: proof about the path, never about the
// endpoint.
func (w *streamWatch) heartbeat() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.watch.Heartbeat(w.now())
}

// serve records who the stream said was answering it, and re-points the watch
// at the machine that is really writing.
//
// A ROUTER HONOURS ANY OF AN ORDER. Until the first chunk names a lane the only
// belief anybody holds is the head of the order's, and every gap after that
// would be judged as a surprise about a machine that was never asked
// ([lane.Watch.Serving]). The ledger read is memory only by its own contract,
// which is what makes it safe on the read loop.
func (w *streamWatch) serve(lane string) {
	if w == nil {
		return
	}
	lane = strings.TrimSpace(lane)
	w.mu.Lock()
	first := w.served == "" && lane != ""
	if first {
		w.served = lane
	}
	model, now := "", w.now()
	if w.race != nil {
		model = w.race.model
	}
	w.mu.Unlock()
	if !first || model == "" {
		return
	}
	belief, ok := lanes.Default().Ledger().Belief(lanes.ID{Model: model, Lane: lane})
	if !ok {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.watch.Serving(lane, belief, now)
}

// token is one delta of real progress — a word, a thought, a fragment of a
// call. It advances the drift test and it is where an arm commits. visible says
// whether this delta was a word a person can read, which is the only count the
// watch's commitment rule may read.
func (w *streamWatch) token(visible bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	now := w.now()
	if w.first.IsZero() {
		w.first = now
	} else if gap := now.Sub(w.last); gap > w.gap {
		w.gap = gap
	}
	w.last = now
	w.tokens++
	if visible {
		w.visible++
	}
	verdict := w.watch.Token(w.tokens, w.visible, now)
	fault := w.watch.PathFault()
	commit := w.commitAt > 0 && w.tokens >= w.commitAt
	arm, race := w.arm, w.race
	w.mu.Unlock()

	if commit {
		race.commit(arm)
	}
	if verdict.Hedge {
		race.hedge(arm, verdict, fault)
	}
}

// silence is the beat: nothing has arrived, and the watch is asked whether that
// has gone on long enough to act on.
func (w *streamWatch) silence(now time.Time) {
	if w == nil {
		return
	}
	w.mu.Lock()
	verdict := w.watch.Silence(now)
	fault := w.watch.PathFault()
	arm, race := w.arm, w.race
	w.mu.Unlock()
	if verdict.Hedge {
		race.hedge(arm, verdict, fault)
	}
}

// commitOn sets this arm's commitment point that many tokens from where it has
// got to, and reports where that was. Before a hedge goes out there is no
// commitment point at all: an unraced stream wins by finishing.
func (w *streamWatch) commitOn(tokens int) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.commitAt = w.tokens + tokens
	return w.tokens
}

// sighting is what this arm measured, and whether it is worth writing down.
//
// A stream that never named a lane is anonymous and is dropped under the
// attribution law: crediting an unnamed measurement to some lane is how a
// ledger learns a fact about a machine that was not involved. A stream whose
// verdict was a path fault is dropped for the other reason — there is no fact
// about the machine in it at all.
func (w *streamWatch) sighting(model string, tokens int) (lanes.Sighting, bool) {
	if w == nil {
		return lanes.Sighting{}, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.served == "" || w.first.IsZero() || w.watch.PathFault() {
		return lanes.Sighting{}, false
	}
	if tokens <= 0 {
		tokens = w.tokens
	}
	return lanes.Sighting{
		ID:     lanes.ID{Model: model, Lane: w.served},
		TTFT:   w.first.Sub(w.began),
		Gen:    w.last.Sub(w.first),
		Gap:    w.gap,
		Tokens: tokens,
		At:     w.last,
	}, true
}

// consequence is the deadline this arm is watched against and the lane a hedge
// would go to, both zero on a call the router is not watching.
//
// IT IS WHAT THE PHASE CLOCK IS ALLOWED TO PROMISE. A countdown on the screen
// has to be a moment at which this build really acts, and this pair is the only
// place in the process where that moment exists.
func (w *streamWatch) consequence() (time.Duration, string) {
	if w == nil {
		return 0, ""
	}
	w.mu.Lock()
	deadline, alt := w.watch.Deadline(), w.watch.Alt()
	race := w.race
	w.mu.Unlock()
	// AND A RESCUE NOBODY CAN AFFORD IS NOT A CONSEQUENCE. The budget is what
	// finally decides whether the second request goes out — a speed guard
	// switched off is a budget of zero — and a countdown drawn over a hedge
	// that was always going to be refused is a countdown that expires and does
	// nothing, which is the one thing the phase clock may never do.
	if race == nil || !race.budget.Affordable(race.now(), race.estimate(alt, race.expected)) {
		return 0, ""
	}
	return deadline, alt
}

// canWalk reports whether this request still has another machine behind the
// same model that it may be sent to.
//
// It is what stops the relax ladder from running too early: a refusal is
// evidence about ONE endpoint, and the ladder's rungs are about the request
// itself. See [Client.sendRecovered] for the whole argument.
func (w *streamWatch) canWalk() bool {
	if w == nil || w.race == nil {
		return false
	}
	return w.race.hasUntriedLane()
}

// speaking reports whether this arm is the one the person is hearing.
//
// An unraced stream always is — there is nobody else. An arm of a race is only
// while it holds the voice, which is the same rule [hedgeRace.emit] keeps about
// the deltas themselves: one request is one story, and a story told from the
// arm whose text is being HELD would be about words nobody is reading.
func (w *streamWatch) speaking() bool {
	if w == nil || w.race == nil {
		return true
	}
	return w.race.hears(w.arm)
}

// quietFor is how long this arm has been silent, spelled the way a person says
// it, and empty when it has never written at all — a stream that never started
// is not a stream that stopped.
func (w *streamWatch) quietFor(now time.Time) string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.last.IsZero() {
		return ""
	}
	quiet := now.Sub(w.last)
	if quiet < time.Second {
		return ""
	}
	return strconv.Itoa(int(quiet.Round(time.Second)/time.Second)) + "s"
}

// lane is who answered this arm, empty when nothing said.
func (w *streamWatch) lane() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.served
}

// written is how many tokens this arm delivered.
func (w *streamWatch) written() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tokens
}

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

// hedgeRace is one request that may become two.
type hedgeRace struct {
	client   *Client
	choice   lanes.Choice
	belief   lanes.Belief
	model    string
	expected int
	session  string
	observer StreamObserver
	report   *HedgeReport
	phase    *phaseClock
	budget   *lanes.Budget
	now      func() time.Time
	base     context.Context
	messages []ai.Message
	options  []ai.Option
	results  chan armResult

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
	// hedging is set the moment a second request is decided on, so that a
	// budget refusal is final rather than retried by the next delta.
	hedging bool
	decided chan struct{}
}

// raceFor decides whether this call is one the router is watching, and builds
// the race if it is.
//
// EVERY GATE IS HERE AND NOWHERE ELSE, so that the answer to "could this call
// hedge?" is one function. An arm of an existing race is never itself raced —
// that is what the watch already in the context means.
func (c *Client) raceFor(ctx context.Context, observer StreamObserver) (*hedgeRace, bool) {
	if streamWatchFrom(ctx) != nil {
		return nil, false
	}
	if !c.isOpenRouter() || c.routing() == RoutingOff {
		return nil, false
	}
	// A build with no router wired reaches this line and stops at the next one:
	// the Choice is put in the context by the chooser that produced it, so
	// "there is a Choice for THIS call, and it names a second lane" answers both
	// "is anybody home?" and "does this request have an alternative?" — and it
	// answers them about the request in hand rather than about a type name. A
	// separate feature check used to stand here and asked whether the registry's
	// chooser was the package's own type; when the real chooser took that type's
	// name the check inverted silently and no shipped build hedged at all. One
	// gate that reads the thing itself cannot rot that way.
	choice, ok := laneChoiceFromContext(ctx)
	if !ok || choice.Alt == "" {
		return nil, false
	}
	race := &hedgeRace{
		client:   c,
		choice:   choice,
		model:    strings.TrimSpace(c.config.Model),
		expected: expectedAnswerFrom(ctx),
		session:  streamSessionFrom(ctx),
		observer: observer,
		report:   HedgeReportFrom(ctx),
		phase:    phaseClockFrom(ctx),
		budget:   currentHedgeBudget(),
		now:      c.clock,
		speaker:  0,
		winner:   -1,
		held:     map[int][]StreamEvent{},
		tried:    map[string]bool{},
		results:  make(chan armResult, maxArms),
		decided:  make(chan struct{}),
	}
	if head := headLane(choice); head != "" {
		race.tried[strings.ToLower(head)] = true
	}
	// What is believed about the lane expected to serve, which is what turns a
	// gap into a surprise. The head of the order is the lane the router was
	// asked for first; a belief nobody holds leaves the watch with a derived
	// deadline and no drift test, which is the honest empty answer.
	if head := headLane(choice); head != "" {
		race.belief, _ = lanes.Default().Ledger().Belief(lanes.ID{Model: race.model, Lane: head})
	}
	return race, true
}

// headLane is the lane the request is expected to land on: the pin if there is
// one, else the head of the order.
func headLane(choice lanes.Choice) string {
	if len(choice.Only) > 0 {
		return choice.Only[0]
	}
	if len(choice.Order) > 0 {
		return choice.Order[0]
	}
	return ""
}

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
// alternative for the hedge, which demands it outright.
func (r *hedgeRace) start(index int, lane string) {
	armCtx, cancel := context.WithCancel(r.base)
	choice := r.choice
	if index > 0 {
		// A HEDGE DOES NOT HEDGE. One request gets one second chance, and an
		// arm with no alternative to name can never ask for another.
		choice.Alt = ""
	}
	watch := lanes.NewWatch(choice, r.belief, r.now())
	watch.SetExpectedTokens(r.expected)
	arm := &hedgeArm{
		index:  index,
		lane:   lane,
		cancel: cancel,
		watch: &streamWatch{
			race:  r,
			arm:   index,
			now:   r.now,
			watch: watch,
			began: r.now(),
		},
	}
	armCtx = withStreamWatch(armCtx, arm.watch)
	if lane != "" {
		armCtx = withHedgeLane(armCtx, lane)
	}
	r.mu.Lock()
	r.arms = append(r.arms, arm)
	r.mu.Unlock()

	observer := r.observerFor(index)
	guard.Go("provider.hedge.arm", func() {
		response, relearned, err := r.client.completeWithMessagesStreaming(armCtx, observer, r.messages, r.options...)
		cancel()
		r.results <- armResult{index: index, response: response, relearned: relearned, err: err}
	})
}

// beat drives the silence half of every watch. It is one goroutine per raced
// request, and it stops with the request.
//
// The period is a fraction of the deadline rather than a constant, because the
// deadline is itself derived: a lane whose first token is expected in four
// hundred milliseconds must not be judged late a quarter of a second after it
// already was.
func (r *hedgeRace) beat(stop <-chan struct{}) {
	period := r.choice.Deadline / 8
	if period < 5*time.Millisecond {
		period = 5 * time.Millisecond
	}
	if period > 250*time.Millisecond {
		period = 250 * time.Millisecond
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-r.base.Done():
			return
		case now := <-ticker.C:
			r.mu.Lock()
			arms := append([]*hedgeArm(nil), r.arms...)
			done := r.winner >= 0
			r.mu.Unlock()
			if done {
				return
			}
			for _, arm := range arms {
				arm.watch.silence(now)
			}
		}
	}
}

// hedge is the second request: budgeted, once, and never from an arm that is
// already the second one.
func (r *hedgeRace) hedge(from int, verdict lanes.Verdict, fault bool) {
	if r == nil || from != 0 {
		return
	}
	alt := strings.TrimSpace(r.choice.Alt)
	r.mu.Lock()
	if r.hedging || r.winner >= 0 || alt == "" {
		r.mu.Unlock()
		return
	}
	// Set before the budget is asked, so that a refusal is FINAL. A watch fires
	// one verdict; a race that re-asked on the next delta would turn one
	// refusal into a poll.
	r.hedging = true
	primary := r.arms[0]
	r.mu.Unlock()

	if !r.budget.Allow(r.now(), r.estimate(alt, r.expected)) {
		return
	}
	r.mu.Lock()
	r.tried[strings.ToLower(alt)] = true
	r.mu.Unlock()
	// The primary's own commitment point is measured from HERE: it keeps the
	// answer by writing another sixty-four tokens, or by finishing, and not by
	// the tokens it had already written before it stalled.
	primary.watch.commitOn(hedgeCommit)
	r.report.note(func(report *HedgeReport) {
		report.hedged, report.reason, report.fault = true, verdict.Reason, fault
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
	r.phase.switching(strings.ToLower(alt), primary.watch.quietFor(r.now()))
	r.start(1, alt)
}

// walk is the second rung: the rescue we sent was refused or broke, nobody has
// committed, and there is another machine behind this model that the frontier
// says is worth asking.
//
// IT IS NOT A SECOND HEDGE. A hedge is a bet against SLOWNESS and it is fired
// by the watch; this is a response to a lane that has FAILED, and the request
// it replaces is already gone. That is why it is not gated by [hedgeRace.hedging]
// — one refusal must not spend the one hedge a slow lane is still owed — and
// why it is gated by the budget and by [maxArms] instead.
//
// THE PRIMARY IS NEVER WALKED FROM. An arm that is still streaming has not
// failed, and the watch is the only thing allowed to give up on it.
func (r *hedgeRace) walk(from int) {
	if r == nil || r.base == nil || r.base.Err() != nil {
		return
	}
	alt := ""
	r.mu.Lock()
	if r.winner >= 0 || len(r.arms) >= maxArms {
		r.mu.Unlock()
		return
	}
	for _, scored := range r.choice.Frontier {
		name := strings.TrimSpace(scored.ID.Lane)
		if name == "" || r.tried[strings.ToLower(name)] {
			continue
		}
		alt = name
		break
	}
	if alt == "" {
		r.mu.Unlock()
		return
	}
	r.tried[strings.ToLower(alt)] = true
	index := len(r.arms)
	r.mu.Unlock()

	if !r.budget.Allow(r.now(), r.estimate(alt, r.expected)) {
		return
	}
	r.report.started(alt)
	r.phase.switching(strings.ToLower(alt), "")
	r.start(index, alt)
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

// commit hands the answer to one arm and cancels the other. It is idempotent:
// the first caller wins and every later one is a no-op.
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
	// CANCELLING IS WHAT STOPS THE BILL on the lanes that honour it, and it is
	// done outside the lock because a cancel wakes the loser's read loop, which
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
// The lock is held across the caller's observer on purpose: the two arms are
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
	for _, scored := range r.choice.Frontier {
		name := strings.TrimSpace(scored.ID.Lane)
		if name != "" && !r.tried[strings.ToLower(name)] {
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

// settle writes the ledger and the report, and hands back the winner's answer.
func (r *hedgeRace) settle(result armResult, seen map[int]armResult) (*ai.Response, bool, error) {
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
	// raced call, hedged or not — the rate limit is counted in REQUESTS rather
	// than in minutes, so a request that never reports leaves the allowance
	// looking emptier than it is.
	now := r.now()
	r.budget.NoteRequest(now)
	r.budget.NoteSpend(r.spent(seen), now)
	if hedged {
		winner, loser, primary := "", "", ""
		var waste, second float64
		for _, arm := range arms {
			if arm.index == result.index {
				winner = laneOf(arm)
			} else {
				// THE LOSER NAMED IS THE LAST ONE ASKED and the waste is EVERY
				// arm that did not answer. One name cannot describe a walk of
				// three, and the row would rather carry the nearest miss than
				// an invented list; the money, though, is the sum, because a
				// waste column that reported one of three cancelled arms would
				// understate what the rescue cost by exactly the amount that
				// makes it worth knowing.
				loser = laneOf(arm)
				waste += r.estimate(loser, arm.watch.written())
			}
			if arm.index == 0 {
				primary = laneOf(arm)
			}
			if arm.index > 0 {
				// EVERY RESCUE IS CHARGED, not only the first one. The walk can
				// put a third and a fourth request on the wire, and a budget
				// that only ever saw the second would let a walk spend the
				// session's whole share while believing it had spent one arm's.
				second += r.armCost(seen, arm)
			}
		}
		r.budget.NoteHedge(second, now)
		r.report.note(func(report *HedgeReport) {
			report.winner, report.loser, report.waste = winner, loser, waste
			report.primary = primary
		})
	}
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
// The fallback is exact rather than a guess: a hedge sends `only`, so an arm
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
// there is one. The cancelled arm sends no usage frame, so what is counted is
// the answer that arrived plus the estimate of what was thrown away.
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
// money — it feeds the budget's share and the ledger's waste column, both of
// which are about proportions.
func (r *hedgeRace) estimate(lane string, tokens int) float64 {
	lane = strings.TrimSpace(lane)
	if lane == "" {
		return 0
	}
	for _, scored := range r.choice.Frontier {
		if scored.ID.Lane != lane {
			continue
		}
		if r.expected <= 0 || tokens <= 0 || tokens >= r.expected {
			return scored.Price
		}
		return scored.Price * float64(tokens) / float64(r.expected)
	}
	return 0
}

// ── THE LANE A HEDGE DEMANDS ────────────────────────────────────────────────

type hedgeLaneContextKey struct{}

// withHedgeLane marks a request as the second one, bound to exactly one lane.
func withHedgeLane(ctx context.Context, lane string) context.Context {
	return context.WithValue(ctx, hedgeLaneContextKey{}, lane)
}

// hedgeLaneFrom is the lane this request must go to, empty on every ordinary
// call.
func hedgeLaneFrom(ctx context.Context) string {
	lane, _ := ctx.Value(hedgeLaneContextKey{}).(string)
	return lane
}

// hedgePreference puts the hedge's demand on the wire.
//
// A HEDGE NAMES ITS LANE AND TAKES NO FALLBACK. The whole point of the second
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
