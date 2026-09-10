package provider

import (
	"context"
	"strings"
	"sync"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── THE PHASE CLOCK: WHAT THIS REQUEST IS DOING, AND WHAT HAPPENS NEXT ──────
//
// THE DEFECT THIS FIXES, in the words it was reported in: "stuck in a state
// where in the middle of thinking it just says still working, but at the bottom
// I see glm 5.3 friendli and t/s — not sure if we are not accounting for all
// places things could slow, and also no timer indicator like 'hey, if this does
// not work I am changing provider in …'".
//
// Both halves of that are true and they are the same omission. A request has at
// least nine states in which it can be slow — a handshake, a queue before the
// first word, a run of thought, an answer arriving in lumps, a rate-limit wait,
// a relaxed re-ask, a rescue in flight, a tool running, a compaction — and
// until this file the surface could see exactly one bit of any of them: whether
// a delta had arrived in the last ten seconds. Everything else it drew was
// INFERRED, which is why the line under a stalled thinking pass said "still
// working" and the status row went on quoting the previous answer's lane and
// rate as though they were happening now.
//
// So the layer that knows says so. A phase is pushed the moment it changes and
// again while it lasts, and it carries four things and no more:
//
//	Phase     what is happening, in a person's word
//	Since     when this phase began — the surface counts up from it, at paint
//	Deadline  when something will be DONE about it, and zero when nothing will
//	Then      what that something is ("parasail"), empty when there is nothing
//
// A DEADLINE IS NEVER INVENTED. The countdown a person reads is the watch's own
// hedge deadline or the pacing wait the router asked for — a real moment at
// which this build really acts. Where no alternative lane exists, or the guard
// is off, or routing is off, the phase and its clock are still true and the
// consequence is simply absent. A fake countdown that expires and does nothing
// is worse than no countdown: it is the surface lying about the machinery, and
// the emptiness law already says what to draw instead, which is nothing.
//
// THE ARROW POINTS ONE WAY, as it does for the lane news it sits beside
// (internal/session's lanenews.go): internal/session imports this package, so
// this package pushes and a reader registers. A build with nobody listening
// pays one atomic load per phase change.

// Phase is what a request is doing right now.
//
// THE WORDS ARE THE PERSON'S. There is no `awaiting_first_token`, no `hedging`,
// no `backoff` — a surface that has to translate a machine word is a surface
// that will translate it differently from the next one (the design law about
// machinery vocabulary). What is spelled here is what is read out loud.
type Phase string

const (
	// PhaseConnecting is the handshake: DNS, TLS, and the request going out.
	// Nothing has been accepted yet.
	PhaseConnecting Phase = "connecting"
	// PhaseConnectionLost is a reachability wait, not a slow model response.
	PhaseConnectionLost Phase = "waiting for connection"
	// PhaseFirstWord is the wait after the endpoint accepted the request and
	// before it wrote anything — the queue, the router's own fallback walk, a
	// cold model loading. It is the phase a hedge deadline belongs to.
	PhaseFirstWord Phase = "first word"
	// PhaseThinking is a run of reasoning tokens: the endpoint IS writing, and
	// none of it is on the screen.
	PhaseThinking Phase = "thinking"
	// PhaseWriting is the answer arriving.
	PhaseWriting Phase = "writing"
	// PhasePaced is a rate-limit wait. Its deadline is the router's own
	// `Retry-After` and is therefore real.
	PhasePaced Phase = "paced"
	// PhaseRetrying is the relax ladder: the same question asked again with
	// something dropped from it. Detail carries "2 of 6".
	PhaseRetrying Phase = "trying again"
	// PhaseSwitching is a rescue in flight — a second request to another lane,
	// with nobody committed yet. Then names the lane it went to.
	PhaseSwitching Phase = "switching"
	// PhaseAsking is a wait a person can end: the lane they pinned has gone
	// quiet, there is somewhere else to go, and a pin is asked rather than
	// overridden (offer.go). Detail carries "coreweave is slow", Then the lane
	// the rescue would go to, and Ask the token `y` answers with.
	//
	// IT IS A PHASE AND NOT A SECOND CHANNEL. One sentence about one request,
	// on the seam that already carries every other sentence about it — a second
	// channel for it would be a second thing to keep alive, and the first time
	// one of them stalled the other would still be drawing.
	PhaseAsking Phase = "asking"
	// PhaseAllSlow is the visible half of [control.Report]: every reachable lane
	// is believed slow, so acting would buy nothing and the only honest act left
	// is to SAY the wait is real.
	//
	// SILENCE IS NEVER AN OPTION, and saying nothing was the old behaviour. A
	// person watching a line that says "still working" through a real wait is
	// being told less than this build knows, and what this build knows is that
	// it has weighed the alternatives and there are none.
	//
	// IT KEEPS THE CLOCK OF THE WAIT IT IS ABOUT. The report does not start a
	// new phase in a person's terms — nothing has changed about what the
	// endpoint is doing — so [phaseClock.allSlow] leaves [PhaseNews.Since]
	// exactly where it was and the surface goes on counting up from the moment
	// the wait began.
	PhaseAllSlow Phase = "all lanes slow"
	// PhaseSwitchingModel is the LAST rung of the ladder and the only one that
	// changes what a person asked for: every lane of the model has been tried
	// and a fallback model is being asked instead. Then names it.
	//
	// It is a different word from [PhaseSwitching] on purpose. Changing which
	// machine serves an answer is bookkeeping; changing which model writes it
	// is a different answer, and a surface that spelled the two the same way
	// would be hiding the one that matters.
	PhaseSwitchingModel Phase = "switching model"
	// PhaseRunning, PhaseChecking, PhaseTidying and PhaseBriefing belong to
	// internal/session and are spelled here because there is ONE vocabulary and
	// one reader: a tool executing, a gate reading an answer, a compaction pass,
	// and a turn being written down for whoever takes it over.
	PhaseRunning  Phase = "running"
	PhaseChecking Phase = "checking"
	PhaseTidying  Phase = "tidying"
	// PhaseBriefing is the harness writing the instruction a turn is handed over
	// on, before there is a task to point at (internal/session's checkpoint.go).
	// Detail names who it is for, so the row reads "briefing a worker".
	PhaseBriefing Phase = "briefing"
	// PhasePreparing names bounded context lookup before the main request starts.
	PhasePreparing Phase = "preparing"
	// PhaseTakingStock is the reading a turn stops for at a mark: a second mind
	// is shown an account of the work so far and asked what is left of the ask
	// (internal/session's checkpoint.go, [readMark]). It is a ten-to-thirty
	// second call and it used to draw nothing at all.
	//
	// THE WORD IS THE ONE A PERSON WOULD USE for stopping to see where you are,
	// and it is a different word from `checking` on purpose: checking is a
	// reader deciding whether an answer is finished, and this is a reader
	// weighing the whole ask against everything that has been done. A surface
	// that spelled them the same way would say the same sentence twice for two
	// waits that mean different things.
	PhaseTakingStock Phase = "taking stock"
)

// PhaseWindow is how long a phase still describes the present: past it a surface
// draws nothing rather than a clock for work that may be over.
//
// IT IS THE CONTRACT BETWEEN WHOEVER POSTS A PHASE AND WHOEVER DRAWS ONE, so it
// is spelled once, here, with the vocabulary — and every beat that keeps a phase
// alive is DERIVED from it rather than written down beside it. Two packages with
// two ideas of how long a phase lasts is a stage that goes dark while it is still
// running, which is the defect this constant was moved out of internal/tui3 to
// end.
//
// Both beats sit comfortably inside it: a request says its phase again at most
// once a second while it lasts ([phaseBeat] below), and a turn holding a phase
// open says it again every third of this window (internal/session's
// [phaseHeldBeat]). Fifteen seconds is therefore a wide margin on either — wide
// enough that a busy frame or a machine under load never blinks the segment, and
// short enough that a posting layer whose goroutine was killed without saying so
// takes its clock off the screen while a person is still looking at it.
const PhaseWindow = 15 * time.Second

// PhaseNews is one moment of one request's life.
//
// Anything unknown is left zero and draws nothing, which is the emptiness law
// said at the seam rather than at the surface: no reader has to invent a figure
// to have something to print.
type PhaseNews struct {
	// Phase is what is happening. An empty phase is the end of the story —
	// posted when a turn stops, so a surface stops drawing a clock for work
	// that is over.
	Phase Phase
	// Since is when THIS phase began. The surface counts up from it at paint,
	// so nothing here has to tick.
	Since time.Time
	// Deadline is the moment something will be done about it, and Then is what
	// that something is. Both are zero and empty unless a real deadline exists:
	// see the header.
	Deadline time.Time
	Then     string
	// Lane is the machine answering, when one has named itself, and Rate how
	// fast it is writing right now in tokens a second. Zero for both is "not
	// measured", never "nothing".
	Lane string
	Rate float64
	// Detail is the phase's own noun, already in a person's words: the tool
	// being run, the rung of the ladder, how long a stall had gone on.
	Detail string
	// Ask is the token naming an open offer, empty when there is none — which
	// is every phase but [PhaseAsking] and the post that withdraws one. A
	// surface hands it back to [AnswerOffer] with the person's answer, so the
	// keystroke lands on the request that raised the question and never on the
	// one that came after it.
	Ask string
	// Model is the model this request is on, and Role who it is for. A surface
	// draws only the roles a person is reading (internal/lane's roles.go): the
	// naming errand and the memory reflex that run beside a talk turn are not
	// the answer somebody is waiting for, and a status line that showed
	// whichever of them answered last was the other half of the reported
	// defect.
	Model string
	Role  lanes.Role
	At    time.Time

	// Session is the conversation this request belongs to ([SessionFrom]), and
	// it is EMPTY IN EVERY BUILD THAT NEEDS NO ANSWER: one process with one
	// window has nothing to disambiguate. An engine that is a separate process
	// from its surfaces reads it to decide which connection a piece of news
	// belongs on, and news that names no conversation is news it cannot place.
	Session string

	// Relayed says this news arrived over a connection from the engine that
	// produced it, rather than off this process's own stream.
	//
	// IT EXISTS TO STOP A LOOP. A build that is both serving and watching —
	// which is every test that drives an engine host inside its own process —
	// would otherwise forward what it just received straight back out of the
	// door it came in, forever. It is never put on the wire: the side that
	// takes a frame off the wire is the only side that can know it, and it
	// stamps it on receipt.
	Relayed bool
}

// Waiting reports whether this phase is one a person is waiting through with
// nothing arriving. It is the phase clock's own reading of its own vocabulary,
// kept here so that two surfaces cannot disagree about it.
func (n PhaseNews) Waiting() bool {
	switch n.Phase {
	case PhaseConnecting, PhaseConnectionLost, PhaseFirstWord, PhasePaced, PhaseRetrying, PhaseSwitching, PhaseSwitchingModel, PhaseAsking, PhaseAllSlow:
		return true
	}
	return false
}

var (
	phaseMu     sync.RWMutex
	phaseReader func(PhaseNews)
)

// OnPhase registers the reader every phase change is told to and hands back the
// one that was there, so a surface that opens over another can put it back when
// it closes. A nil function unregisters.
func OnPhase(fn func(PhaseNews)) (previous func(PhaseNews)) {
	phaseMu.Lock()
	defer phaseMu.Unlock()
	previous, phaseReader = phaseReader, fn
	return previous
}

// phaseListening reports whether anybody is reading phases at all.
//
// IT IS WHAT "NO SURFACE" MEANS, and it is asked in exactly one place besides
// the clock's own constructor: a pinned lane that stalls with nobody to ask
// borrows instead of asking (offer.go). Reading the same seam for both is what
// stops the two ideas of "headless" from drifting apart.
func phaseListening() bool {
	phaseMu.RLock()
	defer phaseMu.RUnlock()
	return phaseReader != nil
}

// postPhase tells whoever is listening.
//
// IT NEVER BLOCKS AND NEVER PANICS. This is called from the read loop, between
// two deltas, against the connection's idle watchdog — the same budget the
// stream observer is documented to keep — so a reader that works pays for it in
// the wrong place. The one live reader hands the news to a desk and asks for a
// frame.
func postPhase(news PhaseNews) {
	phaseMu.RLock()
	reader := phaseReader
	phaseMu.RUnlock()
	if reader == nil {
		return
	}
	if news.At.IsZero() {
		news.At = time.Now()
	}
	if news.Since.IsZero() {
		news.Since = news.At
	}
	reader(news)
}

// ── THE ONE CLOCK A REQUEST KEEPS ───────────────────────────────────────────

// phaseClock is one request's phase, as the stream loop moves it along.
//
// It exists so that the loop says WHAT CHANGED rather than re-deciding the
// whole sentence per delta: a phase change posts at once, and a phase that is
// merely continuing posts at most once a second, which is the fastest a person
// can read a number that is changing anyway.
type phaseClock struct {
	// mu is here because a raced request has TWO stream loops and one story.
	// Both arms hold the same clock; only the one the person is hearing drives
	// it, and the handover happens on a third goroutine (hedge.go's flip).
	mu    sync.Mutex
	model string
	role  lanes.Role
	// session is the conversation these requests belong to, carried so that an
	// engine serving many windows can put this clock on the right one
	// (roles.go's [WithSession]). It is empty in a build where there is only
	// one window to put it on.
	session string
	// phase is what was last posted, since when, and when it was last said out
	// loud.
	phase Phase
	since time.Time
	said  time.Time
	// lane is who is answering, tokens how many deltas this phase has carried,
	// and deadline/then the consequence in force.
	lane     string
	tokens   int
	deadline time.Time
	then     string
	// ask is the open offer's token while one is up, empty otherwise.
	ask string
	// now is the clock, which is the client's own so that a test can move it.
	now func() time.Time
}

// phaseBeat is how often a phase that has not changed says so again. One second
// is the granularity a person reads an elapsed clock at, and it keeps the cost
// of the whole seam at one post a second per request. It is a fifteenth of
// [PhaseWindow], which is the margin that makes a dropped beat invisible.
const phaseBeat = time.Second

// newPhaseClock starts one request's clock. A nil clock is a request nobody is
// watching and every method on it is a no-op, which is how a build with no
// reader — and every test that does not care — pays nothing.
// IT READS NO CLOCK TO DECIDE WHETHER IT EXISTS. A request nobody is watching
// must cost nothing at all, and "nothing" includes the measurement seam: two
// velocity tests script `Client.now` with an exact sequence of ticks, and a
// clock read taken before the nil check moved every figure they assert by one
// step. A seam that is free only when it is switched off is not free.
func (c *Client) newPhaseClock(ctx context.Context, model string) *phaseClock {
	phaseMu.RLock()
	listening := phaseReader != nil
	phaseMu.RUnlock()
	if !listening {
		return nil
	}
	return &phaseClock{
		model:   strings.TrimSpace(model),
		role:    RoleFrom(ctx),
		session: SessionFrom(ctx),
		now:     c.clock,
	}
}

// enter moves to a phase and says so immediately. A phase that is already the
// current one is left alone, so the loop may call it per delta.
func (p *phaseClock) enter(phase Phase, detail string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase == phase {
		return
	}
	now := p.now()
	p.phase, p.since, p.tokens = phase, now, 0
	// A CONSEQUENCE BELONGS TO THE PHASE THAT EARNED IT. The hedge deadline is
	// about the wait for the first word; carrying it into the writing would
	// leave a countdown on the screen for something that can no longer happen.
	if phase != PhaseFirstWord && phase != PhaseConnecting {
		p.deadline, p.then = time.Time{}, ""
	}
	// AND AN OFFER BELONGS TO THE WAIT THAT RAISED IT. A question about a lane
	// that has since started writing is a question about nothing, and leaving
	// it on the screen would be the surface asking a person to answer it.
	p.ask = ""
	p.say(detail, now)
}

// firstWord opens the wait in which the endpoint owes an answer, carrying the
// consequence in the same post when there is one.
//
// THE CONSEQUENCE RIDES WITH THE PHASE IT BELONGS TO, in one post and not two.
// Said separately, the first thing a person read was a bare "first word · 0.0s"
// and the countdown appeared a frame later — which is exactly the flicker the
// waiting grace exists to avoid everywhere else on this surface. A zero
// deadline or an unnamed lane is the honest empty answer and draws nothing.
func (p *phaseClock) firstWord(deadline time.Time, then string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.phase == PhaseFirstWord {
		return
	}
	now := p.now()
	p.phase, p.since, p.tokens = PhaseFirstWord, now, 0
	if then == "" || deadline.IsZero() {
		deadline, then = time.Time{}, ""
	}
	p.deadline, p.then = deadline, then
	p.say("", now)
}

// serve names the machine that is answering.
func (p *phaseClock) serve(lane string) {
	if p == nil || lane == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lane == lane {
		return
	}
	p.lane = lane
	p.say("", p.now())
}

// wrote is one delta of progress in the current phase. It is what turns into a
// rate, and it is the reason a phase says itself again while it lasts.
func (p *phaseClock) wrote() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens++
	now := p.now()
	if now.Sub(p.said) < phaseBeat {
		return
	}
	p.say("", now)
}

// done ends the story, so a surface stops drawing a clock for a request that is
// over. It is deliberately a post and not an absence: a stale phase left on the
// screen is exactly the defect this file exists for.
func (p *phaseClock) done() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = ""
	postPhase(PhaseNews{Model: p.model, Role: p.role, Session: p.session, At: p.now()})
}

// say posts the phase as it stands. IT IS CALLED WITH THE LOCK HELD, from every
// mutator above and from nowhere else; the post itself is a hand-off to a
// reader documented not to work, which is what makes that safe.
func (p *phaseClock) say(detail string, now time.Time) {
	p.said = now
	news := PhaseNews{
		Phase:    p.phase,
		Since:    p.since,
		Deadline: p.deadline,
		Then:     p.then,
		Ask:      p.ask,
		Lane:     p.lane,
		Detail:   detail,
		Model:    p.model,
		Role:     p.role,
		Session:  p.session,
		At:       now,
	}
	// THE RATE IS THE ONE THIS PHASE MEASURED, and never the last answer's. A
	// figure carried over from a finished turn is what the status line was
	// doing when it showed a lane and a throughput under a stalled request.
	if elapsed := now.Sub(p.since).Seconds(); p.tokens > 1 && elapsed > 0 {
		news.Rate = float64(p.tokens) / elapsed
	}
	postPhase(news)
}

// ── THE PHASES THE LAYERS ABOVE THE STREAM LOOP POST ────────────────────────

// notePhase is how a seam that holds no clock of its own — the retry loop, the
// relax ladder — says what it is doing. It is a plain post because those seams
// have exactly one thing to say and no phase to carry.
func notePhase(ctx context.Context, model string, phase Phase, detail string, since, deadline time.Time, then string) {
	postPhase(PhaseNews{
		Phase:    phase,
		Since:    since,
		Deadline: deadline,
		Then:     then,
		Detail:   detail,
		Model:    strings.TrimSpace(model),
		Role:     RoleFrom(ctx),
		Session:  SessionFrom(ctx),
	})
}

// switching is the rescue, said while it is going out rather than once it has
// landed. Detail is how long the stream had been quiet, because "stalled 9s"
// and "switching to parasail" are one sentence and a person reading only the
// second half would not know what it was about.
func (p *phaseClock) switching(alt, stalled string) {
	if p == nil || alt == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.phase, p.since, p.tokens = PhaseSwitching, now, 0
	p.deadline, p.then = time.Time{}, alt
	p.say(stalled, now)
}

// asking raises the offer: this lane is slow, there is somewhere else to go,
// and the person who pinned it is asked rather than overridden (offer.go).
//
// IT IS RAISED ONCE PER REQUEST and the caller is what holds that; a second
// offer for one answer would be nagging.
func (p *phaseClock) asking(lane, ask string) {
	if p == nil || ask == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.phase, p.since, p.tokens = PhaseAsking, now, 0
	p.deadline, p.then, p.ask = time.Time{}, autoRouting, ask
	p.say(lane+" is slow", now)
}

// autoRouting is what a `y` switches TO, in the word this build already uses for
// it: `auto` is routing left alone, which is what the picker's own row and the
// manual page both call it.
//
// IT IS NOT THE LANE THE RESCUE GOES TO, and the difference is what the person
// is being asked. They are not choosing a machine — the frontier chose one
// before the question was raised, because the moment a rescue is wanted is the
// worst possible moment to start choosing — they are being asked whether to let
// go of the pin for this one answer. `switch to parasail?` would be offering
// them a decision they are not making, on evidence they do not have.
const autoRouting = "auto"

// withdrew takes the offer back down: the pin came good, or it was answered, or
// the request ended. It is a POST and never an absence, for the reason
// [phaseClock.done] is: a question left on the screen for a request that is
// over is worse than one that was never asked.
func (p *phaseClock) withdrew() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ask == "" {
		return
	}
	p.ask = ""
	now := p.now()
	if p.phase == PhaseAsking {
		p.phase, p.since, p.tokens = PhaseFirstWord, now, 0
	}
	p.say("", now)
}

// allSlow is the visible half of [control.Report]: there is nowhere better to go
// and the wait is real.
//
// IT MOVES THE PHASE AND KEEPS THE CLOCK, and the pair is deliberate. The phase
// moves because a person reads a sentence rather than a field — a surface that
// had to notice a detail riding some other phase would be a surface inferring
// what it was told, which is the defect this whole file exists to end. The clock
// stays because nothing about the wait restarted: what changed is what this
// build now knows about it.
//
// detail is the controller's own word for why, empty when the sentence the
// surface owns says all there is. It never replaces the phase.
func (p *phaseClock) allSlow(detail string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	// The wait's own moment survives: [phaseClock.enter] would restart it.
	p.phase = PhaseAllSlow
	p.say(detail, now)
}

type phaseClockContextKey struct{}

// withPhaseClock puts one request's clock where every layer of it can find the
// same one.
//
// ONE REQUEST IS ONE STORY, and a raced request is still one request. The two
// arms of a hedge are two runs of the stream loop; if each made its own clock
// the surface would be told "writing" by the arm nobody is hearing while the
// other one was still waiting for its first word.
func withPhaseClock(ctx context.Context, clock *phaseClock) context.Context {
	if clock == nil {
		return ctx
	}
	return context.WithValue(ctx, phaseClockContextKey{}, clock)
}

// phaseClockFrom is the clock this request is already keeping, nil when it is
// the outermost call.
func phaseClockFrom(ctx context.Context) *phaseClock {
	clock, _ := ctx.Value(phaseClockContextKey{}).(*phaseClock)
	return clock
}
