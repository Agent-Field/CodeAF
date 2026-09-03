package provider

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── WHO SERVED, AND HOW FAST ────────────────────────────────────────────────
//
// A router fans one model over many endpoints. Two of them are a datacentre
// away with a warm replica; one is a machine somebody is also training on, and
// it answers the same request at a fifth of the speed. The model id says
// nothing about which one a request landed on, so a session that only names its
// model is describing a decision it did not make.
//
// AND THEY DO NOT CHARGE THE SAME. That was assumed here for a long time and it
// is simply false: an endpoint's tariff is its own, and the model id's published
// list price is a figure none of them is obliged to match. Asking for the
// fastest one and saying nothing about price is how a cost autopsy over 44
// bench cells found this surface paying ~3.5× list at identical token counts —
// see latencyPriceCeiling, which is the whole of the answer.
//
// This file is the half of the adapter that both ASKS for speed and CHECKS it.
// Asking is one object on the wire — the routing preferences below. Checking is
// the ledger: every completion is timed, the response says who served it, and
// an endpoint that keeps being slow is demoted and then, for a while, refused.
//
// Nothing here knows a vendor's name. The law reads only "who served, and how
// fast", and every name in it arrived from the wire a moment ago.

// RoutingStrategy is how a session asks the router to choose among the
// endpoints serving one model.
//
// It is a CHOICE and not a bool because the two live answers are not opposites:
// latency wants the fastest endpoint, price wants the cheapest, and they
// routinely disagree. Off is the third answer and it is a real one — it sends
// no preference object at all, which is what an operator behind a gateway that
// does not speak this dialect needs.
type RoutingStrategy string

const (
	// RoutingLatency asks for the currently-fastest endpoint UNDER A PRICE
	// CEILING (see latencyPriceCeiling). It is what a call with nobody's row
	// written and a person waiting on it gets, because a chat session is a
	// person waiting — and the ceiling is there because being served fastest was
	// never worth being charged anything.
	RoutingLatency RoutingStrategy = "latency"
	// RoutingPrice asks for the cheapest endpoint that can serve the request.
	RoutingPrice RoutingStrategy = "price"
	// RoutingOff sends no preference object, and switches the ledger off with
	// it: an operator who has not asked to be routed has not asked to be
	// measured either, and a demotion nobody can act on is only overhead.
	RoutingOff RoutingStrategy = "off"
)

// sortWord is the strategy as the router spells it, empty when no preference
// object should be sent at all.
func (s RoutingStrategy) sortWord() string {
	switch s {
	case RoutingPrice:
		return "price"
	case RoutingOff:
		return ""
	default:
		return "latency"
	}
}

// ── WHO IS WAITING ──────────────────────────────────────────────────────────
//
// Sorting by latency is a decision about a PERSON, not about a model: it is
// worth something only when somebody is sitting there watching the answer
// arrive. A task worker, a divided part, an auditor, a judge, a title, a memory
// pass — nobody is waiting on any of those, and pinning the fastest endpoint
// for them buys nothing and pays whatever that endpoint charges.
//
// So the request carries who is waiting, and this file is the one place that
// reads it.

// RoutingIntent says whether a person is waiting on this call.
type RoutingIntent int

const (
	// IntentInteractive is a call somebody is watching arrive. It is the zero
	// value, because a call that has said nothing about itself is the
	// conversation's own turn until something says otherwise.
	IntentInteractive RoutingIntent = iota
	// IntentBackground is a call nobody is waiting on. Speed is worth nothing
	// to it and price is worth everything.
	IntentBackground
)

type routingIntentContextKey struct{}

// WithRoutingIntent states who is waiting on the calls made under ctx.
//
// IT IS SAID AND NEVER INFERRED. "Nobody is watching this stream" is close to
// the answer but is not it: a tool that asks a model something takes the
// observer off the context and the person is still sitting there waiting for
// the turn it belongs to. Only the call site knows whether anybody is waiting,
// so only the call site may say — and a call that says nothing keeps the
// behaviour it has always had.
func WithRoutingIntent(ctx context.Context, intent RoutingIntent) context.Context {
	return context.WithValue(ctx, routingIntentContextKey{}, intent)
}

// RoutingIntentFrom answers who is waiting on the calls made under ctx,
// interactive when nothing said. It is the read half of [WithRoutingIntent],
// exported so a surface can assert what its own calls will ask for without
// standing up a router.
func RoutingIntentFrom(ctx context.Context) RoutingIntent {
	return routingIntentFrom(ctx)
}

// routingIntentFrom answers who is waiting on this call, interactive when
// nothing said.
//
// THE ROLE OUTRANKS THE INTENT WHERE BOTH ARE SAID (roles.go). A role is the
// more specific claim — it names the errand, and the table can explain every
// number derived from it — while the intent is a two-valued reading of the same
// fact that a call site had to remember to state. The intent stays because it
// is what `provider.sort` is built from and what a dozen sites still say; it is
// now DERIVED where a role is present rather than believed alongside it.
func routingIntentFrom(ctx context.Context) RoutingIntent {
	if intent, ok := roleIntent(ctx); ok {
		return intent
	}
	intent, _ := ctx.Value(routingIntentContextKey{}).(RoutingIntent)
	return intent
}

// ParseRoutingStrategy reads a settings word. An unrecognized word is NOT an
// error and NOT off: it falls back to the default, because a typo in a config
// row must not silently take routing away from a session that asked for it.
func ParseRoutingStrategy(word string) (RoutingStrategy, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case string(RoutingLatency):
		return RoutingLatency, true
	case string(RoutingPrice):
		return RoutingPrice, true
	case string(RoutingOff):
		return RoutingOff, true
	default:
		return RoutingLatency, false
	}
}

// RoutingSource answers which strategy is in force.
//
// It is an interface rather than a value on Config for one reason: the answer
// lives in a settings file, and an adapter that read one would put a disk read
// on the path of every client construction — in tests, in a leaf, in a
// subharness process that has no profile directory at all. The surface resolves
// the row once and hands the answer down; a test hands down [StaticRouting] and
// touches nothing.
type RoutingSource interface {
	RoutingStrategy() RoutingStrategy
}

type staticRouting RoutingStrategy

func (s staticRouting) RoutingStrategy() RoutingStrategy { return RoutingStrategy(s) }

// StaticRouting is one already-resolved answer as a source. The empty strategy
// is NOBODY HAVING CHOSEN rather than a refusal, so a caller that has nothing
// to say leaves the adapter to decide per request from who is waiting on it —
// see [Client.routingFor].
func StaticRouting(strategy RoutingStrategy) RoutingSource { return staticRouting(strategy) }

// routingChoice is the strategy A PERSON CHOSE, and whether one was chosen at
// all. An empty source, an empty word, or no source is "nobody said" — which is
// a different fact from "somebody said latency", and the whole of what lets the
// default below depend on who is waiting while an explicit row still wins.
func (c *Client) routingChoice() (RoutingStrategy, bool) {
	if c.config.Routing == nil {
		return RoutingLatency, false
	}
	strategy := c.config.Routing.RoutingStrategy()
	if strings.TrimSpace(string(strategy)) == "" {
		return RoutingLatency, false
	}
	parsed, _ := ParseRoutingStrategy(string(strategy))
	return parsed, true
}

// routing resolves the strategy for this client with nothing said about who is
// waiting. It is what the ledger's own gates read — they only ever ask whether
// routing is off — and it keeps the old answer: no row means latency.
func (c *Client) routing() RoutingStrategy {
	strategy, _ := c.routingChoice()
	return strategy
}

// routingFor resolves the strategy one request will actually ask for.
//
// THE PERSON'S ROW WINS OUTRIGHT. Everything below it is the DEFAULT moving
// with who is waiting: the conversation's own turn chases speed, and a call
// nobody is sitting in front of chases price.
func (c *Client) routingFor(intent RoutingIntent) RoutingStrategy {
	if chosen, ok := c.routingChoice(); ok {
		return chosen
	}
	if intent == IntentBackground {
		return RoutingPrice
	}
	return RoutingLatency
}

// providerPrefs is the routing preference object.
//
// Sort is the standing ask. Order and Ignore are the ledger's two verdicts, and
// they are only ever populated from endpoints this process has itself timed —
// see [velocityLedger.preferences].
//
// AllowFallbacks and RequireParameters are pointers so that "true" is written
// on the wire rather than assumed. The first keeps every veto here advisory: an
// endpoint we demoted is still reachable when the ones we prefer are down, so a
// slow answer always beats no answer. The second is the guard on the reasoning
// knob — a request carrying `reasoning` must not be routed to an endpoint that
// silently drops it, because a planning call that was supposed to think and did
// not is a wrong answer rather than a slow one.
type providerPrefs struct {
	Sort  string   `json:"sort,omitempty"`
	Order []string `json:"order,omitempty"`
	// Only is a DEMAND rather than a ranking: the request goes to exactly these
	// lanes or it does not go at all. It is what a pin sends, and what the
	// second request of a hedged pair sends so that the pair cannot both land
	// on the lane that is already stalling (hedge.go).
	Only              []string  `json:"only,omitempty"`
	Ignore            []string  `json:"ignore,omitempty"`
	AllowFallbacks    *bool     `json:"allow_fallbacks,omitempty"`
	RequireParameters *bool     `json:"require_parameters,omitempty"`
	MaxPrice          *maxPrice `json:"max_price,omitempty"`
}

// maxPrice is the ceiling an endpoint's own tariff must sit under to serve this
// request. The router spells the numbers in US DOLLARS PER MILLION TOKENS,
// which is a million times the unit the catalog publishes; the conversion is
// [Client.priceCeiling]'s and is done in exactly one place.
//
// Neither field is omitempty. A model whose list price really is zero — the
// free variants a router publishes — gets a ceiling of zero, and that is the
// honest ask rather than a missing one; dropping it would quietly send `{}` and
// mean the opposite.
//
// IT CANNOT EXPRESS A CACHE-READ CEILING. The router's field takes `prompt`,
// `completion`, `request` and `image` and nothing else, so the 4.0× the cost
// autopsy measured on CACHED tokens is bounded only indirectly, through the
// prompt ceiling that the same endpoint's tariff is derived from. That is a
// real limit of the mechanism and not an omission here.
type maxPrice struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
}

// latencyPriceCeiling is how far above a model's own published list price an
// endpoint may charge and still be worth choosing for speed.
//
// THE MEASUREMENT IT ANSWERS. A cost autopsy over 44 bench cells found this
// surface paying ~3.5× what the model's list price says the same token counts
// should cost: 1.4× list on uncached prompt tokens, 4.0× on cached ones (a
// ~44% cache discount where list promises 80%), and 1.9× on output. Nothing
// about the model or the answer differed. The whole gap was `sort: latency`
// asking the router for the fastest endpoint and then accepting whatever that
// endpoint charged, because the ask carried no ceiling at all.
//
// 1.25 IS THE HONEST BOUND, and the reasoning is about a person rather than
// about a number. An endpoint 25% over list is buying a latency edge somebody
// can actually feel on a turn. An endpoint 4× over list is buying nothing a
// person notices on a five-minute task — the answer arrives while they are
// still reading the last one either way — so there is no version of "a chat
// session is a person waiting" that justifies paying it.
const latencyPriceCeiling = 1.25

// priceCeiling is the ceiling one request carries, nil when there is none to
// carry.
//
// ABSENCE, NEVER A GUESS. A model the catalog has no published price for — a
// row that never loaded, a slug the router calls "it depends", a catalog still
// warming — sends no ceiling and routes exactly as it did before. A ceiling
// invented from a neighbouring model's price would be this process quietly
// refusing endpoints on a number nobody published.
func (c *Client) priceCeiling(model string) *maxPrice {
	if c.config.ModelPrice == nil {
		return nil
	}
	prompt, completion, known := c.config.ModelPrice(normalizeModel(model))
	if !known || prompt < 0 || completion < 0 {
		return nil
	}
	const perMillion = 1_000_000
	return &maxPrice{
		Prompt:     prompt * perMillion * latencyPriceCeiling,
		Completion: completion * perMillion * latencyPriceCeiling,
	}
}

// providerPreferences builds the object one request will carry, nil when none
// should be sent.
//
// It is called from the encoder, which runs immediately before the send, and
// that timing is the point: the demotion a laggy answer earned thirty seconds
// ago applies to the request now being written, without anything having to
// carry it forward.
//
// IT IS THE DECISION SITE OF ISSUE #433, and the gate on it is what the BASE
// ANSWERED rather than what its hostname says. The field is a router's dialect
// and an endpoint that is not a router either ignores it or 400s on it — but
// which of the two a base does is a thing only the base can say, so it is asked
// once and remembered, and only a base that has answered "no" is left off
// (prefcarry.go's [Client.carriesPreferences]).
//
// ── AND AN UNASKED BASE SENDS ONLY WHAT A PERSON ASKED FOR ──────────────────
//
// THE LAW IS ABOUT A PREFERENCE THE PERSON HAS, NOT ABOUT THIS ADAPTER'S OWN
// DEFAULT KNOBS. `sort`, `allow_fallbacks` and `require_parameters` are nobody's
// instruction: they are how this build asks a ROUTER to break a tie among
// machines it already knows about, and putting them on a plain endpoint's every
// request would be a field that every plain-base user suddenly carries, for a
// tie there is nothing to break. So on a base that has not yet SHOWN it carries
// a preference, the object goes out only when there is something to ask WITH,
// and there is exactly one such thing: a lane the person pinned. A ranking
// cannot be the reason, because a ranking only exists once a sheet arrived —
// and a sheet arriving is the base proving it carries.
//
// The consequence, stated so nobody has to derive it: a plain base with nobody
// pinning anything is never asked, never answers, and its requests are
// byte-for-byte the requests it got before this law existed. The moment somebody
// pins a lane, that pin IS the asking.
func (c *Client) providerPreferences(model string, knobs callKnobs, request *ai.Request) *providerPrefs {
	if !c.carriesPreferences() {
		return nil
	}
	if !c.prefsProven() && c.pinnedLaneFor(model) == "" {
		return nil
	}
	strategy := c.routingFor(knobs.intent)
	word := strategy.sortWord()
	if word == "" {
		return nil
	}
	yes := true
	prefs := &providerPrefs{Sort: word, AllowFallbacks: &yes, RequireParameters: &yes}
	if strategy == RoutingLatency && !(c.velocity != nil && c.velocity.ceilingRefused(model)) {
		// The ceiling rides the latency ask and only the latency ask. Sorting by
		// price is already asking for the cheapest thing available, and a ceiling
		// on top of it could only ever take endpoints away without changing which
		// one is chosen.
		//
		// AND IT IS NOT SENT TWICE TO A MODEL THAT REFUSED IT. The ladder
		// (endpoints.go) drops the ceiling on its first rung and the call lands,
		// but a ladder is a recovery, not a routing policy: without the ledger's
		// memo every call to that model would pay a 404 round trip before doing
		// any work. The first refusal teaches the process and the second call
		// is shaped right from the start.
		prefs.MaxPrice = c.priceCeiling(model)
	}
	if c.velocity != nil {
		order, ignore := c.velocity.preferences(model)
		prefs.Ignore = ignore
		// THE LEDGER'S ORDER IS A SPEED RANKING, and `provider.order` names what
		// to try FIRST — so sending it beside `sort: price` would put this
		// process's own fastest lane ahead of the cheapest one and quietly undo
		// the sort. A request that asked for price gets the refusals, which are
		// about endpoints that will not answer at all, and nothing that ranks.
		if strategy != RoutingPrice {
			prefs.Order = order
		}
	}
	// AND THE PIN GOES IN FRONT OF ALL OF IT (affinity.go). It is not a ranking
	// and that is why it travels under BOTH sort words where the ledger's order
	// may not: it names the one machine that already holds this lineage's prompt
	// prefix, and a cold prefix cost 4.7× a warm one at identical token counts —
	// more than any endpoint's tariff differs from another's, so the errand
	// nobody is waiting on wants its cache back exactly as much as the person
	// does. Its first request, having nothing pinned, still asks by price.
	//
	// It is a preference and never a demand: `allow_fallbacks` stays true above,
	// so an endpoint that is busy, gone, or over the ceiling simply does not
	// answer this one and the router picks by the sort word as before.
	held := c.heldEndpoint(knobs.cacheKey, model, prefs.Ignore)
	if held != "" {
		prefs.Order = append([]string{held}, withoutEndpoint(prefs.Order, held)...)
	}
	// AND THE BELIEF SPEAKS LAST (lanes.go). What `internal/lane` has measured
	// about these endpoints is the same question the ledger's order answers and
	// a better answer to it — a posterior per lane rather than three thresholds
	// — so when there is a belief its order replaces the ranking above. When
	// there is not, and on the first call of every fresh machine there is not,
	// nothing here changes and the request goes out exactly as it always did.
	c.applyLaneChoice(prefs, model, knobs, request, held)
	return prefs
}

// wirePreferences is the `provider` object THIS REQUEST ACTUALLY GOES OUT
// WITH: the ledger's own preferences, plus a rescue's demand, minus whatever
// the ladder has already taken off.
//
// IT IS NAMED ONCE BECAUSE TWO READERS HAVE TO AGREE ABOUT IT. The encoder
// writes the object (wire.go) and the ladder decides its first rung from it
// (endpoints.go's [Client.relaxationPlan]); a ladder reading only the ledger's
// half could not see the demand [hedgePreference] adds afterwards, so a pinned
// request was offered no first rung and climbed every other one still pinned to
// the machine that had refused it (issue #266).
func (c *Client) wirePreferences(model string, knobs callKnobs, request *ai.Request) *providerPrefs {
	if knobs.noProvider {
		// THE ONE ENCODE THAT ASKS THE OPPOSITE QUESTION. See [callKnobs] —
		// this is the widened retry that finds out whether a base's 400 was
		// about the field, and it can only find out by sending none.
		return nil
	}
	prefs := hedgePreference(c.providerPreferences(model, knobs, request), knobs)
	if knobs.relaxed.has(relaxEndpointFilter) {
		prefs = relaxedPreferences(prefs)
	}
	return prefs
}

// narrowing reports whether this preference object carries anything that can
// leave the router with NO endpoint to send to.
//
// It is the whole membership rule of the ladder's first rung, written once, so
// that the rung is offered exactly when it would do something
// ([Client.relaxationPlan]) and takes off exactly what it was offered for
// ([relaxedPreferences]). Two lists that had to agree were two lists that
// disagreed for a whole run: `only` could empty the set and was on neither.
func (p *providerPrefs) narrowing() bool {
	if p == nil {
		return false
	}
	return p.RequireParameters != nil || len(p.Ignore) > 0 || p.MaxPrice != nil ||
		len(p.Only) > 0 || p.AllowFallbacks != nil
}

// relaxedPreferences is the preference object with everything that can EXCLUDE
// an endpoint taken out of it, leaving only what orders the ones that remain.
//
// It is the first rung of the endpoint-refusal ladder (endpoints.go), and it is
// the one rung that costs the answer nothing: the model is asked the identical
// question, of a wider set of machines. A preference object with nothing left in
// it is dropped entirely rather than sent empty.
func relaxedPreferences(prefs *providerPrefs) *providerPrefs {
	if prefs == nil {
		return nil
	}
	relaxed := *prefs
	relaxed.RequireParameters = nil
	relaxed.Ignore = nil
	// The price ceiling is the third thing that can empty the endpoint set: a
	// model whose every endpoint charges above its own list price has no lane
	// left once the ceiling is applied, and "no endpoints found" is a worse
	// answer than a dear one. It comes off with the rest of the filter, and the
	// sort still asks for the fastest of whatever remains.
	relaxed.MaxPrice = nil
	// AND THE DEMAND COMES OFF WITH THEM, which is the fourth and was the one
	// that mattered. `only` names the machines this request may go to and
	// `allow_fallbacks: false` forbids any other — together they are the
	// narrowest filter this process ever sends, and the refusal they earn is
	// literally the router saying the set is empty. A ladder that dropped the
	// parameter filter and left the pin on climbed six rungs still pinned to
	// the machine that had said no (issue #266); the pin is what has to go, and
	// it goes first.
	//
	// A RESCUE THAT REACHES THIS RUNG HAS ALREADY LOST ITS ARGUMENT FOR THE
	// PIN. The demand exists so that a hedge cannot land on the lane that is
	// already stalling (hedge.go), and it is only ever relaxed on the LAST arm
	// — the one the race had no other machine to walk to — where the choice is
	// between a wider request and no answer at all.
	relaxed.Only = nil
	relaxed.AllowFallbacks = nil
	if relaxed.Sort == "" && len(relaxed.Order) == 0 {
		return nil
	}
	return &relaxed
}

// ServedEndpoint is a slot one caller opens to be told WHICH endpoint answered
// its calls.
//
// It exists because [LastServed] cannot answer that question honestly for a
// caller: the ledger's latest sighting is process-wide, and two task nodes
// running the same model concurrently would each read the other's endpoint. The
// slot is scoped to the context the caller stamped, so what it holds is always
// an answer to one of that caller's own requests.
//
// It holds the MOST RECENT answer and nothing else. A caller stamps it around a
// turn and reads it beside each response, which is the grain the journal writes
// at (internal/session's addUsage).
//
// AND IT COUNTS THE HOPS, because that is the fact a cost autopsy needs and the
// one nobody could see: eleven moves across six endpoints in a single 41-request
// turn, every one of them a cold prompt cache (affinity.go). A journal line that
// records the endpoint alone shows where a request landed; [ServedEndpoint.Hops]
// and [ServedEndpoint.Pinned] show whether it stayed.
type ServedEndpoint struct {
	mu   sync.Mutex
	name string
	// pinned is whether the last answer came from the endpoint its request had
	// asked to come back to — a warm cache we kept, rather than one we found.
	pinned bool
	// hops counts how many times the answering endpoint CHANGED under this slot.
	// It starts at zero for the first answer, which is an arrival and not a move.
	hops int
}

// Name is the endpoint that answered most recently, empty when nothing has
// answered yet or when no answer named its server — which is every endpoint
// that is not a router.
func (s *ServedEndpoint) Name() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

// Pinned reports whether the most recent answer came from the endpoint its own
// request asked for by name — that is, whether this lineage kept the machine
// holding its prompt cache. False is a first request, a lineage with no cache
// key, a session with routing off, and every hop.
func (s *ServedEndpoint) Pinned() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pinned
}

// Hops is how many times the answering endpoint changed while this slot was
// open. Every hop is a prompt cache written from cold on the far side, so this
// is the number a cost autopsy reads first.
func (s *ServedEndpoint) Hops() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hops
}

// note records one answer: which endpoint served it, and which endpoint its
// request had asked to come back to ("" when it asked for none).
func (s *ServedEndpoint) note(name, asked string) {
	if s == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		// THE ATTRIBUTION LAW, the same one the ledger keeps: an answer whose
		// server did not identify itself replaces nothing. Blanking the slot
		// would turn one unnamed reply into "we no longer know" about the named
		// ones beside it.
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.name != "" && s.name != name {
		s.hops++
	}
	s.name = name
	s.pinned = asked != "" && asked == name
}

type servedEndpointContextKey struct{}

// WithServedEndpoint asks the adapter to write down, in the caller's own slot,
// which endpoint answered each call made under ctx.
func WithServedEndpoint(ctx context.Context, slot *ServedEndpoint) context.Context {
	if slot == nil {
		return ctx
	}
	return context.WithValue(ctx, servedEndpointContextKey{}, slot)
}

// ServedEndpointFrom returns the slot in force for ctx, nil when none was
// opened — which every method here answers correctly, so a caller never tests.
func ServedEndpointFrom(ctx context.Context) *ServedEndpoint {
	slot, _ := ctx.Value(servedEndpointContextKey{}).(*ServedEndpoint)
	return slot
}

// noteServed hands one answer's endpoint — and the endpoint its request asked
// to come back to, "" when none — to whatever slot the caller opened. It runs
// OUTSIDE the ledger's gates: a session with `routing off` has asked not to be
// steered, which is not a request to be lied to about who answered.
func noteServed(ctx context.Context, served, asked string) {
	slot, _ := ctx.Value(servedEndpointContextKey{}).(*ServedEndpoint)
	slot.note(served, asked)
}

// noteVelocity folds one timed answer into this client's ledger.
//
// It is the one gate between the completion paths and the ledger, and it holds
// the OTHER half of what `routing off` means: a session that has asked for no
// routing preference is not measured either. Measuring it would build a ledger
// whose only possible use — demoting an endpoint on the next request — is a
// thing this client has just promised not to do.
// cached is how many of the prompt's tokens the ROUTER SAID it read back out
// of that endpoint's cache, from the usage frame. It is the only direct
// evidence there is that a lane really held our prefix — every other reading of
// it is this process's own memory of where it sent the last request — and it is
// passed through rather than estimated, because an estimate of a cache hit is a
// discount nobody granted. Zero is "the frame did not say", which is also what
// a cold prefix looks like; the belief treats them the same and is right to,
// since neither is evidence of a cache.
func (c *Client) noteVelocity(model, served string, ttft time.Duration, tokens int, elapsed time.Duration, gap time.Duration, cached int) {
	if c.velocity == nil || c.routing() == RoutingOff {
		return
	}
	c.velocity.observe(model, served, ttft, tokens, elapsed, gap)
	// The same answer, folded into the belief that is replacing the table above
	// (lanes.go). It is one call rather than two seams because the two are the
	// same fact — who served, and how fast — and the strike ledger keeps its
	// half only until the belief has been proven against it.
	c.noteLane(model, served, ttft, tokens, elapsed, gap, cached)
}

// notePacedProvider folds one provider-named 429 into the ledger, under the
// same gate as noteVelocity: a session that asked for no routing is not
// steered either, and only the router's own errors carry a provider name to
// act on. The retry loop calls this with the refusal body's named endpoint so
// every request encoded after it routes around the saturated pool instead of
// joining the queue behind it — the retries of the call that drew the 429
// still wait it out, because their body is already written.
func (c *Client) notePacedProvider(model, served string, wait time.Duration) {
	// A BELIEF SITE (#433): a pace is written against a NAMED lane, and lane
	// names come back only from a base that carries a preference. `pace` itself
	// refuses an unnamed one, which is the attribution law and the real floor.
	if c.velocity == nil || !c.carriesPreferences() || c.routing() == RoutingOff {
		return
	}
	c.velocity.pace(model, served, wait)
}

// noteCutProvider folds one guard-cut stream into the ledger under the same
// gate as notePacedProvider. A named endpoint that stopped producing an answer
// is stronger evidence than a merely slow completion, so it is refused at once
// and the retry encoded by the turn loop can route around it. An unnamed stream
// reaches pace too, where the attribution law leaves the ledger untouched.
//
// It REPORTS WHETHER IT STRUCK, because a caller has one question this is the
// only place that can answer: will the next attempt be routed away from the
// endpoint that just went quiet? False is `routing off`, a client that is not
// talking to a router at all, or a stream that died before any chunk named its
// provider — and in every one of those the next attempt goes back to the same
// lane. See [StreamCut.Rerouted] for what is decided from it.
func (c *Client) noteCutProvider(model, served string) bool {
	// A BELIEF SITE (#433), under [Client.notePacedProvider]'s gate word for
	// word: a strike against a named lane, on a base that carries a preference.
	if c.velocity == nil || !c.carriesPreferences() || c.routing() == RoutingOff {
		return false
	}
	return c.velocity.pace(model, served, 0)
}

// noteRun and streamWall are the two halves of the stream wall's evidence, and
// they are the ONLY pair on this file that is not gated on routing.
//
// The gate above exists because a demotion nobody can act on is overhead. A
// wall is acted on by every session — the cut happens, the ladder re-asks — so
// gating its history would leave `routing off` running on the bare floor with
// no way to earn anything better. See [velocityLedger.runs].
func (c *Client) noteRun(model, served string, ran time.Duration) {
	if c.velocity == nil {
		return
	}
	c.velocity.noteRun(model, served, ran)
}

// streamWall is how long the stream about to be opened, or the one now known to
// be served by `served`, may run before it is cut. See streamguard.go's THE
// WALL for the law and the constants.
func (c *Client) streamWall(model, served string) time.Duration {
	if c.velocity == nil {
		return wallFor(0)
	}
	return c.velocity.wall(model, served)
}

// streamGap is how long the stream about to be opened, or the one now known to
// be served by `served`, may go quiet between two tokens. See streamguard.go's
// [gapFor] for the law.
func (c *Client) streamGap(model, served string) time.Duration {
	if c.velocity == nil {
		return gapFor(0)
	}
	return gapFor(c.velocity.rate(model, served))
}

// completionWall is the wall a reply that is NOT streamed is held to, and
// whether there is one. A stream on a lane nothing is known about gets the
// floor, because silence bounds it as well; a completion has only its total
// deadline, sized from the room it was given, and on an unknown lane that
// deadline stays — a model that thinks at max regardless may need every
// minute of it the first time. Once the lane has finished a reply for us the
// measured wall applies to completions exactly as it does to streams, because
// a reply running five times longer than the longest this endpoint ever
// finished is a wedge, however it is being delivered. Measured 2026-08-29: a
// headless worker's completion on deepseek-flash sat in flight for the whole
// rest of a fifteen-minute run, on a lane whose longest finished reply was
// twenty-four seconds.
func (c *Client) completionWall(model string) (time.Duration, bool) {
	if c.velocity == nil || !c.velocity.measured(model) {
		return 0, false
	}
	return c.velocity.wall(model, ""), true
}

// refuseUpstream takes the lane away from an endpoint that REFUSED this request,
// so the next encode routes around it, and reports whether it struck.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// SWE-Marathon run s2 died on `after 3 retries: API error (400): Provider
// returned error`. Three retries, and nothing between them moved: releasing the
// pin (affinity.go) only stops this process ASKING for that endpoint — it does
// not stop the router choosing it again, and a router with a warm pool chooses
// the same member every time. The three attempts were three deliveries of the
// same request to the same upstream, and the turn ended with five hours of the
// ask unspent.
//
// THE LAW: AN UPSTREAM THAT REFUSED IS ROUTED AROUND, NOT ASKED AGAIN. It is
// the same verdict a 429 that names its pool earns and the same one a cut stream
// earns — a lane this process has decided not to send to — and it travels the
// same way, in `provider.ignore` on every request encoded after it.
//
// IT STRIKES A LANE OR IT STRIKES NOTHING, and which of the two is not this
// function's to decide: it is reader (a) of the one refusal object
// (refusalobject.go), and every question about what a refusal MEANS is answered
// there. A 4xx over our own bytes that demanded no machine names no lane and
// strikes nothing — there is no endpoint to blame for a malformed request, and
// refusing endpoints over our own bytes would empty the ledger one attempt at a
// time. A 429 is left to [Client.notePacedProvider], which knows the wait the
// provider named.
//
// ── AND THE ROUTER'S OWN REFUSAL IS NO LONGER EXEMPT ────────────────────────
//
// It used to require `provider_name` in the error metadata, which OpenRouter
// puts there exactly when it is relaying somebody ELSE'S refusal — so the one
// refusal that is certain about a lane, the router's own
// `…your request's provider.only preference permits only: coreweave`, was
// structurally unable to strike the lane it named. It carried no
// `provider_name` because the router was answering for itself, so the strike
// declined it, and the same machine was chosen three more times in one run
// (issue #266). The lane a refusal is about now comes from OUR OWN REQUEST, and
// a router refusal against a demanded machine is terminal for that pairing: it
// is both paced here and written out of the serving set, because a pin the
// frontier can still choose is a pin that comes back on the next turn.
func (c *Client) refuseUpstream(request *ai.Request, knobs callKnobs, err error) bool {
	return c.strikeRefusal(c.modelFor(request), c.refusalObject(request, knobs, err))
}

// strikeRefusal is the strike itself, asked by a caller that has already
// classified the refusal.
//
// IT IS AN ENTRANCE AND NOT A SECOND STRIKE, for [Client.laneRefusalFor]'s
// reason exactly: the fork every routing refusal passes through
// (client.go's [Client.sendRecovered]) holds the object already, and asking the
// classifier a second time from there would be the classification happening
// twice — which is the whole defect refusalobject.go closed. Everything a
// strike DOES is here, once, and [Client.refuseUpstream] is this function with
// the classification in front of it.
//
// STRIKING TWICE IS HARMLESS AND IS RELIED ON. A refusal that reaches a caller
// as a 4xx is struck at this seam and struck again by whoever reads the status;
// both halves are writes of a state rather than counters ([lane.RefuseServing]
// files a moment, [velocityLedger.pace] sets strikes rather than incrementing
// them), so the second is the first said again.
func (c *Client) strikeRefusal(model string, refusal laneRefusal) bool {
	// A BELIEF SITE (#433), under the same gate as the two paces above.
	if c.velocity == nil || !c.carriesPreferences() || c.routing() == RoutingOff {
		return false
	}
	if !refusal.struck() {
		return false
	}
	c.refuseServing(model, refusal)
	return c.velocity.pace(model, refusal.Lane, 0)
}

// pacedProviderName reads which endpoint a 429 came from, "" when the body
// does not say. OpenRouter names the upstream in the error's metadata when the
// limit is one provider's shared pool rather than this account — exactly the
// case where another endpoint could answer right now and waiting is the wrong
// move. Decoded leniently and separately from errorBody: metadata is the
// router's dialect, and a provider that shapes its errors differently simply
// answers "" here and keeps the pacing behaviour it always had.
func pacedProviderName(payload []byte) string {
	var decoded struct {
		Error struct {
			Metadata struct {
				ProviderName string `json:"provider_name"`
			} `json:"metadata"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return ""
	}
	return strings.TrimSpace(decoded.Error.Metadata.ProviderName)
}

// ── THE LAG LAW ─────────────────────────────────────────────────────────────
//
// The four constants below are the whole of it. They are stated once, here,
// because a threshold buried in a branch is a threshold nobody can argue with.

const (
	// LagTTFT is the wait before the first token that counts as slow. Two
	// seconds is roughly four times what a warm endpoint takes to start
	// answering, so crossing it is a claim about the endpoint rather than about
	// a large prompt.
	LagTTFT = 2000 * time.Millisecond

	// LagRate is the sustained output rate, in tokens per second, below which
	// an endpoint is slow. Thirty is about a third of what the small fast models
	// this surface rides sustain, and comfortably under the slowest large one.
	LagRate = 30.0

	// LagGap is the widest quiet stretch INSIDE a successful answer that still
	// counts as streaming. Above it the endpoint is assembling the reply
	// server-side and delivering it in lumps — a shape measured across one
	// model's sixteen endpoints on 2026-08-24, where every endpoint that
	// streamed stayed under four seconds between deltas and every one that
	// buffered sat at twelve seconds or worse, up to fifty. Fifteen sits in
	// the empty middle. A lumped answer that arrives is still an answer, which
	// is why this is a lag strike and never a cut: the lane is demoted below
	// the endpoints that stream, and the stall guard's patience (streamguard.
	// go's bufferedQuietBound) is what keeps the lump survivable meanwhile.
	LagGap = 15 * time.Second

	// demoteAfter is how many laggy answers an endpoint gets before the next
	// request prefers something else. TWO rather than one: a single slow answer
	// is as likely to be a cold cache or a long prompt as a bad endpoint, and
	// acting on one measurement is how a ledger learns noise.
	demoteAfter = 2

	// ignoreAfter is how many laggy answers it takes to be refused outright.
	ignoreAfter = 3

	// ignoreCooldown is how long a refusal lasts. Five minutes is long enough to
	// outlive the load spike that usually caused it and short enough that a
	// session never permanently loses an endpoint over one bad stretch.
	ignoreCooldown = 5 * time.Minute

	// ratedFloor is the fewest output tokens a rate may be computed from.
	// A twelve-token answer is over before the connection is warm, and its
	// "rate" measures the handshake. Below the floor only the TTFT judges.
	ratedFloor = 32
)

// Sighting is one timed answer: who served it, and how fast.
//
// It is the ledger's unit and the HUD's fact, which is why it carries both the
// raw measurements and the verdict — a surface must not have to re-derive
// "was this slow" from thresholds it would then own a second copy of.
type Sighting struct {
	// Model is the model as it was asked for, normalized.
	Model string
	// Provider is the endpoint the router says served it, exactly as the
	// response spelled it.
	Provider string
	// TTFT is the wait before the first token, zero when unmeasured — which is
	// every non-streamed call, where there is no first token to observe.
	TTFT time.Duration
	// Tokens is what the answer was worth in output tokens, and Elapsed is the
	// window Rate was computed over.
	Tokens  int
	Elapsed time.Duration
	// Rate is output tokens per second, zero when the answer was too short to
	// rate (see [ratedFloor]).
	Rate float64
	// Gap is the widest quiet stretch between two deltas of a streamed answer,
	// zero when unmeasured — every non-streamed call, where the answer has no
	// inside to be quiet in.
	Gap time.Duration
	// Laggy is the verdict this sighting earned under the law above.
	Laggy bool
	// At is when the answer finished.
	At time.Time
}

// lane is one endpoint's standing with one model.
type lane struct {
	provider string
	// strikes counts laggy answers, and it FALLS on a fast one: a lane that
	// recovers walks back out the way it walked in, so a busy hour cannot
	// permanently condemn an endpoint that is now the fastest thing available.
	strikes int
	// ignoredUntil is when a refusal expires. Zero is not refused.
	ignoredUntil time.Time
	// refused says the lane was taken away because IT DID NOT SERVE — a 429 that
	// named its pool, a stream that went quiet, an upstream that answered 4xx —
	// rather than because it served SLOWLY. The two are different claims and the
	// "never condemn everything" rule in [velocityLedger.preferences] treats them
	// differently; see the law stated there.
	refused bool
	seen    int
	last    Sighting
}

// velocityLedger is what this process has measured, in memory, per model.
//
// It is in-memory ON PURPOSE. Endpoint speed is a fact about the last few
// minutes — a replica that was saturated at noon is the fastest one at ten past
// — and a ledger that survived a restart would open every session by acting on
// a claim it could no longer see. Nothing here is written to disk, nothing here
// costs a call, and nothing here is billed.
type velocityLedger struct {
	mu    sync.Mutex
	now   func() time.Time
	lanes map[string]map[string]*lane
	last  map[string]Sighting
	// runs is model → endpoint → the LONGEST REPLY THAT ENDPOINT HAS FINISHED
	// for this process. It is what the stream wall is derived from
	// (streamguard.go's [wallFor]), and it is kept apart from `lanes` for two
	// reasons.
	//
	// FIRST, IT IS NOT A STRIKE. A lane's entry above is a standing — demoted,
	// refused, on probation — and every write to it is a steering decision.
	// This is a measurement of duration and nothing else; folding it into a
	// lane would make "how long does this endpoint take" and "should we send
	// there" one field, and the two are asked at different moments by different
	// code.
	//
	// SECOND, IT IS RECORDED EVEN WITH `routing off`. The ledger above is not:
	// an operator who asked for no steering asked for no demotions either, and
	// the file says so. But a WALL is acted on whatever routing says — the cut
	// happens, the request is re-asked — so the history it is derived from has
	// to exist under routing off too, or every such session would run on the
	// bare floor forever. Nothing here can move a request to another endpoint,
	// so recording it steers nothing.
	runs map[string]map[string]time.Duration
	// noCeiling is model → "the price ceiling has emptied this model's endpoint
	// set once, do not send it again". It is process-lifetime and never expires,
	// because what it records is not a lane's mood but the ACCOUNT'S privacy
	// policy meeting the catalog's list price: the ceiling is list × 1.25, the
	// only endpoint under it is the first-party one, and the account has that
	// provider switched off. Nothing about that changes between one call and
	// the next, so a memo that expired would just buy the same 404 back.
	noCeiling map[string]bool
}

func newVelocityLedger() *velocityLedger {
	return &velocityLedger{
		now:       time.Now,
		lanes:     map[string]map[string]*lane{},
		last:      map[string]Sighting{},
		runs:      map[string]map[string]time.Duration{},
		noCeiling: map[string]bool{},
	}
}

// refuseCeiling records that the router emptied model's endpoint set on price
// or policy grounds while a ceiling was on the request. From here on
// [Client.providerPreferences] sends no max_price for that model.
func (v *velocityLedger) refuseCeiling(model string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.noCeiling[normalizeModel(model)] = true
}

// ceilingRefused reports whether [refuseCeiling] has been called for model.
func (v *velocityLedger) ceilingRefused(model string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.noCeiling[normalizeModel(model)]
}

// noteRun remembers how long one COMPLETED reply took, under the endpoint that
// served it.
//
// COMPLETED IS THE WHOLE TEST OF HEALTH HERE, and it is a deliberately weaker
// word than the one the lag law uses. A lane demoted for slowness still
// answered; its answers took as long as they took, and a wall that excluded
// them would cut the next one and turn a speed verdict into an outage. The
// question this is evidence for is not "was that fast" but "has this lane ever
// legitimately taken this long", and only a reply that arrived can answer it.
//
// It keeps the maximum rather than the last or a mean. A wall wants the widest
// legitimate reply, because that is the one it must not cut; the average would
// cut half of them.
//
// A reply from an endpoint that never named itself is recorded under the empty
// name, where it still counts towards the lineage's widest wall and belongs to
// no lane — the same attribution law observe follows, for the same reason.
func (l *velocityLedger) noteRun(model, served string, ran time.Duration) {
	if l == nil || ran <= 0 {
		return
	}
	key := normalizeModel(model)
	if key == "" {
		return
	}
	served = strings.TrimSpace(served)
	l.mu.Lock()
	defer l.mu.Unlock()
	byLane := l.runs[key]
	if byLane == nil {
		byLane = map[string]time.Duration{}
		l.runs[key] = byLane
	}
	if ran > byLane[served] {
		byLane[served] = ran
	}
}

// wall is how long the next reply on this (model, endpoint) may run.
//
// A NAMED LANE IS ASKED ABOUT ITSELF FIRST, and falls back to the lineage's
// widest when this process has never seen it finish anything. That fallback is
// the difference between a wall that works and one that fights the router: an
// endpoint the ledger has just steered a long session onto is new by
// construction, and giving its first reply the bare floor would cut exactly the
// work the steering was for.
//
// An unnamed ask — a request that has not yet learned who is serving it — gets
// the lineage's widest, which is the most generous honest answer available
// before the first chunk arrives.
// measured says whether any endpoint serving the model has finished a reply
// in this process — the difference between a wall that was derived and the
// floor handed to a stranger.
func (l *velocityLedger) measured(model string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.runs[normalizeModel(model)]) > 0
}

func (l *velocityLedger) wall(model, served string) time.Duration {
	if l == nil {
		return wallFor(0)
	}
	key := normalizeModel(model)
	l.mu.Lock()
	defer l.mu.Unlock()
	byLane := l.runs[key]
	if len(byLane) == 0 {
		return wallFor(0)
	}
	served = strings.TrimSpace(served)
	if served != "" {
		if longest, seen := byLane[served]; seen {
			return wallFor(longest)
		}
	}
	widest := time.Duration(0)
	for _, longest := range byLane {
		if longest > widest {
			widest = longest
		}
	}
	return wallFor(widest)
}

// rate is the output rate, in tokens per second, that this process has last
// measured for one (model, endpoint) — the same figure the status line prints
// (velocity.go's [Sighting.Rate]), read here so that patience can be stated in
// it. Zero is a lane nothing has been rated for.
//
// A NAMED LANE IS ASKED ABOUT ITSELF FIRST, and falls back to the SLOWEST rate
// any endpoint of this model has shown. That fallback is the mirror image of
// [velocityLedger.wall]'s, and for the same reason: before the first chunk names
// who is serving, the most generous honest answer is the one that grants the
// most patience, and on a rate the most generous answer is the smallest number.
func (l *velocityLedger) rate(model, served string) float64 {
	if l == nil {
		return 0
	}
	key := normalizeModel(model)
	l.mu.Lock()
	defer l.mu.Unlock()
	byLane := l.lanes[key]
	if len(byLane) == 0 {
		return 0
	}
	if served = strings.TrimSpace(served); served != "" {
		if entry, seen := byLane[served]; seen && entry.last.Rate > 0 {
			return entry.last.Rate
		}
	}
	slowest := 0.0
	for _, entry := range byLane {
		if rate := entry.last.Rate; rate > 0 && (slowest == 0 || rate < slowest) {
			slowest = rate
		}
	}
	return slowest
}

// sharedVelocity is the ledger every client built by [NewClient] folds into.
//
// It is process-wide for the reason the quirks memo is: the fact belongs to the
// model and its endpoints, not to whichever adapter happened to be holding the
// connection, and a surface's HUD has no handle on the adapter its session
// built. A test that wants isolation builds its own ledger and assigns it.
var sharedVelocity = newVelocityLedger()

// LastServed is the most recent sighting for a model, false when this process
// has not seen one. It is the HUD's read and it is a copy: nothing a surface
// does can reach the ledger's state.
func LastServed(model string) (Sighting, bool) { return sharedVelocity.lastServed(model) }

func (l *velocityLedger) lastServed(model string) (Sighting, bool) {
	if l == nil {
		return Sighting{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	sighting, ok := l.last[normalizeModel(model)]
	return sighting, ok
}

// observe folds one timed answer in and returns what it decided.
//
// An answer whose server did not identify itself is measured and remembered as
// the model's latest sighting, but earns no strike: a strike is a claim about
// an endpoint, and there is no endpoint here to make it about.
func (l *velocityLedger) observe(model, served string, ttft time.Duration, tokens int, elapsed time.Duration, gap time.Duration) Sighting {
	if l == nil {
		return Sighting{}
	}
	key := normalizeModel(model)
	if key == "" {
		return Sighting{}
	}
	sighting := Sighting{
		Model:    key,
		Provider: strings.TrimSpace(served),
		TTFT:     ttft,
		Tokens:   tokens,
		Elapsed:  elapsed,
		Gap:      gap,
	}
	if tokens >= ratedFloor && elapsed > 0 {
		sighting.Rate = float64(tokens) / elapsed.Seconds()
	}
	sighting.Laggy = (ttft > 0 && ttft > LagTTFT) || (sighting.Rate > 0 && sighting.Rate < LagRate) || gap > LagGap

	l.mu.Lock()
	defer l.mu.Unlock()
	sighting.At = l.now()
	l.last[key] = sighting
	if sighting.Provider == "" {
		return sighting
	}
	lanes := l.lanes[key]
	if lanes == nil {
		lanes = map[string]*lane{}
		l.lanes[key] = lanes
	}
	entry := lanes[sighting.Provider]
	if entry == nil {
		entry = &lane{provider: sighting.Provider, seen: len(lanes)}
		lanes[sighting.Provider] = entry
	}
	entry.last = sighting
	switch {
	case sighting.Laggy:
		entry.strikes++
		if entry.strikes >= ignoreAfter {
			entry.ignoredUntil = sighting.At.Add(ignoreCooldown)
			// A SLOWNESS VERDICT, and it says so: this lane answered, three times,
			// too slowly. That is a different claim from a lane that refused, and
			// [velocityLedger.preferences] is allowed to weigh it differently.
			entry.refused = false
		}
	case entry.strikes > 0:
		// A fast answer pays a strike back. It also lifts a refusal, which can
		// only be observed after the cooldown let the endpoint be tried again —
		// that retry succeeding is exactly the evidence the refusal was for.
		entry.strikes--
		entry.ignoredUntil = time.Time{}
		entry.refused = false
	}
	return sighting
}

// preferences is the ledger's verdict for the next request: which endpoints to
// try first, and which to refuse.
//
// THE LAW, in the order it applies:
//
//	refused    strikes ≥ 3, until the cooldown expires → provider.ignore
//	probation  a refusal that has expired comes back demoted, one strike short
//	           of being refused again, so an endpoint that is still slow is
//	           dropped by its very next answer instead of getting a fresh three
//	demoted    strikes ≥ 2 → last in provider.order, behind every healthy lane
//	healthy    everything else, in the order it was first seen
//
// Order is emitted ONLY when a healthy lane exists. `provider.order` names what
// to try first, so a list containing nothing but demoted endpoints would pin
// the slowest thing we know to the front — the exact inverse of the intent. In
// that case the sort word is left to choose and the refusals still stand.
func (l *velocityLedger) preferences(model string) (order []string, ignore []string) {
	if l == nil {
		return nil, nil
	}
	key := normalizeModel(model)
	l.mu.Lock()
	defer l.mu.Unlock()
	lanes := l.lanes[key]
	if len(lanes) == 0 {
		return nil, nil
	}
	now := l.now()
	ranked := make([]*lane, 0, len(lanes))
	for _, entry := range lanes {
		ranked = append(ranked, entry)
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].seen < ranked[j].seen })

	var healthy, demoted []string
	// refusedSomething says at least one of the names going into `ignore` is
	// there because it DID NOT SERVE rather than because it served slowly. The
	// rule below turns on it.
	refusedSomething := false
	for _, entry := range ranked {
		if !entry.ignoredUntil.IsZero() {
			if now.Before(entry.ignoredUntil) {
				ignore = append(ignore, entry.provider)
				refusedSomething = refusedSomething || entry.refused
				continue
			}
			entry.ignoredUntil = time.Time{}
			entry.strikes = ignoreAfter - 1
			entry.refused = false
		}
		if entry.strikes >= demoteAfter {
			demoted = append(demoted, entry.provider)
			continue
		}
		healthy = append(healthy, entry.provider)
	}
	// THE LEDGER MAY NEVER CONDEMN EVERYTHING IT KNOWS OVER SPEED. On a model
	// with one provider — and single-provider models are common — three slow
	// answers used to put that one name in `ignore` and turn every request for
	// five minutes into an instant "All providers have been ignored" 404. A
	// SLOWNESS verdict that condemns the whole set is not a preference, it is an
	// outage this process built for itself; when nothing is left to prefer, the
	// honest answer is no verdict at all, and the sort word chooses among slow
	// lanes.
	//
	// BUT A LANE THAT REFUSED IS NOT A SLOW LANE, and this is where the measured
	// failure of SWE-Marathon run s2 lived. One endpoint answered 400 "Provider
	// returned error"; it was the only lane the ledger held for that model; the
	// rule above then dropped the verdict entirely, so the next request went
	// straight back to the endpoint that had just refused, three times, fifteen
	// seconds apart, and the turn died with five hours of the ask unspent.
	// Sending to a lane already known to refuse is not a fallback, it is the same
	// failure again — and the case this rule was written for is answered a rung
	// higher anyway: a request that ignored everybody comes back "no endpoints
	// found", and the refusal ladder re-sends it with the ignores taken off
	// ([relaxedPreferences], endpoints.go).
	if len(healthy) == 0 && len(demoted) == 0 && !refusedSomething {
		return nil, nil
	}
	if len(healthy) == 0 {
		return nil, ignore
	}
	return append(healthy, demoted...), ignore
}

// pace is the ledger taking a provider at its word: a 429 that NAMES the
// endpoint it came from is that endpoint saying "not now", which is better
// evidence than any number of timed answers. The lane is refused outright for
// the given wait so the next encoded request routes around it instead of
// queueing behind a pool somebody else is saturating.
//
// The strikes are set rather than incremented, so recovery is the one already
// written: the cooldown expires, the lane comes back on probation, and its
// first fast answer walks it out (observe). A wait the provider did not name,
// or named absurdly, is clamped to the same cooldown a laggy lane serves —
// pacing is a claim about the next minutes, never about the day.
//
// It reports whether a lane was actually refused. The attribution law leaves an
// unnamed endpoint alone, and "nothing was struck" is a fact a caller acts on
// (noteCutProvider), not a silence to infer from.
func (l *velocityLedger) pace(model, served string, wait time.Duration) bool {
	if l == nil {
		return false
	}
	key := normalizeModel(model)
	served = strings.TrimSpace(served)
	if key == "" || served == "" {
		return false
	}
	if wait <= 0 || wait > ignoreCooldown {
		wait = ignoreCooldown
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	lanes := l.lanes[key]
	if lanes == nil {
		lanes = map[string]*lane{}
		l.lanes[key] = lanes
	}
	entry := lanes[served]
	if entry == nil {
		entry = &lane{provider: served, seen: len(lanes)}
		lanes[served] = entry
	}
	entry.strikes = ignoreAfter
	entry.ignoredUntil = l.now().Add(wait)
	// AND IT IS MARKED AS A LANE THAT DID NOT SERVE, which is what every caller
	// of pace has in common: a 429 naming its pool, a stream that went quiet, an
	// upstream that answered 4xx. The distinction is read in [preferences].
	entry.refused = true
	return true
}
