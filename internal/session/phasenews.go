package session

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── THE PHASE CLOCK, FORWARDED AND EXTENDED ─────────────────────────────────
//
// internal/provider knows what a REQUEST is doing — connecting, waiting for the
// first word, thinking, writing, paced, trying again, switching lanes. This
// package knows what a TURN is doing between requests — running a tool,
// checking an answer, tidying the transcript, writing down a turn that is being
// handed over — and those waits are longer than any of the provider's: a `go
// test` runs for minutes, and the route judge that reads a finished answer has
// been measured at a quarter of an hour.
//
// Both halves are one story to the person watching, so they leave by one door.
// internal/tui3 imports this package and this package imports internal/provider,
// so the arrow points one way the whole distance (docs/ARCHITECTURE.md Decision
// 10): a surface registers [OnPhaseNews], this package forwards everything the
// transport posts and adds its own, and a build with no surface registers
// nothing and pays one nil check.
//
// THE VOCABULARY IS THE TRANSPORT'S, said once (internal/provider's phase.go).
// Two packages with two spellings of "thinking" is two surfaces that disagree
// the first time one of them is corrected.

// PhaseNews is one moment of one turn's life. It is [provider.PhaseNews] under
// this package's own name, so a surface imports one package rather than two.
type PhaseNews = provider.PhaseNews

// The phases a turn has that a request does not. They are spelled in
// internal/provider for the reason above — one vocabulary — and named here so a
// reader of this package can see the whole list in one place.
//
// EVERY ONE OF THEM IS HELD OPEN UNTIL IT ENDS, AND IT SAYS ITSELF WHILE IT
// LASTS. A surface stops drawing a phase it has not heard again for
// [provider.PhaseWindow], and a request's own clock beats inside that window
// because a stream gives it something to beat on; a turn's phases have no
// deltas, so this package beats for them ([Agent.tellPhase] and the heart
// below). It did not, once, and the measured cost of that was a route judge
// that ran for a quarter of an hour and drew for fifteen seconds of it.
const (
	PhaseRunning     = provider.PhaseRunning
	PhaseChecking    = provider.PhaseChecking
	PhaseTidying     = provider.PhaseTidying
	PhaseBriefing    = provider.PhaseBriefing
	PhaseTakingStock = provider.PhaseTakingStock
)

var (
	phaseMu     sync.RWMutex
	phaseReader func(PhaseNews)
	// phasePrevious is the transport's reader as it was before this package
	// took it over, so that closing a surface really does put it back.
	phasePrevious  func(PhaseNews)
	phaseInstalled bool
)

// OnPhaseNews registers the reader every phase change is told to and hands back
// the one that was there. A nil function unregisters, and takes this package's
// forwarder off the transport with it.
func OnPhaseNews(fn func(PhaseNews)) (previous func(PhaseNews)) {
	phaseMu.Lock()
	previous, phaseReader = phaseReader, fn
	install := fn != nil && !phaseInstalled
	remove := fn == nil && phaseInstalled
	if install {
		phaseInstalled = true
	}
	if remove {
		phaseInstalled = false
	}
	phaseMu.Unlock()
	if install {
		phaseMu.Lock()
		phasePrevious = provider.OnPhase(forwardPhase)
		phaseMu.Unlock()
	}
	if remove {
		phaseMu.RLock()
		restore := phasePrevious
		phaseMu.RUnlock()
		provider.OnPhase(restore)
	}
	return previous
}

// forwardPhase carries a request's phase up to the surface unchanged. It is a
// named function rather than a literal so that what the transport is holding
// can be compared against what this package installed.
//
// IT IS ALSO WHERE AN OFFER IS NOTICED, because an offer IS a phase: the pinned
// lane a person is waiting on has gone quiet, and the wait is one they can end.
// Noticing it here rather than on a second channel is the whole of why it rides
// this one ([noteOffer]).
func forwardPhase(news PhaseNews) {
	// THE MOMENTS ARE SETTLED BEFORE EITHER READER SEES THEM. [postPhaseNews]
	// fills them in for the surface anyway; the offer desk needs the same two,
	// and an offer whose start was a zero would have lapsed before it was drawn.
	if news.At.IsZero() {
		news.At = time.Now()
	}
	if news.Since.IsZero() {
		news.Since = news.At
	}
	noteOffer(news)
	postPhaseNews(news)
}

// ── A WAIT A PERSON CAN END ─────────────────────────────────────────────────
//
// A pinned lane is a person's instruction, and an instruction is asked rather
// than overridden. So where an unpinned call would quietly race somebody else,
// a pinned one raises an OFFER — `coreweave is slow · switch to auto? (y)` —
// and the answer fires the same rescue the controller would have fired, to the
// lane the frontier already named. There is no second choice made at the worst
// possible moment, and the pin itself is untouched: accepting is for THIS
// answer, and the next request goes to the pinned machine, because that is what
// a pin means.
//
// IT RIDES THE PHASE CHANNEL THAT ALREADY EXISTS. A second channel for one
// sentence is a second thing to keep alive, and the two would disagree the first
// time one of them was fixed.
//
// THE OFFER IS WITHDRAWN BY ANYTHING THAT ANSWERS IT: a visible token, which
// makes the question moot; the phase ending; and the window lapsing, so a
// question about a request that is over never sits on a screen. A thinking
// delta does NOT withdraw it — nothing has arrived that a person can read.

// PhaseAsking is a wait a person can end.
//
// IT IS SPELLED HERE AND IN internal/provider, ONCE EACH, WHILE TWO LANES OF
// WORK LAND. The transport owns the vocabulary and W3 adds the constant there;
// this is the same word, so that the surface and the engine can be written and
// tested against it before the two halves meet. Joining them is a rename with no
// behaviour in it.
const PhaseAsking = provider.Phase("asking")

// PhaseAllSlow is the visible half of a report: every reachable lane is believed
// slow, so acting buys nothing and the only honest act left is to SAY the wait
// is real. Silence is never an option; a wait that is real is reported.
const PhaseAllSlow = provider.Phase("all lanes slow")

// offerAnswerer is the transport's side of an open offer.
//
// IT IS AN INTERFACE AND NOT A CALL because the two halves are built in
// parallel: internal/provider owns the registry of open offers and their
// expiry, and this package owns the model on screen and the person answering.
// One method, one direction, and the thing installed is a package that this one
// already imports — so the join is one line and there is no seam to keep alive
// afterwards.
type offerAnswerer interface {
	// AnswerOffer answers the offer named by an opaque token, and reports
	// whether one was still open to answer.
	AnswerOffer(ask string, yes bool) bool
}

// openOffer is one live question and the moment it was raised.
type openOffer struct {
	ask string
	at  time.Time
}

var offers = struct {
	mu       sync.Mutex
	open     map[string]openOffer
	answerer offerAnswerer
}{open: map[string]openOffer{}}

// SetOfferAnswerer installs the side that owns the open offers, and hands back
// the one that was there. A nil answerer uninstalls it, which is the state a
// build with no transport wired is in.
func SetOfferAnswerer(answerer offerAnswerer) (previous offerAnswerer) {
	offers.mu.Lock()
	defer offers.mu.Unlock()
	previous, offers.answerer = offers.answerer, answerer
	return previous
}

// noteOffer opens, keeps or withdraws one model's offer from the phase it is in.
func noteOffer(news PhaseNews) {
	model := strings.TrimSpace(news.Model)
	if model == "" {
		return
	}
	offers.mu.Lock()
	defer offers.mu.Unlock()
	if news.Phase != PhaseAsking {
		// EVERY OTHER PHASE IS AN ANSWER TO THE QUESTION. The first visible
		// token, the request ending, a rescue that went out anyway: all of them
		// mean the offer is moot, and a question a person can no longer act on
		// is worse than no question at all.
		delete(offers.open, model)
		return
	}
	// A HELD OFFER KEEPS ITS OWN MOMENT. The phase says itself again while it
	// lasts, and an offer whose clock restarted on every beat would never lapse.
	if held, open := offers.open[model]; open && held.ask == askOf(news) {
		return
	}
	offers.open[model] = openOffer{ask: askOf(news), at: news.Since}
}

// askOf is the opaque token naming the offer this news carries.
//
// THE TOKEN IS THE REQUEST'S IDENTITY AND NOT ITS WORDS. W3 adds `Ask` to
// [provider.PhaseNews] in parallel with this lane, so the read is in ONE
// function and the join is its body: `return news.Ask`. Until then an offer is
// named by the request it belongs to — the model, and the moment the phase
// began — which is unique for exactly as long as an offer lives.
func askOf(news PhaseNews) string {
	return news.Model + "@" + strconv.FormatInt(news.Since.UnixNano(), 10)
}

// liveOffer is the open offer for one model, false when there is none or when
// the one on the desk has lapsed. It clears what it finds either way: an offer
// is answered once, because a second answer to one question is a second rescue
// nobody asked for.
func liveOffer(model string, now time.Time) (openOffer, bool) {
	offers.mu.Lock()
	defer offers.mu.Unlock()
	offer, open := offers.open[strings.TrimSpace(model)]
	if !open {
		return openOffer{}, false
	}
	delete(offers.open, strings.TrimSpace(model))
	if now.Sub(offer.at) > provider.PhaseWindow {
		return openOffer{}, false
	}
	return offer, true
}

// forgetOffers empties the desk. It is for tests, which must not inherit one
// another's questions.
func forgetOffers() {
	offers.mu.Lock()
	defer offers.mu.Unlock()
	offers.open = map[string]openOffer{}
}

// AnswerLaneOffer answers the offer standing over this conversation's model,
// and reports whether there was one to answer.
//
// IT IS THE ONLY DOOR THE SURFACE HAS, and it takes the answer rather than the
// question: which lane the rescue goes to was decided when the offer was raised,
// from the frontier that was already computed, because the moment a rescue is
// wanted is the worst possible moment to start choosing one.
func (a *Agent) AnswerLaneOffer(yes bool) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	offer, open := liveOffer(model, time.Now())
	if !open {
		return false
	}
	offers.mu.Lock()
	answerer := offers.answerer
	offers.mu.Unlock()
	if answerer == nil {
		return false
	}
	return answerer.AnswerOffer(offer.ask, yes)
}

// postPhaseNews tells whoever is listening. It never blocks on a slow reader
// and never panics on an absent one, for the reason [postLaneNews] does not: a
// measurement must not be able to break the turn it measured.
func postPhaseNews(news PhaseNews) {
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

// ── THE BEAT: A STAGE THAT IS STILL RUNNING IS STILL DRAWN ──────────────────
//
// THE DEFECT THIS FIXES, measured: the route judge at the end of a turn — one
// `tellPhase(PhaseChecking, …)` in loop.go — ran for a quarter of an hour and
// the screen drew it for fifteen seconds. A surface drops a phase it has not
// heard again for [provider.PhaseWindow] (internal/tui3's phaseWindow), which
// is the right rule and the only thing standing between a person and a clock
// that runs forever after a goroutine was killed. What was missing was the
// other half of that bargain: something to keep saying a phase that is STILL
// TRUE.
//
// A REQUEST HAS DELTAS TO BEAT ON AND A TURN HAS NOTHING. internal/provider's
// clock re-says its phase off the stream itself, once a second, because a
// stream is arriving; the waits in THIS package are a tool running, a gate
// reading, a compaction, a brief being written, a mark being read — long
// silences with no event inside them at all. So the beat is a timer, and it is
// ONE timer that every holder gets by holding a phase, rather than a workaround
// per site. The first workaround is already gone: the briefing stage used to
// post itself twice, once per model call, and said so in its own comments
// (checkpoint.go).

// phaseHeldBeat is how often a phase this package is holding open says itself
// again while it lasts.
//
// IT IS DERIVED FROM THE WINDOW AND NEVER WRITTEN DOWN BESIDE IT. How long a
// phase describes the present is a contract between whoever posts one and
// whoever draws one, spelled once in [provider.PhaseWindow]; a beat with its own
// hand-written number is a stage that goes dark the first time somebody tunes
// the window. A third of the window means two beats may be lost — to a busy
// frame, a machine under load, a stop-the-world pause — before anything a person
// is reading blinks.
//
// It is a var rather than a const for exactly one reason: the tests that prove a
// stage outlasting the window keeps drawing shorten it, and a test that waited
// five real seconds per beat would cost more than the defect it guards.
var phaseHeldBeat = provider.PhaseWindow / 3

// phaseHeart is the one phase an agent is holding open and the beat that keeps
// saying it.
//
// ONE AGENT HOLDS ONE PHASE, which is not a simplification of the machinery but
// a description of it: the turn loop is sequential, [Agent.tellPhase] replaces
// whatever was open, and [Agent.endPhase] clears it. That is what lets the
// turn's own deferred end (loop.go) fire after a gate has already closed its own
// without anybody having to count them.
type phaseHeart struct {
	mu sync.Mutex
	// held is what is being said, zero when nothing is, and stop closes when it
	// is over. THE BEAT DIES WITH THE PHASE AND HAS NO OTHER WAY OUT: a beat
	// that outlived what it was saying would put a finished stage back on the
	// screen, which is the same defect as a stale phase and harder to see.
	held PhaseNews
	stop chan struct{}
}

// tellPhase is how this package says what a turn is doing between requests, and
// goes on saying it for as long as it is true.
//
// It carries the agent's own model and role so that a surface can tell a
// conversation's wait from a task node's, and so that a side errand's phase
// never takes the clock away from the answer somebody is reading.
//
// EVERY CALL IS PAIRED WITH AN [Agent.endPhase], and the pairing is what stops
// the beat. Every site in this package does it with a `defer` or on each arm of
// a branch, and runTurn's own deferred end (loop.go) is the backstop that fires
// even on a panic.
func (a *Agent) tellPhase(phase provider.Phase, detail string, since time.Time) {
	if a == nil {
		return
	}
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	now := time.Now()
	if since.IsZero() {
		since = now
	}
	// SINCE AND AT ARE SETTLED HERE, not left for the post to fill in. The beat
	// re-says this exact news with a fresh At and the SAME Since, so a stage's
	// clock counts the whole stage; a held phase with no start on it would have
	// its clock reset to zero by every beat, which reads as a stage restarting
	// over and over rather than one that is lasting.
	news := PhaseNews{
		Phase:  phase,
		Since:  since,
		Detail: detail,
		Model:  model,
		Role:   a.laneRole(),
		At:     now,
	}
	a.phase.mu.Lock()
	defer a.phase.mu.Unlock()
	a.dropHeldPhaseLocked()
	stop := make(chan struct{})
	a.phase.held, a.phase.stop = news, stop
	postPhaseNews(news)
	guard.Go("phase beat", func() { a.beatHeldPhase(stop) })
}

// endPhase says the turn has stopped doing whatever it was doing, so a surface
// stops drawing a clock for work that is over. A stale phase left on the screen
// is exactly the defect the phase clock exists for, and it stops the beat in the
// same breath for the same reason.
func (a *Agent) endPhase() {
	if a == nil {
		return
	}
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	over := PhaseNews{Model: model, Role: a.laneRole()}
	a.phase.mu.Lock()
	defer a.phase.mu.Unlock()
	a.dropHeldPhaseLocked()
	postPhaseNews(over)
}

// dropHeldPhaseLocked forgets whatever was being held and ends its beat. It is
// called with the heart's lock held, from the only two places that may change
// what is being said.
func (a *Agent) dropHeldPhaseLocked() {
	if a.phase.stop != nil {
		close(a.phase.stop)
		a.phase.stop = nil
	}
	a.phase.held = PhaseNews{}
}

// beatHeldPhase re-says one held phase until it ends. It is the whole lifetime
// of the goroutine [Agent.tellPhase] spawns: it starts with a phase and it
// returns when that phase is over, and there is no other exit.
func (a *Agent) beatHeldPhase(stop chan struct{}) {
	beat := time.NewTicker(phaseHeldBeat)
	defer beat.Stop()
	for {
		select {
		case <-stop:
			return
		case <-beat.C:
			if !a.sayHeldPhaseAgain(stop) {
				return
			}
		}
	}
}

// sayHeldPhaseAgain re-says the phase this beat belongs to and reports whether
// there is still one to say.
//
// IT POSTS UNDER THE HEART'S OWN LOCK, and that is the whole of what makes the
// beat safe. [Agent.endPhase] clears under the same lock, so a beat and the end
// of a phase cannot cross: either the beat goes out and the clear lands after
// it, or the clear lands first and this returns having said nothing. A post
// taken outside the lock would sometimes arrive after the clear and put a
// finished stage back on the screen for a whole window. The post itself is a
// hand-off to a reader documented never to block ([postPhaseNews]), which is
// what makes holding a lock across it safe.
func (a *Agent) sayHeldPhaseAgain(stop chan struct{}) bool {
	a.phase.mu.Lock()
	defer a.phase.mu.Unlock()
	if a.phase.stop != stop || a.phase.held.Phase == "" {
		return false
	}
	news := a.phase.held
	news.At = time.Now()
	postPhaseNews(news)
	return true
}

// laneRole is what this agent's own calls are for. A conversation is talk; a
// task node is a leaf, and which of the two leaf roles it is depends on the
// only thing that changes what a second is worth — whether anybody is here to
// read what it lands (see [Agent.turnLambda]).
func (a *Agent) laneRole() lane.Role {
	if a == nil {
		return lane.RoleUnknown
	}
	if !a.config.InTask {
		return lane.RoleTalk
	}
	if someoneIsWatching() {
		return lane.RoleLeafAttached
	}
	return lane.RoleLeafUnattended
}
