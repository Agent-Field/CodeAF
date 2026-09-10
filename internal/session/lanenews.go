package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── TELLING A SURFACE WHAT THE LANES DID ────────────────────────────────────
//
// A model id is an address; the LANE is the machine behind it. Which one
// answered, whether a rescue went out while you were waiting, and which of the
// two finished are three facts a surface draws and cannot see: a stream belongs
// to internal/provider and a turn belongs to this package.
//
// SO THE NEWS IS PUSHED AND THE ARROW POINTS THE RIGHT WAY. internal/tui3
// imports this package, so this package may not import it (docs/ARCHITECTURE.md
// Decision 10's table, and the compiler). A surface registers a reader at open
// and this package calls it; a build with no surface registers nothing and the
// calls cost one nil check per answer.
//
// It is a package-level seam rather than a field on the agent for the same
// reason [provider.LastServed] is one: the surface that draws a status line
// does not hold the agent that ran the errand behind it, and a per-agent hook
// would have to be re-registered every time /new mints a session.
//
// TWO POSTS FOR A RESCUED ANSWER, ONE FOR EVERY OTHER. The middle state — a
// second request is out and nobody has committed — is the only interesting
// state of a hedge, and it is over before the call returns, so it cannot be
// reported from the answer. It arrives from [provider.HedgeReport.OnHedgeStart]
// instead, and it is the one place this build calls an answer slow.
//
// AND THAT SEAM CARRIES WHY, NOT ONLY WHO. It used to hand over a lane name and
// nothing else, so a lane that was LATE and a lane that had REFUSED the model
// outright arrived here identical and were drawn identically — `· slow · trying
// nextbit…` for a 404 that said the machine could not serve the model at all
// (issue #266). [provider.RescueNews] carries the transport's own
// classification, and a rescue that itself fails arrives a second time to
// withdraw the sentence the first one put on the screen.

// LaneNews is one answer's lane story, as the layer that sent it knows it.
//
// Anything unknown is left zero, and a zero field draws nothing — the emptiness
// law, said at the seam rather than at the surface, so that no reader has to
// invent a figure to have something to print.
type LaneNews struct {
	// Model is the model the answer came back on. A news with no model belongs
	// to nobody and is dropped.
	Model string
	// Lane is the machine the request WENT TO, and Winner the machine that
	// finished it. They differ only when a rescue landed, which is the whole of
	// what a surface means by "rescued".
	Lane   string
	Alt    string
	Winner string

	// TTFT is the wait before the first token and Rate how fast the answer was
	// written, both as this session timed them on its own stream.
	TTFT time.Duration
	Rate float64

	// Hedged says a second request went out for this answer; Trying says one is
	// out RIGHT NOW and nobody has committed yet.
	Hedged bool
	Trying bool

	// Reason is why the rescue went out, in the transport's own two words —
	// [provider.RescueSlow] or [provider.RescueRefused]. Empty is a rescue
	// nobody classified, which a surface reads as slow.
	//
	// AND ONE WORD THAT IS NOT ABOUT A RESCUE AT ALL. [provider.RescueRetired]
	// is a person's own pin, refused by the router for this model and stood
	// down until they pin again (issue #456). It rides this seam because it is
	// the same KIND of sentence — something about the machine behind this
	// answer changed while you were waiting — and the fact also reaches the
	// conversation as a note of its own, which is the copy that stays.
	//
	// IT IS CARRIED AND NEVER DECIDED HERE. The word a person reads has to be
	// the word the ledger acted on, and a second opinion formed at this seam is
	// how a status line ends up disagreeing with the routing it is describing.
	Reason string
	// Failed WITHDRAWS a claim this seam already made: the lane named in Alt is
	// the one a `trying X…` was about, and it has now failed. A surface that
	// went on drawing the promise would be telling somebody about a request
	// that is over.
	Failed bool

	// Role is who the answer was for (internal/lane's roles.go). A surface
	// draws only the roles a person is reading: a naming errand and a memory
	// reflex both answer during an ordinary talk turn, and a status line that
	// took the lane and the rate from whichever of them finished last was
	// telling somebody about a machine that had nothing to do with the answer
	// they were waiting for.
	Role lane.Role

	At time.Time

	// Session is the conversation this answer belongs to, and it is EMPTY IN
	// EVERY BUILD THAT NEEDS NO ANSWER: one process with one window has nothing
	// to disambiguate. An engine that is a separate process from its surfaces
	// (internal/enginehost) reads it to decide which connection this sighting
	// belongs on — see [Agent.newsKey].
	Session string

	// Relayed says this news arrived over a connection from the engine that
	// produced it, rather than off this process's own stream. It is
	// [provider.PhaseNews.Relayed]'s twin and exists for its reason: a build
	// that is both serving and watching must not forward what it just received
	// back out of the door it came in.
	Relayed bool
}

var (
	laneNewsMu     sync.RWMutex
	laneNewsReader func(LaneNews)
)

// OnLaneNews registers the reader every answer's lane story is told to, and
// hands back the one that was there — so a surface that opens over another can
// put it back when it closes. A nil function unregisters.
func OnLaneNews(fn func(LaneNews)) (previous func(LaneNews)) {
	laneNewsMu.Lock()
	defer laneNewsMu.Unlock()
	previous, laneNewsReader = laneNewsReader, fn
	return previous
}

// postLaneNews tells whoever is listening. It never blocks on a reader that is
// slow and never panics on one that is not there: a measurement must not be
// able to break the turn it measured.
func postLaneNews(news LaneNews) {
	if news.Model == "" {
		return
	}
	laneNewsMu.RLock()
	reader := laneNewsReader
	laneNewsMu.RUnlock()
	if reader == nil {
		return
	}
	if news.At.IsZero() {
		news.At = time.Now()
	}
	reader(news)
}

// laneNewsFrom turns one finished request into the news a surface draws.
//
// THE PRIMARY IS THE LANE THE REQUEST WENT TO AND THE WINNER IS THE ONE THAT
// ANSWERED, which is only worth spelling out because on an un-hedged answer
// they are the same machine and the report names neither: what a plain answer
// knows is the endpoint that served it, which this session already recorded.
func laneNewsFrom(model string, facts laneFacts, report *provider.HedgeReport) LaneNews {
	news := LaneNews{
		Model:  model,
		Lane:   facts.Lane,
		TTFT:   facts.TTFT,
		Hedged: facts.Hedged,
	}
	if facts.Gen > 0 && facts.Output > 0 {
		news.Rate = float64(facts.Output) / facts.Gen.Seconds()
	}
	if !facts.Hedged {
		return news
	}
	winner, loser := report.Lanes()
	news.Winner = winner
	if primary := report.Primary(); primary != "" {
		news.Lane = primary
	}
	// The alternative is whichever of the pair was not the one asked first. A
	// hedge that won is named by the winner; a hedge that lost is the loser.
	news.Alt = winner
	if news.Alt == news.Lane {
		news.Alt = loser
	}
	return news
}

// tellLaneNews posts one finished answer's story. It is called beside the
// witness's own fold, which is the same grain — one response — and the only
// place both the timings and the report are known at once.
func (a *Agent) tellLaneNews(model string, facts laneFacts, report *provider.HedgeReport) {
	if facts.Lane == "" && !facts.Hedged {
		// AN ANSWER THAT COULD NOT SAY WHICH MACHINE WROTE IT TEACHES NOTHING
		// AND DRAWS NOTHING. It is the attribution law the ledger keeps, said
		// once more where a person would have read the result of breaking it.
		return
	}
	news := laneNewsFrom(model, facts, report)
	news.Role = a.laneRole()
	news.Session = a.newsKey()
	postLaneNews(news)
}

// watchLaneRescue arms the slot so that a rescue is reported WHILE IT IS OUT,
// which is the only moment the sentence is worth saying.
func (a *Agent) watchLaneRescue(model string, report *provider.HedgeReport) {
	report.OnHedgeStart(a.laneRescueStarted(model))
}

// laneRescueStarted is what a rescue's start — and its death — becomes. It is a
// named function rather than a literal so that the sentence a person reads can
// be asserted without staging a slow lane and a race to produce it; the race
// itself is proved in internal/provider, which is the layer that owns one.
//
// A FAILED RESCUE IS NOT A RESCUE IN FLIGHT, which is the whole of why Trying
// is the negation of Failed here rather than always true: the news that arrives
// when an arm dies exists to take a promise off the screen, and posting it with
// Trying set would put the promise back.
func (a *Agent) laneRescueStarted(model string) func(provider.RescueNews) {
	return func(news provider.RescueNews) {
		postLaneNews(LaneNews{
			Model:   model,
			Alt:     news.Alt,
			Reason:  news.Reason,
			Failed:  news.Failed,
			Trying:  !news.Failed,
			Role:    a.laneRole(),
			Session: a.newsKey(),
		})
	}
}

// ── THE KEYSTROKE THAT BUYS A MEASUREMENT ───────────────────────────────────

// laneProber is the OPTIONAL half of a [Completer]: a transport that can buy a
// one-token measurement of the machines behind a model.
//
// It is a second interface rather than a method on Completer for [modelChain]'s
// reason: probing is a thing only the real adapter has, and a completer that
// does not offer one makes the capability ABSENT rather than present and
// failing. A test double, a build wired to no router, a session over a
// connection — all of them simply never probe.
type laneProber interface {
	ProbeLanes(ctx context.Context, model string)
}

// Typing says a person has started writing a turn.
//
// IT IS THE ONLY THING IN THIS PACKAGE A KEYSTROKE REACHES, and it exists
// because of what a keystroke KNOWS: seconds before a request is made, roughly
// where it will go. A one-token request to the two lanes at the head of the
// frontier costs about two hundredths of a cent, lands in the ledger as a
// measurement of OUR path taken NOW rather than everybody's average over half
// an hour, and warms the connection so the real request's first token is not
// also paying for a handshake (internal/lane's probe.go).
//
// NOTHING WAITS FOR IT AND NOTHING CAN BE MADE SLOWER BY IT. It returns before
// anything is sent, the debounce is the prober's own — at most one pair every
// twenty seconds per model — and every gate that could refuse is asked inside
// it. A surface may therefore call this on every character typed, which is what
// internal/tui3 does, because a first keystroke is not a thing a composer can
// tell apart from a fifth.
//
// It is silent for a session with a task's posture: a node's turn is not
// somebody typing, and an errand's pane is not the room they are sitting in.
func (a *Agent) Typing() {
	if a == nil || a.config.InTask || a.config.Errand {
		return
	}
	// The speed guard off is a person saying this build may not spend extra to
	// keep an answer moving; a probe is the cheapest of those spends and it is
	// still one of them. The transport asks the same question of its own gate —
	// this one is here so that a guard that is off costs no allocation at all.
	if !provider.LaneGuardOn() {
		return
	}
	prober, ok := a.client.(laneProber)
	if !ok {
		return
	}
	a.mu.Lock()
	model, closed := a.model, a.closed
	a.mu.Unlock()
	if closed || strings.TrimSpace(model) == "" {
		return
	}
	prober.ProbeLanes(a.probeContext(), model)
}

// probeContext is the context a probe rides: the session's own, so that a
// window being closed stops the probes it started. It is deliberately not a
// turn's context — a probe outlives the keystroke that bought it and belongs to
// no turn at all.
func (a *Agent) probeContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.laneCtx != nil {
		return a.laneCtx
	}
	return context.Background()
}

// ── WHAT A SECOND OF THIS TURN'S WAIT IS WORTH ──────────────────────────────

// turnLambda is λ for this turn, in seconds per dollar: how many seconds of
// waiting one dollar is worth buying out of.
//
// See the call site in loop.go for the argument. In one line: a conversation's
// turn is worth a person's attention, a task node is worth a person's attention
// while a person is here to read what it lands, and nothing at all when they
// are not — and the three durations [lane.Lambda] takes after the first are the
// plan graph's to supply, which no build yet does.
// IT IS ASKED OF THE ROLE AND NEVER COMPUTED HERE (internal/lane's roles.go).
// The two answers this function used to give — a conversation is worth a
// person's attention, a node is worth it while somebody is here — are now two
// rows of a table that also decides the quality bar, the exploration horizon
// and whether the status line is this call's to move. Four numbers that must
// agree about one errand, computed at one site each, is four places for the
// next fix to land in only one of.
func (a *Agent) turnLambda() float64 { return a.laneRole().Lambda() }
