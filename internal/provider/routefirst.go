package provider

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ── `auto` FIRST, THE BELIEF WHEN THE ROUTER LETS GO ────────────────────────
//
// The `lane` row's default is `auto`, and what `auto` means changed here:
// OpenRouter routes, and this build watches. Every answer names the machine
// that served it, and that machine's waits and outcomes land in the belief
// ledger ([Client.noteLane], [Client.noteLaneOutcome]) whether or not this
// build asked for it — so a takeover, when one comes, is never a cold guess.
//
// A takeover is earned, never assumed: two refusals, or two answers that lost
// their thread, inside the window that remembers them. One bad minute is a
// queue draining somewhere; two of them is the router sending the next call to
// the same machine. When the window opens the chooser ranks the lanes it has
// been learning from all along and the request carries its set — the same
// object a pin would send — and when the window lapses the row is `auto`
// again, which is the same thing said in reverse: OpenRouter routes, and this
// build watches.
//
// THE FLOOR IS THE SAME FLOOR THE STRIKE LEDGER ALREADY KEEPS. A refusal is
// forgotten after five minutes (velocity.go's ignoreCooldown, mirrored in
// internal/lane's AvailabilityHalfLife) and a takeover after thirty — long
// enough for a person to feel the difference, short enough that one bad
// evening does not re-route the week. Neither figure is tuned here: the window
// is the strike ledger's cooldown counted twice over, and the threshold is the
// number of bad answers a person has already been offered by the time anybody
// would reach for `/model`.

const (
	// takeoverWindow is how long the chooser holds the road after the router
	// earned a takeover. It is the strike ledger's own five-minute cooldown
	// counted six times — the horizon over which a person reading their status
	// line can tell the difference, and no longer, because the row on their
	// screen still says `auto` and `auto` owes them the router's balance back.
	takeoverWindow = 30 * time.Minute

	// takeoverStrikes is how many refusals, inside their own five-minute
	// cooldown, earn a takeover. Two: one is a queue draining, two is the
	// router sending the next call to the same machine — and it is the same
	// count internal/lane's availability prior already prices as halving the
	// belief a lane will answer at all.
	takeoverStrikes = 2

	// takeoverBads is how many answers that came back unusable — the thread
	// lost, tool markup, a cut — earn a takeover, on the same window. The
	// quality gate the frontier applies needs six sightings to drop a lane
	// (assessment, 2026-09-11); a person has felt the answer by the second.
	takeoverBads = 2
)

// routerGate is the per-model record of what the router has been doing with a
// model this build asked it to route. It is per model for the reason the
// retired pin is per pair: "the router is serving this model badly" is a fact
// about one model, and a second model's traffic is a different question.
type routerGate struct {
	mu sync.Mutex
	// strikes are the moments of the last few routing refusals and the last
	// few unusable answers, pruned to their own half-lives on every read. Two
	// slices rather than one because the two triggers are different promises:
	// a refusal is about whether an answer ARRIVES, a bad answer is about
	// whether the one that arrived could be used, and internal/lane keeps the
	// two on different axes for the same reason.
	strikes []time.Time
	bads    []time.Time
	// until is when the current takeover lapses. A zero value is "the router
	// is routing" — which is most of the time, and is the whole point of the
	// row being called `auto` and not `advisory`.
	until time.Time
}

// routerGates is the process-wide map of them. Process-wide for the reason
// the pin knob is: the conversation, the errands beside it and every task node
// it starts are one process, and a router that is failing a model is failing
// it for all of them at once. Keyed by the ledger's own folded model id, so
// the dated slug and the pointer land on the same gate.
var routerGates sync.Map // string → *routerGate

// gateFor returns the gate one model's traffic is judged against, creating it
// on first sight.
func gateFor(model string) *routerGate {
	model = laneModel(model)
	if model == "" {
		return &routerGate{}
	}
	gate, _ := routerGates.LoadOrStore(model, &routerGate{})
	return gate.(*routerGate)
}

// prune forgets every moment older than its own half-life. It is called with
// the lock held, on every read and every write, so the slices stay short and
// the arithmetic below is over the window a person would call "lately".
func (g *routerGate) prune(now time.Time) {
	g.strikes = pruneTo(g.strikes, now, 5*time.Minute)
	g.bads = pruneTo(g.bads, now, 30*time.Minute)
}

func pruneTo(list []time.Time, now time.Time, age time.Duration) []time.Time {
	cut := now.Add(-age)
	kept := list[:0]
	for _, at := range list {
		if at.After(cut) {
			kept = append(kept, at)
		}
	}
	return kept
}

// TakenOver reports whether the chooser holds this model's road right now.
func (g *routerGate) TakenOver(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prune(now)
	return now.Before(g.until)
}

// NoteRefused records that a routing refusal named this model. It answers
// whether this call is the one that earned the takeover, so a person is told
// once and not once per retry.
func (g *routerGate) NoteRefused(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prune(now)
	g.strikes = append(g.strikes, now)
	if len(g.strikes) < takeoverStrikes || now.Before(g.until) {
		return false
	}
	g.until = now.Add(takeoverWindow)
	return true
}

// NoteBad records that an answer the router chose came back unusable. Same
// contract as [NoteRefused].
func (g *routerGate) NoteBad(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prune(now)
	g.bads = append(g.bads, now)
	if len(g.bads) < takeoverBads || now.Before(g.until) {
		return false
	}
	g.until = now.Add(takeoverWindow)
	return true
}

// NoteHealthy records an answer that arrived whole. It does not shorten a
// takeover that is already armed — the window is the promise, and a single
// good answer inside it is the router having one good minute, the exact thing
// the threshold is there to disregard — but it keeps a router that is serving
// well from ever earning one.
func (g *routerGate) NoteHealthy(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.prune(now)
	if now.Before(g.until) {
		return
	}
	// HALF THE EVIDENCE DRAINS ON A GOOD ANSWER. A refusal an hour ago beside
	// a refusal a minute ago reads as a lane having an afternoon; the same two
	// with a healthy answer between them read as a queue that drained. One
	// good answer takes the freshest mark off each axis, which is the shape of
	// "back to normal" a person would draw on the same record.
	if n := len(g.strikes); n > 0 {
		g.strikes = g.strikes[:n-1]
	}
	if n := len(g.bads); n > 0 {
		g.bads = g.bads[:n-1]
	}
}

// ── THE TWO DOORS THE GATE IS READ AND WRITTEN THROUGH ──────────────────────

// routingGateTakenOver is the read [drawLaneChoice] makes: is the chooser
// holding this model's road? It is its own function for the reason every
// other belief site is one — a law test counts the doors, and a second door is
// a second answer to one question.
func (c *Client) routingGateTakenOver(model string) bool {
	if !c.carriesPreferences() {
		return false
	}
	return gateFor(model).TakenOver(laneNow())
}

// noteRouterChoice is the write side for ANSWERS, called where an answer's lane
// is known. Refused and bad move their own axes; a healthy answer moves both.
// It answers whether THIS call armed the takeover, so the sentence a person
// reads is said once, by the call that earned it, and never by the retry that
// follows.
//
// A REFUSAL IS EVIDENCE ABOUT THE ROUTER'S DEFAULT ONLY WHEN THE ROUTER'S
// DEFAULT WAS ASKED. A demand this process narrowed, that the router then
// refused, is evidence about OUR CHOICE — and it must not hold the road. That
// is what [Client.noteRouterRefusal] splits, and the measurement that priced
// it: a live chat (2026-09-12) held four `Retry 1/2: relaxed the endpoint
// filter` lines, because the takeover had armed on earlier refusals, and every
// narrowed demand it then chose answered 404 — and counted as ANOTHER refusal,
// arming the takeover again, each cost of the loop a full ~80k-token re-send
// BEFORE the ladder's first rung could go bare. Only a bare refusal may count.
//
// THE ANSWER SIDES NEED NO SHAPE SPLIT. An answer that arrived is not a
// refusal of anything: a good one is a queue drained, and an unusable one is a
// verdict about the machine that spoke it, whichever spine chose it. So
// NoteHealthy and NoteBad apply regardless of what the request demanded; only
// the refusal axis narrows.
func (c *Client) noteRouterChoice(model, served, reason string, accepted bool) (armed bool) {
	model = laneModel(model)
	if !c.carriesPreferences() || model == "" {
		return false
	}
	// THE ATTRIBUTION LAW IS [Client.noteLaneOutcome]'s: an answer that named
	// no machine is evidence about no machine, and a takeover earned on an
	// anonymous answer is a takeover against nobody.
	if strings.TrimSpace(served) == "" {
		return false
	}
	gate := gateFor(model)
	now := laneNow()
	switch {
	case reason == "refused":
		return gate.NoteRefused(now)
	case !accepted:
		return gate.NoteBad(now)
	default:
		gate.NoteHealthy(now)
		return false
	}
}

// noteRouterRefusal is the refusal door, reached from velocity.go's
// [Client.refuseLane] — the only door a refusal ever passes. It carries the
// attempt's shape because the law above splits refusals on it: only a refusal
// of a BARE attempt is evidence about the router's default, and only that may
// count toward a takeover. A refusal of a demand this process narrowed is a
// verdict about OUR CHOICE — the picker earned that, not the router — and the
// gate hears nothing, which is what keeps a takeover's own narrowed demands
// from re-arming the takeover that chose them.
func (c *Client) noteRouterRefusal(model, served string, askedBare bool) (armed bool) {
	if !askedBare {
		return false
	}
	return c.noteRouterChoice(model, served, "refused", false)
}

// armTakeover puts the gate over for one model, and forgetRouterGates empties
// the map. They are for tests: a fixture about the chooser's wire object needs
// the chooser to be holding the road, and no fixture may inherit another's
// takeovers — the shipped path arms the gate through [routerGate.NoteRefused]
// and [routerGate.NoteBad] and forgets it on the window's own clock.
func armTakeover(model string) {
	gate := gateFor(model)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.until = laneNow().Add(takeoverWindow)
}

func forgetRouterGates() {
	routerGates = sync.Map{}
	parkedTakeover.mu.Lock()
	defer parkedTakeover.mu.Unlock()
	parkedTakeover.lines = nil
}

// takeoverLine is the whole sentence a person reads when the chooser takes
// over, spelled once. Two surfaces say it — the stream while the answer is in
// flight and the conversation that keeps it — and a sentence spelled twice is
// a sentence that drifts. It names the model rather than the machine, because
// the machine is the thing that changed and the model is the thing a person
// goes to change.
func takeoverLine(model string) string {
	return model + " has come back refused or unusable twice lately, so aforge is choosing its machine for a while · pin one in /model to choose it yourself"
}

// ── THE SENTENCE, PARKED UNTIL SOMEBODY IS READING ──────────────────────────
//
// The call that earns a takeover is often one nobody is watching — a naming
// errand, a memory reflex, a task node — and a sentence emitted into a stream
// nobody reads is a sentence never said. So it parks here, beside the retired
// pin's parked lines, and the next request a person is actually watching
// carries it ([Client.sendShaped] calls [tellTakeover]). The mechanism is the
// retired pin's own (lanepin.go), for the same reason and under the same two
// conditions: a stream to say it on, and a role a person reads.

var parkedTakeover struct {
	mu    sync.Mutex
	lines []string
}

// parkTakeoverLine queues the sentence once per takeover. It is called only by
// a door that answered "this call armed it", so the parked lines never repeat
// one fact twice — a second refusal inside the window earns no second line.
func parkTakeoverLine(model string) {
	parkedTakeover.mu.Lock()
	defer parkedTakeover.mu.Unlock()
	parkedTakeover.lines = append(parkedTakeover.lines, takeoverLine(model))
}

// tellTakeover hands whatever is parked to a call somebody is reading. The two
// conditions are lanepin.go's tellRetiredPins', word for word: a stream to say
// it on, and a role a person is watching.
func tellTakeover(ctx context.Context) {
	if ctx == nil || streamObserverFrom(ctx) == nil || !RoleFrom(ctx).Visible() {
		return
	}
	for _, line := range takeParkedTakeovers() {
		Emit(ctx, StreamNotice, line)
	}
}

// takeParkedTakeovers drains the queue in one locked step, so the Emits that
// follow run outside the lock: a notice that re-entered the provider would
// otherwise meet the mutex its own door is holding.
func takeParkedTakeovers() []string {
	parkedTakeover.mu.Lock()
	defer parkedTakeover.mu.Unlock()
	owed := parkedTakeover.lines
	parkedTakeover.lines = nil
	return owed
}
