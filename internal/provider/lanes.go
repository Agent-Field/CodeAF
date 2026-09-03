package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE ADAPTER BETWEEN A BELIEF AND A WIRE ─────────────────────────────────
//
// `internal/lane` holds what this build believes about the machines behind a
// model. It has no transport: it never opens a connection, never reads a clock
// of its own and never draws anything. This file is the whole of the join
// between it and the router's dialect, and the arrows point one way
// (docs/ARCHITECTURE.md, Decision 10):
//
//	request  →  lane.Chooser.Choose  →  provider.order / provider.ignore
//	answer   →  lane.Ledger.Note     →  the belief this process measured itself
//
// TWO LAWS SHAPE EVERY LINE OF IT.
//
// AN EMPTY CHOICE IS A REAL ANSWER. On a machine that has never used a model
// the ledger believes nothing, the chooser says nothing, and the request is
// shaped exactly as it was before this package existed — the sort word, the
// strike ledger's order, the price ceiling, the affinity pin. Nothing here has
// a fallback of its own to invent, because the transport already has one that
// works.
//
// NOTHING HERE FETCHES. The chooser reads memory and the ledger is written from
// answers that have already arrived. `lane_law_test.go` fails the build if any
// non-test file in this package so much as names the sheet's Refresh, and that
// is the law this file must keep hardest: it runs inside the encoder, in front
// of somebody's first token.

const (
	// talkTokens and workTokens are how long an answer is expected to be, and
	// they are the split that matters rather than the total: talk is READ by a
	// person and work is not. Four hundred and two thousand are the design's
	// figures, and they are what [lane.PerceivedSeconds] turns into the
	// difference between "any lane over 30 tok/s is the same speed" and "every
	// token is pure waiting".
	talkTokens = 400
	workTokens = 2000

	// talkQuality and workQuality are the share of answers that must come back
	// usable for a lane to stay in the candidate set. Work is held higher
	// because a tool call the decoder refuses costs a whole retry, while a talk
	// turn that reads oddly costs a re-ask somebody was going to make anyway.
	talkQuality = 0.90
	workQuality = 0.97

	// defaultHorizon is how many more calls a session is assumed to have in it
	// when nothing has said. It is [lane.ExplorationHorizon] — the point at
	// which exploration is worth its full width — so a session that says
	// nothing explores normally rather than being quietly pinned to what it
	// already knows.
	defaultHorizon = lanes.ExplorationHorizon

	// charsPerToken is the crude estimate this adapter already rates answers
	// with ([outputTokens]). It is stated once, here, because a second copy of
	// it is a second answer to "how long is this prompt".
	charsPerToken = 4
)

// ── WHAT ONE CALL SAYS ABOUT ITSELF ─────────────────────────────────────────

// secondsPerDollar is λ as a call site stated it: how many seconds of waiting
// one dollar is worth buying out of, and whether anybody said.
//
// THE SECOND FIELD IS THE POINT. Zero is a real answer — "nobody is waiting,
// price wins outright" — and it is a different fact from "this call has said
// nothing about itself", which falls back to who is waiting on it. A bare
// float64 cannot tell the two apart, and the one it would silently choose is
// the expensive one.
type secondsPerDollar struct {
	seconds float64
	said    bool
}

type valueOfTimeContextKey struct{}
type callHorizonContextKey struct{}

// WithValueOfTime states what a second is worth to whoever is waiting on the
// calls made under ctx, in SECONDS PER DOLLAR.
//
// It is the companion of [WithRoutingIntent] and it is written under the same
// law: IT IS SAID AND NEVER INFERRED. The intent says whether somebody is
// waiting; this says what their wait costs, which is a thing only the graph
// knows — a chat turn is worth a person's attention ([lane.AttentionValue]), a
// node on a task's critical path is worth the whole task, and a node with slack
// off the path is worth nothing at all. [lane.Lambda] is the table that answers
// it; this is how the answer travels.
//
// A call that says nothing keeps the behaviour it has always had: interactive
// calls are worth a person's attention, background calls are worth nothing, and
// a `price` routing row is worth nothing whatever anybody said.
func WithValueOfTime(ctx context.Context, seconds float64) context.Context {
	if seconds < 0 {
		seconds = 0
	}
	return context.WithValue(ctx, valueOfTimeContextKey{}, secondsPerDollar{seconds: seconds, said: true})
}

// ValueOfTimeFrom answers what a second is worth on the calls made under ctx,
// and whether anybody said. It is the read half of [WithValueOfTime], exported
// so a surface can assert what its own calls will ask for.
func ValueOfTimeFrom(ctx context.Context) (float64, bool) {
	stated, _ := ctx.Value(valueOfTimeContextKey{}).(secondsPerDollar)
	return stated.seconds, stated.said
}

// WithCallHorizon states roughly how many more model calls the work under ctx
// expects to make. It sizes exploration and nothing else: a three-call errand
// should never pay to find out whether a lane it has not used is quicker, and a
// five-hundred-call swarm should find out early (Part II §7 of the design).
func WithCallHorizon(ctx context.Context, calls int) context.Context {
	if calls < 0 {
		calls = 0
	}
	return context.WithValue(ctx, callHorizonContextKey{}, calls)
}

// CallHorizonFrom answers how many more calls are expected under ctx, and
// whether anybody said.
func CallHorizonFrom(ctx context.Context) (int, bool) {
	calls, said := ctx.Value(callHorizonContextKey{}).(int)
	return calls, said
}

func valueOfTimeFrom(ctx context.Context) secondsPerDollar {
	stated, _ := ctx.Value(valueOfTimeContextKey{}).(secondsPerDollar)
	return stated
}

func callHorizonFrom(ctx context.Context) int {
	calls, _ := ctx.Value(callHorizonContextKey{}).(int)
	return calls
}

// ── ASKING THE BELIEF ───────────────────────────────────────────────────────

// laneValueOfTime is λ for one request, in seconds per dollar.
//
// The routing row wins outright, exactly as it does for the sort word: `price`
// is a person saying that speed is not worth money on any of their calls, and
// no call site may override it. Below that the call site's own figure applies,
// and below that the default follows who is waiting.
//
// THE ROW A PERSON WROTE AND THE DEFAULT DERIVED FROM WHO IS WAITING ARE NOT
// THE SAME FACT, and reading them through one value silently made λ a dead
// letter for every background call. [Client.routingFor] answers `price` for an
// unattended call because nobody said otherwise; taking that as "a person said
// speed is worthless" then discarded the call site's own λ, so a task node that
// declared its wait was worth something was routed as though it had declared
// the opposite — and no test could see it, because the caller had said the
// right thing. So the veto is asked of [Client.routingChoice], which reports
// whether a person really said, and the stated figure below it is authoritative
// for everybody else.
func (c *Client) laneValueOfTime(knobs callKnobs) float64 {
	if chosen, said := c.routingChoice(); said && chosen == RoutingPrice {
		return 0
	}
	if knobs.lambda.said {
		return knobs.lambda.seconds
	}
	if knobs.intent == IntentBackground {
		return 0
	}
	return lanes.AttentionValue
}

// laneRequest is everything the chooser is allowed to know about this call.
//
// The visible and hidden split is read from WHO IS WAITING rather than from the
// body: a turn somebody is watching is text they will read, and a call nobody
// is waiting on is a tool loop whose tokens are pure waiting. That is the one
// distinction [lane.PerceivedSeconds] needs, and it is the difference between
// paying for throughput and paying for nothing.
func (c *Client) laneRequest(model string, knobs callKnobs, request *ai.Request, lambda float64) lanes.Request {
	visible, hidden, quality := talkTokens, 0, talkQuality
	if knobs.intent == IntentBackground {
		visible, hidden, quality = 0, workTokens, workQuality
	}
	horizon := knobs.horizon
	if horizon <= 0 {
		horizon = defaultHorizon
	}
	ceiling := 0
	if request.MaxTokens != nil {
		ceiling = *request.MaxTokens
	}
	return lanes.Request{
		Model:        laneModel(model),
		PromptTokens: promptTokens(request),
		Prefix:       knobs.cacheKey,
		Visible:      visible,
		Hidden:       hidden,
		Tools:        len(request.Tools) > 0,
		MaxTokens:    ceiling,
		ValueOfTime:  lambda,
		QualityNeed:  quality,
		Horizon:      horizon,
		Now:          laneNow(),
	}
}

// LaneTalkAsk is the request A CONVERSATION'S OWN TURN makes, with nothing yet
// typed into it: one model, a person waiting, an answer they will read.
//
// IT EXISTS SO THE PICKER ASKS THE CHOOSER THE SAME QUESTION THE WIRE WILL.
// The `auto` row under a model says which machine would answer if you sent
// something now (internal/tui3's laneAuto), and the only honest way to say that
// is to ask with the request a turn really carries. A surface that built its own
// [lanes.Request] got a different answer for the same belief — one built with λ
// left at zero says "the cheapest machine", which is the correct answer to a
// question a conversation never asks — and the row then named a lane the very
// next turn did not use.
//
// The prompt's own length is left out and so is its cache prefix: neither is
// known before somebody has typed, both only sharpen a ranking this row draws
// before the fact, and inventing them would be the surface guessing at a
// request that does not exist yet.
func LaneTalkAsk(model string, now time.Time) lanes.Request {
	return lanes.Request{
		Model:       laneModel(model),
		Visible:     talkTokens,
		QualityNeed: talkQuality,
		ValueOfTime: lanes.AttentionValue,
		Horizon:     defaultHorizon,
		Now:         now,
	}
}

// laneModel is the id a BELIEF is filed under, which is not always the id a
// request is sent with.
//
// `moonshotai/kimi-k3:high` and `moonshotai/kimi-k3` are one model served by one
// set of machines: the suffix says how hard to think, the endpoints page answers
// the same seventeen lanes for both, and the router publishes that page under
// the bare id. Sent with the suffix — which is what the wire needs — and FILED
// with it too, a session's whole sheet lands in one ledger and every question
// is asked of another, empty one. The chooser then has fewer than two lanes to
// rank, answers with no opinion, and the request goes out on the router's own
// sort with no watch on it. That is what happened on 2026-08-30, and it is the
// same fact `internal/lane` states at [lanes.BareModel]; this is the seam where
// it is applied, so that the ask this adapter remembers per model and the
// sighting it later attributes are filed under one name.
//
// AND THE SAME SEAM ANSWERS THE FLOATING ALIAS, which is the shipped default's
// version of the same split and the worse one, because every install routes by
// it. `~deepseek/deepseek-v4-flash-latest` is what the config carries and what
// the wire sends; OpenRouter resolves it on its own side, per request, and
// publishes the endpoints page under the concrete model it points at — there is
// no such page for the alias. Filed as written, this side keyed
// `deepseek/deepseek-v4-flash-latest`, the beat asked for a sheet under
// `~deepseek/deepseek-v4-flash-latest`, and the machines that answered were
// `deepseek/deepseek-v4-flash-0731`'s. One model, three ledger keys, and the
// default model of every install therefore chose between no lanes at all.
//
// [lanes.LedgerModel] folds both — the tier suffix and the alias — and the fold
// is INJECTED rather than imported, so on a build with no catalog installed this
// line is exactly the bare-model normalisation it always was. The alias is
// resolved to its alias TARGET and never to the canonical slug: for these rows
// the router repeats the alias in canonical_slug, and the dated id one hop on is
// served nowhere.
//
// The wire keeps the suffix and the alias. Only the bookkeeping resolves them.
func laneModel(model string) string { return lanes.LedgerModel(normalizeModel(model)) }

// laneNow is the moment a choice is made at and a sighting is stamped with.
//
// IT IS THE WALL CLOCK AND DELIBERATELY NOT [Client.clock]. That seam exists so
// a test can SCRIPT a measurement — four hundred milliseconds to the first
// token, a second of writing — by handing out a prepared sequence of moments,
// and every read of it consumes one. Asking it what time it is in order to
// timestamp a belief would spend a tick a measurement was going to use, which
// would make the belief's arrival quietly change the numbers the ledger beside
// it records.
func laneNow() time.Time { return time.Now() }

// promptTokens is roughly how long this conversation is, in tokens.
//
// It is the same four-characters-a-token approximation the adapter already
// rates answers with, and it is deliberately crude: it feeds a gate on context
// length and the prompt half of a price comparison between lanes, where being
// ten per cent out moves nothing. It is never billed and never shown.
func promptTokens(request *ai.Request) int {
	if request == nil {
		return 0
	}
	characters := 0
	for _, message := range request.Messages {
		for _, part := range message.Content {
			characters += len(part.Text)
		}
		for _, call := range message.ToolCalls {
			characters += len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	for _, tool := range request.Tools {
		characters += len(tool.Function.Name) + len(tool.Function.Description)
	}
	return characters / charsPerToken
}

// tokensIn is the estimate above, over one string. See [outputTokens], which is
// its other caller and the reason it is stated in one place.
func tokensIn(text string) int { return len(text) / charsPerToken }

// applyLaneChoice puts the belief's preference on a request that is about to go
// out, and does nothing at all when there is no belief to put.
//
// THE BELIEF'S ORDER TAKES PRECEDENCE OVER THE STRIKE LEDGER'S. They are two
// answers to one question and the newer one is derived from a posterior rather
// than from three thresholds; keeping both would be two rankings fighting over
// the same field. The strike ledger's REFUSALS are kept and merged, because a
// lane that answered 4xx thirty seconds ago is a fact no belief carries yet.
//
// THE SORT WORD COMES OFF when an order goes on, because the router reads them
// together and `order` already says what to try first.
//
// AND THE AFFINITY PIN STAYS IN FRONT (affinity.go). The chooser prices the
// prompt cache it can see — a lane this process watched answer this lineage —
// but the pin knows one thing the belief does not: that this lineage asked to
// come back, whatever the ledger has since forgotten. A cold prefix cost 4.7×
// a warm one in the cost autopsy, which is more than any lane's tariff differs
// from another's, so the pin keeps its place until the belief is proven against
// it.
func (c *Client) applyLaneChoice(prefs *providerPrefs, model string, knobs callKnobs, request *ai.Request, pinned string) {
	if prefs == nil || request == nil || !c.isOpenRouter() {
		return
	}
	choice, made := c.laneChoiceFor(knobs, model, request)
	if !made {
		return
	}
	// A DEMAND IS NOT A RANKING, so it replaces the object rather than joining
	// it. `Only` is what a pin sends (lanepin.go): the request goes to exactly
	// that machine or it does not go, which is the sentence the picker and the
	// manual both promise — "every request for this conversation goes to that
	// lane and nowhere else". The sort word comes off because there is nothing
	// left to sort, the ledger's order comes off because it is a ranking over
	// machines this request may not use, and `allow_fallbacks` goes to false
	// because a fallback is precisely the thing a pin refuses.
	if len(choice.Only) > 0 {
		only := make([]string, 0, len(choice.Only))
		for _, lane := range choice.Only {
			if lane != "" {
				only = append(only, lane)
			}
		}
		if len(only) > 0 {
			no := false
			prefs.Only, prefs.Order, prefs.Sort = only, nil, ""
			prefs.AllowFallbacks = &no
			return
		}
	}
	if len(choice.Order) > 0 {
		order := make([]string, 0, len(choice.Order)+1)
		// AND THE AFFINITY PIN YIELDS TO A PERSON'S OWN. It leads the order for
		// the reason stated above — a warm prefix is worth more than any lane's
		// tariff — but it is a heuristic about a cache, and a borrowable lane
		// pin is somebody naming the machine they want. Letting the cache jump
		// the person would make `pinned: cloudflare, borrow when slow` mean "go
		// wherever the last answer came from", which is not what the row says.
		if pinned != "" && !strings.EqualFold(pinned, CurrentLanePin().pinned()) {
			order = append(order, pinned)
		}
		for _, lane := range choice.Order {
			if lane != "" && lane != pinned {
				order = append(order, lane)
			}
		}
		prefs.Order = order
		prefs.Sort = ""
	}
	for _, lane := range choice.Ignore {
		if lane == "" || lane == pinned || namesEndpoint(prefs.Order, lane) || namesEndpoint(prefs.Ignore, lane) {
			continue
		}
		prefs.Ignore = append(prefs.Ignore, lane)
	}
}

// laneChoiceFor is the preference this request goes out on: the one the call
// already decided, or a fresh one for a call that decided none.
//
// A STREAMED CALL DECIDES ONCE, IN client.go, BEFORE ANYTHING IS SENT — because
// the watch that may hedge it and the encoder that writes `provider.order` have
// to agree about which lane was asked for, and the choice is a sampled decision
// that answers differently every time it is asked. A non-streamed call has no
// watch to agree with, so it decides here, which is the last moment the model
// and the shaped request are both known.
func (c *Client) laneChoiceFor(knobs callKnobs, model string, request *ai.Request) (lanes.Choice, bool) {
	if knobs.laneChoice != nil {
		return *knobs.laneChoice, true
	}
	// WHAT A PERSON SAID IS READ BEFORE THE BELIEF IS ASKED (lanepin.go), and
	// two of the three rows never reach the chooser at all.
	//
	// `openrouter` is a person asking for NO lane, so there is no choice to
	// make: the request goes out shaped exactly as it was before this package
	// existed — the sort word, the strike ledger's order, the price ceiling —
	// and with no Choice on the context nothing downstream hedges either
	// ([Client.raceFor] reads the same absence).
	//
	// A STRICT PIN SENDS ONLY THE MACHINE IT NAMES, and that is the difference
	// between a pin and a preference: the order comes off, fallbacks come off,
	// and nothing may quietly route around what a person asked for — which is
	// what "and nowhere else" means when the pinned lane is slow.
	//
	// IT STILL CARRIES THE FRONTIER, and that is not a contradiction. A pin is
	// ASKED rather than overridden: when the machine goes quiet the surface says
	// `coreweave is slow · switch to auto? (y)`, and a question that cannot name
	// where a `y` would go is a question nobody can answer. So the candidate set
	// is computed exactly as it is for any other call and sent to nobody: it is
	// what [control.Plan] points an offer at, and the wire still asks for `Only`.
	pin := CurrentLanePin()
	if pin.OpenRouter {
		return lanes.Choice{}, false
	}
	strategy := c.routingFor(knobs.intent)
	if strategy == RoutingOff {
		return lanes.Choice{}, false
	}
	lambda := c.laneValueOfTime(knobs)
	ask := c.laneRequest(model, knobs, request, lambda)
	c.rememberAsk(ask)
	choice := lanes.Default().Chooser().Choose(ask)
	if named := pin.pinned(); named != "" && !pin.Borrow {
		// The candidate set survives and the ranking does not: see above.
		return lanes.Choice{Only: []string{named}, Frontier: choice.Frontier}, true
	}
	// AND A PIN THAT MAY BE BORROWED IS A PREFERENCE, so it goes in front of
	// the belief's own ranking rather than replacing it: the named machine is
	// asked first, fallbacks stay on, and the Alt the chooser named is left
	// where it is so a rescue has somewhere to go when the pin stalls.
	if named := pin.pinned(); named != "" {
		choice.Order = append([]string{named}, withoutEndpoint(choice.Order, named)...)
	}
	return choice, !choice.Empty()
}

// withLaneChoice decides this call's lane preference and carries it on the
// context, so that the watch and the wire are looking at the same choice. See
// [Client.laneChoiceFor] for why it is decided once rather than per encode.
func (c *Client) withLaneChoice(ctx context.Context, request *ai.Request) context.Context {
	if _, made := laneChoiceFromContext(ctx); made {
		return ctx
	}
	if request == nil || !c.isOpenRouter() {
		return ctx
	}
	choice, made := c.laneChoiceFor(knobsFrom(ctx), c.modelFor(request), request)
	if !made {
		return ctx
	}
	return WithLaneChoice(ctx, choice)
}

// namesEndpoint reports whether a preference list already names an endpoint.
func namesEndpoint(list []string, name string) bool {
	for _, held := range list {
		if held == name {
			return true
		}
	}
	return false
}

// ── FEEDING THE BELIEF ──────────────────────────────────────────────────────

// laneAsk is what the last request for one model told the chooser about itself.
//
// WHY IT IS REMEMBERED AT ALL. A sighting arrives at [Client.noteVelocity] with
// the timings and the endpoint that served, and with none of the request's own
// facts: the seam carries no context and no usage frame. Two of those facts are
// worth keeping — how long the prompt was, and which conversation it belonged
// to — because the first is what tells a slow lane apart from a long prefill
// and the second is what makes the next choice cache-aware. They are the
// ESTIMATES the encoder computed a moment earlier rather than the exact figures
// of the usage frame, which do not reach this seam; when the stream loop grows
// a seam that carries the frame, this becomes the frame.
type laneAsk struct {
	prompt int
	prefix string
	at     time.Time
}

// rememberAsk keeps the last ask per model, so the answer can be attributed.
func (c *Client) rememberAsk(ask lanes.Request) {
	if ask.Model == "" {
		return
	}
	c.laneAsks.mu.Lock()
	defer c.laneAsks.mu.Unlock()
	if c.laneAsks.last == nil {
		c.laneAsks.last = map[string]laneAsk{}
	}
	c.laneAsks.last[ask.Model] = laneAsk{prompt: ask.PromptTokens, prefix: ask.Prefix, at: ask.Now}
}

// askFor reads back what the last request for a model said about itself.
func (c *Client) askFor(model string) laneAsk {
	c.laneAsks.mu.Lock()
	defer c.laneAsks.mu.Unlock()
	return c.laneAsks.last[model]
}

// noteLane folds one timed answer into the belief, and remembers which
// conversation this lane now holds the prompt cache for.
//
// It is called from [Client.noteVelocity] and under the same gate: a session
// that asked for no routing preference is not measured either. An answer whose
// server did not identify itself teaches nothing here — the attribution law the
// ledger keeps, for the reason it keeps it: crediting an anonymous measurement
// to some lane is how a belief learns a fact about a machine that was never
// asked.
func (c *Client) noteLane(model, served string, ttft time.Duration, tokens int, generation, gap time.Duration, cached int) {
	served = strings.TrimSpace(served)
	model = laneModel(model)
	if !c.isOpenRouter() || model == "" || served == "" {
		return
	}
	id := lanes.ID{Model: model, Lane: served}
	now := laneNow()
	ask := c.askFor(model)
	lanes.Default().Ledger().Note(lanes.Sighting{
		ID:           id,
		TTFT:         ttft,
		Gen:          generation,
		Gap:          gap,
		Tokens:       tokens,
		PromptTokens: ask.prompt,
		// AND WHAT THE ROUTER SAID IT READ BACK OUT OF THIS LANE'S CACHE. It is
		// the usage frame's own figure and never the estimate beside it: the
		// prompt length above is what this adapter computed before the send,
		// and a cache hit invented from it would be a belief that a lane holds
		// our prefix on evidence that says nothing about any lane at all.
		CachedTokens: cached,
		At:           now,
	})
	lanes.RememberPrefix(id, ask.prefix, now)
}

// lanesState is the small mutable half of this file: the last ask per model.
// It is on the client rather than in a package variable because two clients in
// one process talk to two routers, and a prompt one of them sent is not
// evidence about the other's lanes.
type lanesState struct {
	mu   sync.Mutex
	last map[string]laneAsk
}

// ── THE ONE THING internal/lane MAY NOT OWN ─────────────────────────────────

// sheetTimeout bounds one sheet fetch. It is the fifteen seconds the catalog
// reader already uses for the same router over the same connection
// (internal/catalog's fetch), and it is stated as one figure because a beat
// that hangs is a beat that stops beating: nothing waits on this, so the only
// thing a longer deadline could buy is a goroutine parked on a dead socket.
const sheetTimeout = 15 * time.Second

// sheetFetcher is the connection `internal/lane` is forbidden to open for
// itself, handed to it at construction through [lanes.WireSheet].
//
// A STRUCTURAL TEST IN THAT PACKAGE FAILS THE BUILD IF IT SO MUCH AS IMPORTS A
// TRANSPORT, and it is right to: a package that could open a connection is a
// package where somebody eventually opens one on the send path. So this is the
// whole of the transport behind the sheet — one GET, the house headers, and a
// status that is not a success turned into an error rather than into forty
// kilobytes of somebody's HTML.
//
// It carries its own client rather than the adapter's, because the adapter's
// two are shaped for a conversation: one has the caller's configured timeout on
// it and the other deliberately has none at all, so that a stream may run for
// as long as an answer takes. Neither is the right shape for a background read
// of a small JSON document.
type sheetFetcher struct {
	// http is the client this fetch rides. It is a field only so a test can
	// hand in one pointed at an httptest server; nil is the real one.
	http *http.Client
}

// Fetch GETs url and hands back its body, which the caller closes.
func (f sheetFetcher) Fetch(ctx context.Context, url, bearer string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	// AN EMPTY BEARER IS A REAL STATE AND NOT A MISTAKE. The sheet is a public
	// document, and a client may be built before the person has pasted a key
	// (see [NewClient]); sending `Bearer ` with nothing after it is how a
	// request that would have worked earns a 401.
	if bearer = strings.TrimSpace(bearer); bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	// And this read is attributed like every other read this binary makes of
	// the router — the values are the package's own constants and no caller
	// carries them (attribution.go).
	ApplyAttribution(request.Header)
	client := f.http
	if client == nil {
		client = &http.Client{Timeout: sheetTimeout}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// The body is read a little before the connection is closed, for two
		// reasons that happen to want the same bytes: draining it sends the
		// connection back to the pool rather than tearing it down, which is the
		// courtesy the catalog reader pays on the same host, and a 404 is read
		// for what it says below. The cap is [sheetRefusalBytes] either way.
		payload, _ := io.ReadAll(io.LimitReader(response.Body, sheetRefusalBytes))
		response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			return nil, sheetNotFound(response.Status, payload)
		}
		// Everything else — a 500, a 429, a 502 from somebody's load balancer —
		// is a router having an afternoon, and it stays an ordinary error so
		// that one bad reply never costs a base its lanes for five minutes.
		return nil, fmt.Errorf("lane sheet: %s", response.Status)
	}
	return response.Body, nil
}

// sheetRefusalBytes bounds how much of a refused sheet fetch is read. A 404
// body that matters is a JSON envelope of under a hundred bytes; the router's
// own HTML page for a route that does not exist is a few kilobytes; nothing
// past four is evidence about anything, and reading it would be paying for
// somebody's error page to decide a thing the first line already decided.
const sheetRefusalBytes = 4 << 10

// errNoSheetForModel is a 404 the router answered ABOUT A MODEL: the endpoints
// route is there and this one model has no page on it. It is deliberately not
// [lanes.ErrNoSheetHere], and nothing in internal/lane reads it: a refresh that
// returns it is quiet (refreshAndPrime primes on nil and logs nothing) and
// leaves the base's answer exactly where it was.
var errNoSheetForModel = errors.New("lane sheet: the router publishes no page for this model")

// sheetNotFound reads what a 404 from the endpoints route MEANS, which is the
// one reading the sheet remembers a base by, and it is made by the body and
// never by the status alone.
//
// THE STATUS SAYS NOTHING ABOUT THE BASE; THE BODY DOES. Measured against the
// live router on 2026-09-02:
//
//	GET /api/v1/models/nonexistent/model-xyz/endpoints
//	→ 404, {"error":{"message":"Not Found","code":404}}
//
//	GET /api/v1/nonexistent-route/x/endpoints
//	→ 404, <!DOCTYPE html>…<title>Not Found | OpenRouter</title>…
//
// The first is the router's own error envelope: the route exists, it answered
// about the model, and the model simply has no page — [errNoSheetForModel],
// which marks NOTHING about the base. Reading it as "no page here" was the
// defect class of #373 in a new coat: on any base that is not the shipped
// router (where LaneSheetCertain skips the asking) a first beat model the
// router does not publish would have held the WHOLE BASE sheetless for five
// minutes, a wrong answer about the base derived from one request. The second
// is not the envelope — an HTML page, a proxy framework's own not-found, a
// bare `404 page not found` — and it is the one answer that says there is no
// such route, so it is the one that wraps [lanes.ErrNoSheetHere].
//
// THE ENVELOPE IS DECODED ONCE IN THIS PACKAGE. The body goes through
// [apiError], the same raw-body decoder every refusal on the send path is
// recovered from ([RefusalFrom]), and "is this the router speaking" is the
// [APIError.Message] it filled in — never a second unmarshal of the same
// shape here. Once #368 lands, its Client.routingRefusal(model, status,
// payload) is the one function that decides what a 404 body means, and this
// read becomes a third answer on it — "no such route" — as an enum, in a
// follow-up on top of both.
func sheetNotFound(status string, payload []byte) error {
	if refused, ok := RefusalFrom(apiError(http.StatusNotFound, payload)); ok && refused.Message != "" {
		return fmt.Errorf("lane sheet: %s: %w: %w", status, errNoSheetForModel, refused)
	}
	return fmt.Errorf("lane sheet: %s: %w", status, lanes.ErrNoSheetHere)
}

// LaneSheetCertain reports whether base is KNOWN to publish a lane sheet
// without anybody having to ask it: the shipped router, recognised by its
// hostname.
//
// IT IS A HINT AND NEVER A REFUSAL. This build used to decide whether the
// endpoints page was fetched at all by this very substring test, so a binary
// driven through AFORGE_BASE_URL at a proxy, a mirror, a self-hosted router or
// the router reached by its IP silently got no sheet, an empty frontier and no
// lane behaviour whatever — nothing errored and nothing logged a refusal, the
// feature was simply absent (issue #373). A ROUTER IS RECOGNISABLE BY WHAT IT
// ANSWERS AND NEVER BY A SUBSTRING OF WHERE IT LIVES: every base is wired, and
// the sheet learns from the base's own first answer whether there is a page
// there ([lanes.ErrNoSheetHere]). What this hostname buys is only that the one
// base everybody already knows about skips straight to "serves", so the shipped
// path is unchanged in behaviour and pays not one extra round trip.
//
// IT IS THE BASE URL AND NEVER THE MODEL ID, which is where it parts company
// with [Client.isOpenRouter]. That one is also true of a client whose model is
// spelled `openrouter/...` behind somebody's own gateway, and it is right to
// be: the ledger still learns from what that gateway serves. But the sheet is
// fetched FROM THE BASE URL, so the base is the thing a hint can be about.
func LaneSheetCertain(base string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(base)), "openrouter.ai")
}

// WireLaneSheet points the live lane sheet at a base, with the bearer a fetch
// should carry. It is the one door through which internal/lane is handed a
// transport, and it is exported because two callers need it and a second
// spelling of it would drift: [NewClient] wires the base its client talks to,
// and the process's own beat (cmd/aforge's lanebeat.go) wires the base the
// settings name, because on three headless doors the beat starts before any
// client exists and a beat over an unwired sheet fetches nothing at all.
//
// Wiring is not a fetch — it hands the sheet a base, a bearer and something
// that can open a connection, and nothing goes to the network until a beat
// calls Refresh. Wiring the same base twice is harmless: the sheet keeps what
// that base already answered, and forgets it only when the base itself moves.
//
// EVERY NON-EMPTY BASE IS WIRED. Whether there is an endpoints page at it is
// the base's own to say, once, and the sheet remembers ([LaneSheetCertain]
// says why the hostname is a hint here and not the decision).
func WireLaneSheet(base, key string) {
	base = strings.TrimSpace(base)
	if base == "" {
		return
	}
	lanes.WireSheet(base, strings.TrimSpace(key), sheetFetcher{}, LaneSheetCertain(base))
}

// wireLaneSheet points the live lane sheet at the router this client talks to.
//
// IT IS CALLED FROM THE CONSTRUCTOR AND FROM NOWHERE ELSE IN THIS PACKAGE. It
// is still a write to a process-wide seam, and a write repeated per request
// is a lock taken in front of somebody's first token for no gain.
//
// A CLIENT BUILT WITHOUT A KEY STILL WIRES. `/models/{id}/endpoints` is a
// public document, so a session that opens on the first-run screen and is
// handed its key a minute later still has a prior for its first call; the key
// this carries is the one the client was constructed with, and the sheet does
// not chase [Client.SetAPIKey] because a bearer buys nothing on a public read.
func (c *Client) wireLaneSheet() {
	WireLaneSheet(c.config.BaseURL, c.config.APIKey)
}

// noteLaneOutcome folds one answer's USABILITY into the lane's belief, which is
// the quality axis internal/lane keeps beside its two timing ones.
//
// It is the other half of [Client.noteLane], and the two are separate because
// speed and usefulness are different claims about a machine. A lane that starts
// answering in four hundred milliseconds and then writes the model's own chat
// template out as text is a fast lane and a useless one; until an outcome
// reached the belief nothing could tell those apart, so the chooser ranked on
// time alone and sent the next request straight back to it. That is the whole
// of the measured failure this closes: an endpoint served a reply that had
// stopped being language, the stream was cut for it, and the retry landed on
// the same endpoint on equal footing.
//
// THE DECAY AND THE RECOVERY ARE THE LEDGER'S OWN AND NOT THIS FILE'S. A
// refusal ages back toward the lane's prior over [lanes.QualityHalfLife], and a
// usable answer walks the belief up again — so a bad stretch costs a lane its
// standing for about an hour rather than for the life of the process, and
// nothing here needs a penalty box or a timer of its own. The gate that reads
// it is the frontier's, against the role's own QualityNeed.
//
// The attribution law is [Client.noteLane]'s, word for word: an answer whose
// server did not name itself teaches nothing, because crediting it to a lane is
// how a belief learns a fact about a machine that was never asked. And it is
// under the same routing gate, for the reason velocity.go states about strikes:
// somebody who asked for no steering asked for no demotions either.
//
// Reason is a short machine-readable word for the log and never a sentence a
// person reads ([lanes.Outcome] says so itself).
func (c *Client) noteLaneOutcome(model, served, reason string, accepted bool) {
	if c.routing() == RoutingOff {
		return
	}
	served = strings.TrimSpace(served)
	model = laneModel(model)
	if !c.isOpenRouter() || model == "" || served == "" {
		return
	}
	lanes.Default().Ledger().NoteOutcome(lanes.Outcome{
		ID:       lanes.ID{Model: model, Lane: served},
		Accepted: accepted,
		Reason:   reason,
		At:       laneNow(),
	})
}

// answerOutcome reads a reply that survived every stream bound for the quality
// axis: the word the belief records, and whether the answer counts as service.
//
// A reply with no words and no tool call is NOT service. It passed every bound —
// the endpoint answered 200 and closed the connection tidily — and the turn loop
// has always read it as the failure it is (internal/session's journalError: "that
// is not a short answer, it is an endpoint that did not answer"). Counting it as
// a usable answer here would let a lane that returns nothing at speed walk its
// own standing back up, which is the precise shape of the failure the quality
// axis exists to catch.
func answerOutcome(response *ai.Response) (string, bool) {
	if response == nil {
		return "empty", false
	}
	if strings.TrimSpace(responseText(response)) != "" || len(response.ToolCalls()) > 0 {
		return "served", true
	}
	return "empty", false
}
