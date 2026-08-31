package provider

import (
	"context"
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
	// fault records that the verdict was about the path rather than the lane,
	// which is what keeps the belief out of it.
	fault bool
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
	tokens   int
	commitAt int
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

// serve records who the stream said was answering it.
func (w *streamWatch) serve(lane string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.served == "" {
		w.served = strings.TrimSpace(lane)
	}
}

// token is one delta of real progress — a word, a thought, a fragment of a
// call. It advances the drift test and it is where an arm commits.
func (w *streamWatch) token() {
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
	verdict := w.watch.Token(w.tokens, now)
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
	// held is what the arm that is not speaking has produced, replayed if it
	// wins and dropped if it does not.
	held []StreamEvent
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
	if !lanes.Chooses() {
		return nil, false
	}
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
		budget:   currentHedgeBudget(),
		now:      c.clock,
		speaker:  0,
		winner:   -1,
		results:  make(chan armResult, 2),
		decided:  make(chan struct{}),
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
	// The primary's own commitment point is measured from HERE: it keeps the
	// answer by writing another sixty-four tokens, or by finishing, and not by
	// the tokens it had already written before it stalled.
	primary.watch.commitOn(hedgeCommit)
	r.report.note(func(report *HedgeReport) {
		report.hedged, report.reason, report.fault = true, verdict.Reason, fault
	})
	r.start(1, alt)
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
	held := r.held
	r.held = nil
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
		if len(r.held) < heldEvents {
			r.held = append(r.held, event)
		}
		return
	}
	if event.Kind == StreamDelta {
		r.spoken = true
	}
	r.observer(event)
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
		winner, loser := "", ""
		var waste, second float64
		for _, arm := range arms {
			if arm.index == result.index {
				winner = laneOf(arm)
			} else {
				loser = laneOf(arm)
				waste = r.estimate(loser, arm.watch.written())
			}
			if arm.index == 1 {
				second = r.armCost(seen, arm)
			}
		}
		r.budget.NoteHedge(second, now)
		r.report.note(func(report *HedgeReport) {
			report.winner, report.loser, report.waste = winner, loser, waste
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
