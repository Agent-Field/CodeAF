package provider

import (
	"context"
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
func (c *Client) laneValueOfTime(strategy RoutingStrategy, knobs callKnobs) float64 {
	if strategy == RoutingPrice {
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
		Model:        normalizeModel(model),
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
	strategy := c.routingFor(knobs.intent)
	if strategy == RoutingOff {
		return
	}
	lambda := c.laneValueOfTime(strategy, knobs)
	ask := c.laneRequest(model, knobs, request, lambda)
	c.rememberAsk(ask)
	choice := lanes.Default().Chooser().Choose(ask)
	if choice.Empty() {
		return
	}
	if len(choice.Order) > 0 {
		order := make([]string, 0, len(choice.Order)+1)
		if pinned != "" {
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
func (c *Client) noteLane(model, served string, ttft time.Duration, tokens int, generation, gap time.Duration) {
	served = strings.TrimSpace(served)
	model = normalizeModel(model)
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
