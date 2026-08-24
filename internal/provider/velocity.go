package provider

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── WHO SERVED, AND HOW FAST ────────────────────────────────────────────────
//
// A router fans one model over many endpoints. Two of them are a datacentre
// away with a warm replica; one is a machine somebody is also training on, and
// it answers the same request at a fifth of the speed for the same price. The
// model id says nothing about which one a request landed on, so a session that
// only names its model is describing a decision it did not make.
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
	// RoutingLatency asks for the currently-fastest endpoint. It is the default
	// because a chat session is a person waiting.
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
// is the default rather than a refusal, so a caller that has nothing to say
// gets latency.
func StaticRouting(strategy RoutingStrategy) RoutingSource { return staticRouting(strategy) }

// routing resolves the strategy for this client.
func (c *Client) routing() RoutingStrategy {
	if c.config.Routing == nil {
		return RoutingLatency
	}
	strategy := c.config.Routing.RoutingStrategy()
	if strings.TrimSpace(string(strategy)) == "" {
		return RoutingLatency
	}
	parsed, _ := ParseRoutingStrategy(string(strategy))
	return parsed
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
	Sort              string   `json:"sort,omitempty"`
	Order             []string `json:"order,omitempty"`
	Ignore            []string `json:"ignore,omitempty"`
	AllowFallbacks    *bool    `json:"allow_fallbacks,omitempty"`
	RequireParameters *bool    `json:"require_parameters,omitempty"`
}

// providerPreferences builds the object one request will carry, nil when none
// should be sent.
//
// It is called from the encoder, which runs immediately before the send, and
// that timing is the point: the demotion a laggy answer earned thirty seconds
// ago applies to the request now being written, without anything having to
// carry it forward.
//
// It is OpenRouter-only. The field is a router's dialect, and an OpenAI-
// compatible endpoint that is not a router either ignores it or 400s on it —
// neither of which is worth risking for a preference it could not honour.
func (c *Client) providerPreferences(model string) *providerPrefs {
	if !c.isOpenRouter() {
		return nil
	}
	strategy := c.routing()
	word := strategy.sortWord()
	if word == "" {
		return nil
	}
	yes := true
	prefs := &providerPrefs{Sort: word, AllowFallbacks: &yes, RequireParameters: &yes}
	if c.velocity != nil {
		prefs.Order, prefs.Ignore = c.velocity.preferences(model)
	}
	return prefs
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
	if relaxed.Sort == "" && len(relaxed.Order) == 0 && relaxed.AllowFallbacks == nil {
		return nil
	}
	return &relaxed
}

// noteVelocity folds one timed answer into this client's ledger.
//
// It is the one gate between the completion paths and the ledger, and it holds
// the OTHER half of what `routing off` means: a session that has asked for no
// routing preference is not measured either. Measuring it would build a ledger
// whose only possible use — demoting an endpoint on the next request — is a
// thing this client has just promised not to do.
func (c *Client) noteVelocity(model, served string, ttft time.Duration, tokens int, elapsed time.Duration, gap time.Duration) {
	if c.velocity == nil || c.routing() == RoutingOff {
		return
	}
	c.velocity.observe(model, served, ttft, tokens, elapsed, gap)
}

// notePacedProvider folds one provider-named 429 into the ledger, under the
// same gate as noteVelocity: a session that asked for no routing is not
// steered either, and only the router's own errors carry a provider name to
// act on. The retry loop calls this with the refusal body's named endpoint so
// every request encoded after it routes around the saturated pool instead of
// joining the queue behind it — the retries of the call that drew the 429
// still wait it out, because their body is already written.
func (c *Client) notePacedProvider(model, served string, wait time.Duration) {
	if c.velocity == nil || !c.isOpenRouter() || c.routing() == RoutingOff {
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
	if c.velocity == nil || !c.isOpenRouter() || c.routing() == RoutingOff {
		return false
	}
	return c.velocity.pace(model, served, 0)
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
	seen         int
	last         Sighting
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
}

func newVelocityLedger() *velocityLedger {
	return &velocityLedger{
		now:   time.Now,
		lanes: map[string]map[string]*lane{},
		last:  map[string]Sighting{},
	}
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
		}
	case entry.strikes > 0:
		// A fast answer pays a strike back. It also lifts a refusal, which can
		// only be observed after the cooldown let the endpoint be tried again —
		// that retry succeeding is exactly the evidence the refusal was for.
		entry.strikes--
		entry.ignoredUntil = time.Time{}
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
	for _, entry := range ranked {
		if !entry.ignoredUntil.IsZero() {
			if now.Before(entry.ignoredUntil) {
				ignore = append(ignore, entry.provider)
				continue
			}
			entry.ignoredUntil = time.Time{}
			entry.strikes = ignoreAfter - 1
		}
		if entry.strikes >= demoteAfter {
			demoted = append(demoted, entry.provider)
			continue
		}
		healthy = append(healthy, entry.provider)
	}
	// THE LEDGER MAY NEVER REFUSE EVERYTHING IT KNOWS. On a model with one
	// provider — and single-provider models are common — three slow answers
	// used to put that one name in `ignore` and turn every request for five
	// minutes into an instant "All providers have been ignored" 404. A verdict
	// that condemns the whole set is not a preference, it is an outage this
	// process built for itself; when nothing is left to prefer, the honest
	// answer is no verdict at all, and the sort word chooses among slow lanes.
	if len(healthy) == 0 && len(demoted) == 0 {
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
	return true
}
