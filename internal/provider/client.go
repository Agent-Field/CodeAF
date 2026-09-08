package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Config configures the adapter. It is deliberately the same shape the
// AgentField SDK client takes, plus the two resolvers that let the adapter
// decide a request's economics without ever performing I/O on the hot path —
// and minus Temperature: an absent sampling parameter is omitted upstream and
// the provider's own default applies, so the field is absent rather than
// carried and always sent (withoutSampling covers the SDK's own loop, which
// injects one).
// It carries no attribution fields on purpose: who this binary reports itself
// as is a constant (attribution.go), and a config field for it is exactly how a
// caller ends up sending a different app — or none.
type Config struct {
	APIKey    string
	BaseURL   string
	Model     string
	MaxTokens int
	Timeout   time.Duration

	// SupportsParameter answers "does this model accept this request field?"
	// from data already in memory. It must not block or perform I/O; an unknown
	// answer is reported by returning known=false, never by waiting.
	SupportsParameter func(model, parameter string) (bool, bool)

	// ReasoningProfile answers what the provider published about a model's
	// thinking pass — whether it can be turned off, and which effort words the
	// model takes — from data already in memory, under the same contract as
	// SupportsParameter: never blocks, and an unknown is known=false. It is
	// the authority thinking.go reads first; the quirks memo is what stands in
	// when it is silent.
	ReasoningProfile func(model string) (ReasoningProfile, bool)

	// Routing says how this client asks the router to choose among the
	// endpoints serving one model (velocity.go). NIL IS NOBODY'S CHOICE, not a
	// choice of latency: the adapter then decides per request from who is
	// waiting on it, so a caller that has never heard of the row still chases
	// speed on a person's own turn and price on an errand. A caller that HAS
	// heard of it hands down the resolved setting rather than a path to it, and
	// that setting wins over everything.
	Routing RoutingSource

	// ModelPrice is the model's OWN published list price, per token in US
	// dollars, from rows already in memory. It is what the latency ask's price
	// ceiling is derived from (velocity.go's latencyPriceCeiling), and like
	// SupportsParameter it must not block or perform I/O: a catalog that has not
	// resolved answers known=false, which sends no ceiling at all.
	//
	// known=false is the ONLY way to say "no price". A published zero is a real
	// figure — the free variants a router carries — and must not be reported as
	// unknown.
	ModelPrice func(model string) (prompt, completion float64, known bool)

	// Fallbacks are the models to try, in order, when no endpoint serving the
	// configured one will accept the request's shape (endpoints.go). It is the
	// operator's own list and it wins outright over any inference; empty is the
	// ordinary case and means the catalog is asked instead.
	Fallbacks []string

	// NearestModels answers "what else could have taken this conversation?" from
	// data already in memory, and is consulted ONLY when Fallbacks is empty. Like
	// SupportsParameter it must not block or perform I/O — a catalog that has not
	// resolved answers nil, which is one more way of not knowing rather than a
	// reason to wait on the one path where somebody is already watching a failure.
	NearestModels func(model string) []string

	// HTTPClient is optional and exists for deterministic tests.
	HTTPClient *http.Client
}

// Client is Aforge's model adapter. It satisfies the harness's LoopClient and
// TextStreamer interfaces structurally, so nothing above it knows a wire
// format, and it owns the only outbound provider path in the process.
type Client struct {
	config Config
	http   *http.Client
	// stream is the same client with the total deadline removed. A streamed
	// answer is bounded by silence, not by duration — see send.
	stream *http.Client
	// keyMu guards the two fields under it, which are the only part of the
	// adapter that changes after construction: a key can arrive mid-session
	// ([Client.SetAPIKey]) while a request on another goroutine is being
	// encoded.
	keyMu sync.RWMutex
	// apiKey is the bearer every request carries, read per request rather than
	// out of config so that a key handed over after construction reaches the
	// very next call. Empty is a client that cannot send yet ([ErrNoAPIKey]).
	apiKey string
	// base is the pinned AgentField client, retained for the one surface this
	// adapter does not implement for itself: the tool-call loop against a plain
	// OpenAI-compatible endpoint. It never sees an OpenRouter request and never
	// sees a request the adapter has shaped — see ExecuteToolCallLoop for where
	// that boundary is drawn and why it is where it is. Nil while there is no
	// key, because the SDK refuses to be built without one.
	base *ai.Client
	// wait is the retry backoff, seamed exactly like the media client's video
	// poll: production sleeps, tests record what would have been slept and
	// return, so how long a retry waits is assertable without waiting.
	wait func(context.Context, time.Duration) error
	// velocity is what this process has measured about the endpoints serving
	// its models (velocity.go). It is consulted by the encoder immediately
	// before a send and written the moment an answer completes.
	velocity *velocityLedger
	// laneAsks is what the last request for each model told the belief about
	// itself (lanes.go). It is on the client because a prompt one router was
	// sent is not evidence about another's lanes.
	laneAsks lanesState
	// pins is which endpoint holds each prompt lineage's cache (affinity.go).
	// It is read at the same moment the velocity ledger is — encode time — and
	// written from the same answers, and the two never disagree: a lane the
	// velocity ledger refuses drops its pin rather than being asked for again.
	pins *endpointPins
	// now is the clock those measurements are taken against, seamed like wait
	// so a test can state a two-second first token without waiting two seconds.
	now func() time.Time
	// encodes is what this client already knows its transcript and its tool
	// block serialize to (memo.go). It changes nothing about the bytes and is
	// carried per client because a transcript belongs to a conversation.
	encodes encodeMemo
	// unstreamable is this endpoint admitting, from the wire, that it does not
	// speak server-sent events: a gateway that took `stream: true` and answered
	// one whole JSON completion anyway. It is set once, from what actually
	// happened.
	//
	// IT CHANGES THE TRANSPORT AND NEVER THE REQUEST. The bytes on the wire stay
	// byte-identical call to call — that determinism is a law of this adapter and
	// a memo that rewrote the body would break it — and what the memo buys is the
	// only thing worth buying: an endpoint that generates the whole answer before
	// it sends a header must not be read through the streaming transport, whose
	// header deadline would then be a deadline on the generation. Such a call is
	// bounded in total instead, by [adaptiveCompletionTimeout], because a total
	// deadline is the only bound an answer with no inside can have.
	//
	// It is a fact about the BASE URL rather than about a model, which is why it
	// lives on the client and not in the velocity ledger.
	unstreamable atomic.Bool
	// prefSent is whether any request from this client has really carried a
	// routing preference, which is what makes a later answer with no lane
	// information EVIDENCE about the base rather than a fact about a request
	// that never asked (prefcarry.go's [Client.prefWentOut]).
	prefSent atomic.Bool
}

// ErrNoAPIKey is what a request meets on a client built without a key and not
// yet handed one ([Client.SetAPIKey]). It is a value so the session can name the
// state to a person in its own words rather than matching a sentence.
var ErrNoAPIKey = errors.New("no API key: this session has not been given one yet")

// NewClient builds the adapter. It performs no network request.
//
// A CLIENT MAY BE BUILT WITHOUT A KEY. The chat surface opens on a profile with
// nothing in it and asks for the key on its first screen (internal/tui3's
// firstrun.go), so the session — and this adapter under it — has to exist
// before the key does. Every request refuses with [ErrNoAPIKey] until
// [Client.SetAPIKey] lands one; nothing is sent with an empty bearer. The other
// three fields are still required: they have defaults and a caller with none
// is a caller with a bug.
func NewClient(config Config) (*Client, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, errors.New("provider base URL is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("provider model is required")
	}
	if config.Timeout < 0 {
		return nil, errors.New("provider timeout must not be negative")
	}
	httpClient, streamClient := config.HTTPClient, config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Transport: SharedTransport(), Timeout: config.Timeout}
		streamClient = &http.Client{Transport: streamTransport()}
	}
	client := &Client{
		config:   config,
		http:     httpClient,
		stream:   streamClient,
		wait:     waitContext,
		velocity: sharedVelocity,
		pins:     sharedPins,
		now:      time.Now,
	}
	if err := client.SetAPIKey(config.APIKey); err != nil {
		return nil, err
	}
	// AND THE LANE SHEET LEARNS WHERE THE ROUTER IS, here and nowhere else
	// (lanes.go). It opens no connection: it hands `internal/lane` the base, the
	// bearer and the one thing that package may not own, and the fetching is a
	// beat the session starts and stops. A client pointed somewhere that is not
	// a router wires nothing, because there is no sheet there to read.
	client.wireLaneSheet()
	return client, nil
}

// SetAPIKey hands the adapter the key its requests ride from now on: the one a
// person pasted on the first-run screen or into the settings row, arriving
// while this client is already the conversation's.
//
// The SDK client under the plain-OpenAI loop is rebuilt here rather than
// patched, because it validates its key at construction and holds it
// privately; while there is no key it is simply absent, and the one path that
// needs it says so (ExecuteToolCallLoop). The adapter's own transport reads the
// key per request under the lock, so a request already in flight keeps the
// bearer it was encoded with and the next one carries the new key.
func (c *Client) SetAPIKey(key string) error {
	key = strings.TrimSpace(key)
	var base *ai.Client
	if key != "" {
		built, err := ai.NewClient(&ai.Config{
			APIKey:    key,
			BaseURL:   c.config.BaseURL,
			Model:     c.config.Model,
			MaxTokens: c.config.MaxTokens,
			Timeout:   c.config.Timeout,
			SiteURL:   AppURL,
			SiteName:  AppName,
		})
		if err != nil {
			return err
		}
		base = built
	}
	c.keyMu.Lock()
	defer c.keyMu.Unlock()
	c.apiKey, c.base = key, base
	return nil
}

// apiKeyNow is the key the next request carries, or "" with [ErrNoAPIKey].
func (c *Client) apiKeyNow() (string, error) {
	c.keyMu.RLock()
	defer c.keyMu.RUnlock()
	if c.apiKey == "" {
		return "", ErrNoAPIKey
	}
	return c.apiKey, nil
}

// sdkClient is the pinned SDK client, or nil while there is no key.
func (c *Client) sdkClient() *ai.Client {
	c.keyMu.RLock()
	defer c.keyMu.RUnlock()
	return c.base
}

// Model reports the adapter's default model slug.
func (c *Client) Model() string { return c.config.Model }

// OwnsToolLoop tells the harness that this client's transport is harness-owned,
// so the bounded safe loop — horizon compaction, stall detection, the tool
// membrane — drives it rather than a provider-side loop.
func (c *Client) OwnsToolLoop() bool { return true }

// ExecuteToolCallLoop satisfies the harness's LoopClient interface. Aforge
// ordinarily drives its own loop against this adapter, so this is a contract
// detail rather than the live chat path — but it is a REACHABLE one, and where
// it goes is the SDK boundary.
//
// ── THE SDK BOUNDARY ────────────────────────────────────────────────────────
//
// On OpenRouter nothing below this line is the SDK's. The loop runs over this
// adapter's own transport (openrouter_client.go), which is the only way a
// request can carry X-OpenRouter-Categories — the SDK's client has no field for
// it — and the only way a refused belt reaches the endpoint-refusal ladder
// instead of ending the turn on a 404.
//
// c.base is for everything else: an operator pointed at a plain OpenAI-
// compatible endpoint, where the SDK's loop is a working implementation this
// package has no reason to duplicate. It never sees an OpenRouter request, and
// it never sees a request this adapter shaped. The SDK module itself is
// read-only and is not edited to make any of this true.
func (c *Client) ExecuteToolCallLoop(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
	config ai.ToolCallConfig,
	call ai.CallFunc,
	options ...ai.Option,
) (*ai.Response, *ai.ToolCallTrace, error) {
	// THE HINT AND NOT THE PREFERENCE ANSWER. Which of two transports drives
	// the loop is a fact about the SHIPPED ROUTER's own dialect — the
	// categories header, the refusal ladder — and not about whether some base
	// carries a `provider` object (prefcarry.go).
	if c.shippedRouterHint() {
		return c.executeOwnToolCallLoop(ctx, messages, tools, config, call, options...)
	}
	base := c.sdkClient()
	if base == nil {
		return nil, nil, ErrNoAPIKey
	}
	// The SDK builds every round's request with its configured temperature —
	// its config supplies one even when nobody asked — so the option that
	// undoes the injection goes on LAST, after anything a caller passed: a
	// sampling decision this adapter has promised not to make.
	return base.ExecuteToolCallLoop(ctx, messages, tools, config, call, append(options, withoutSampling)...)
}

// withoutSampling removes the one sampling parameter the SDK's request
// builders always set. The SDK is read-only here and its temperature cannot
// be configured AWAY — a zero would be sent as zero — so the answer is an
// option that takes the field back off, leaving the provider's own default
// to answer. Everything else the SDK injects stays.
func withoutSampling(request *ai.Request) error {
	request.Temperature = nil
	return nil
}

// maxResponseBytes bounds what one completion may be believed to be. A
// completion is text and a cap this far above any real answer changes nothing
// about a working provider; what it removes is the unbounded case, where a
// misrouted endpoint streaming something else entirely is read into memory in
// full before anyone looks at it.
const maxResponseBytes = 64 << 20

type callKnobs struct {
	cacheKey string
	effort   effortRequest
	// intent is whether a person is waiting on this call (velocity.go). It is
	// resolved once here, at the top of the call, rather than at encode time,
	// because it is a fact about the CALLER and cannot change between the two.
	intent RoutingIntent
	// lambda is what a second is worth to whoever is waiting on this call, in
	// seconds per dollar, and whether the call site said (lanes.go). It is
	// resolved here with the intent because it is the same kind of fact about
	// the same caller.
	lambda secondsPerDollar
	// horizon is roughly how many more calls the work this call belongs to
	// expects to make. It sizes exploration and nothing else (lanes.go).
	horizon int
	// relaxed is what this encode has been told to leave off the body, set only
	// by the endpoint-refusal chain (endpoints.go). Zero on every ordinary call,
	// which is what keeps a healthy request byte-for-byte what it always was.
	relaxed relaxSet
	// reasoning is aligned with the request's messages. It stays outside the SDK
	// values because ai.Message has no reasoning fields of its own.
	reasoning []MessageReasoning
	// noProvider takes the `provider` object OFF this one encode entirely, and
	// it is set by exactly one caller: the single widened retry that asks
	// whether a base's 400 was about the field at all (endpoints.go's
	// [Client.widenPastTheUncarriedPreference], issue #433).
	//
	// IT IS NOT A LADDER RUNG. [relaxEndpointFilter] takes off everything that
	// can EXCLUDE an endpoint and leaves the sort word, which is the right rung
	// when the question is which machine can serve a shape. Here the question is
	// whether the base understands the field, so the field has to be gone —
	// otherwise the retry asks the same question again and its answer means
	// nothing.
	noProvider bool
	// hedgeLane is the one lane this request must go to, set only on the second
	// request of a hedged pair (hedge.go). Empty on every ordinary call, which
	// is what keeps a healthy request byte-for-byte what it always was.
	hedgeLane string
	// laneChoice is the lane preference this call was decided on, nil when
	// nobody decided one — which is every call in a build where the router is
	// not wired in, and every non-streamed call, which has no watch to agree
	// with and makes its own inside the encoder.
	laneChoice *lanes.Choice
	// trace is what ONE CALL accumulates on its way to an answer — how many
	// times it went out, what its refusals taught, the body it last carried —
	// for the model-call log (calllog.go). It is a pointer because the knobs
	// travel by value through the repair and relax chains, and the fact the
	// completed record needs is the count across all of them.
	trace *callTrace
}

func knobsFrom(ctx context.Context) callKnobs {
	knobs := callKnobs{
		cacheKey:  CacheKeyFrom(ctx),
		effort:    effortFrom(ctx),
		intent:    routingIntentFrom(ctx),
		lambda:    valueOfTimeFrom(ctx),
		horizon:   callHorizonFrom(ctx),
		hedgeLane: hedgeLaneFrom(ctx),
		reasoning: MessageReasoningFrom(ctx),
		trace:     newCallTrace(),
	}
	// The choice this call was already made on, if it was. See
	// [Client.withLaneChoice]: it is carried rather than recomputed because it
	// is a sampled decision and two draws are two different answers.
	if choice, made := laneChoiceFromContext(ctx); made {
		knobs.laneChoice = &choice
	}
	return knobs
}

// modelFor names the model a request will actually run against: the one the
// router pinned, or the adapter's own default when nothing pinned one.
func (c *Client) modelFor(request *ai.Request) string {
	if model := strings.TrimSpace(request.Model); model != "" {
		return model
	}
	return c.config.Model
}

// sendShaped is the whole of what this adapter does to make one request land:
// the self-repairable 400s below, and then the endpoint refusals above them.
//
// The two are separate passes because they are different mistakes. A repairable
// 400 is a knob the adapter guessed wrong about and can simply stop sending,
// once, silently, at no cost to anybody. A refusal — "no endpoints found that
// can handle the requested parameters" — is the request's whole SHAPE being
// unservable, and the answer to it is a narrated ladder the person watches
// (endpoints.go), because every rung of it takes away something they may care
// about having sent.
//
// And a third thing, which is not about the request's shape at all: a provider
// that paced this call until its patience ran out. That call has no answer and
// no shape to fix, so the only door left is another model — the same chain,
// through [Client.recoverFromPacing], which hands the error straight back when
// there is no chain to walk.
//
// IT IS ALSO WHERE A FAILED REQUEST LETS GO OF ITS ENDPOINT. Every way a send
// can fail passes through here exactly once — a transport error, a 4xx, a 5xx,
// a refusal the ladder could not repair — and a lineage pinned to an endpoint
// that just failed it moves (affinity.go's releaseEndpoint). The check is on the
// way out rather than at each return so that no future rung can be added past
// it and quietly keep a dead pin.
func (c *Client) sendShaped(ctx context.Context, request *ai.Request, knobs callKnobs, stream bool) (*http.Response, error) {
	// AND ANYTHING A PERSON IS STILL OWED IS SAID HERE, on the first request of
	// theirs that goes out after it was learned (lanepin.go's
	// [tellRetiredPins]). It is one nil check on a call nobody is reading and
	// on every call after the sentence has been said.
	tellRetiredPins(ctx)
	tellUncarriedPins(ctx)
	response, err := c.sendRecovered(ctx, request, knobs, stream)
	if err != nil || (response != nil && response.StatusCode >= 400) {
		c.releaseEndpoint(ctx, c.modelFor(request))
	}
	return response, err
}

// sendRecovered is sendShaped's two recovery passes — the repairable 400s and
// the endpoint-refusal ladder — with nothing said about pins.
func (c *Client) sendRecovered(ctx context.Context, request *ai.Request, knobs callKnobs, stream bool) (*http.Response, error) {
	// The moment the call really left, kept because the ONE recovery below that
	// answers a refusal by sending a different request has to write the refused
	// one down first (calllog.go's logNow, and [Client.widenPastTheRetiredPin]).
	began := logNow()
	response, err := c.sendRepaired(ctx, request, knobs, stream)
	if err != nil {
		return c.recoverFromPacing(ctx, request, knobs, stream, err)
	}
	if !endpointRefusalStatus(response.StatusCode) {
		return response, nil
	}
	model := c.modelFor(request)
	peek, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
	// WHETHER THE BASE UNDERSTANDS THE `provider` FIELD AT ALL IS ASKED BEFORE
	// ANYTHING ELSE (#433), because until it is answered nothing below this line
	// is reading the right refusal. It is a QUESTION here and an answer only
	// after the retry: the same request goes out once without the object, and
	// whether THAT lands is the whole of the evidence.
	if readErr == nil && c.prefsMayBeRefused(model, response.StatusCode, peek) {
		response.Body.Close()
		return c.widenPastTheUncarriedPreference(ctx, request, knobs, stream, began, response.StatusCode, peek)
	}
	if readErr != nil || !c.routingRefusal(model, response.StatusCode, peek) {
		// Not this class. The body is handed back whole — a peek must never
		// shorten what the caller goes on to read.
		response.Body = rewound(peek, response.Body)
		return response, nil
	}
	status := response.StatusCode
	response.Body.Close()
	// THE REFUSAL IS CLASSIFIED ONCE, HERE, AND ACTED ON BEFORE EITHER RECOVERY
	// RUNS. This line is the fork EVERY routing refusal passes through — the
	// walk takes one road out of it and the ladder the other, and both are
	// below here — which is why the two things that are true whatever happens
	// next are true at this line and not on one of the two roads.
	//
	// THE STRIKE IS THE FIRST OF THEM, and it was missing (issue #456). It
	// fired only where a refusal surfaced to a caller as a 4xx
	// ([Client.refuseUpstream]'s three call sites); a routing refusal the
	// LADDER absorbed never reached one, so the machine that had just said it
	// cannot serve this model was left standing in the serving set and the next
	// turn chose it again. "This machine refused this model" is true whatever
	// the recovery below does with the request, and it is recorded at the seam
	// where it is known.
	refusal := c.refusalObject(request, knobs, apiError(status, peek))
	c.strikeRefusal(model, refusal)
	carriedCeiling := c.carriedCeiling(model, knobs, request)
	// THE MEMO IS WRITTEN AT THE REFUSAL, BEFORE ITS RECOVERIES PART.
	// A funded walk may keep this arm out of the ladder, but its very next arm is
	// still a request to the same model and must not repeat the ceiling the router
	// has already refused. The router's STRUCTURED refusal is the evidence; its
	// sentence is never a gate on this behaviour.
	if c.velocity != nil && carriedCeiling {
		c.velocity.refuseCeiling(model)
	}
	// AND THE SAME MEMO IS TAKEN FOR THE IGNORE LIST, from the only authority on
	// how many machines serve a model. This process holds the lanes it has timed
	// and no denominator, so it cannot tell a veto that narrowed a set of five
	// from one that emptied a set of one — until the router says which it was,
	// in this sentence. It is written here, once, and read on every later encode
	// ([velocityLedger.keepTheSetServable]), so the second request for this
	// model is shaped right rather than paying the same instant refusal again.
	//
	// AND ONLY WHEN A VETO OF OURS WAS IN PLAY. The router says this same
	// sentence when the ignored providers on somebody's ACCOUNT empty the set,
	// and a refusal we had no hand in teaches us nothing about our own list. The
	// ledger is asked rather than the object rebuilt, because rebuilding it here
	// would expire cooldowns and redraw a sampled choice on the way back
	// ([velocityLedger.holdsVetoes]).
	if c.velocity != nil && ignoredEverything(peek) && c.velocity.holdsVetoes(model) {
		c.velocity.refuseCoveringIgnore(model)
	}
	// AND THE SECOND IS A PERSON'S OWN PIN (lanepin.go, issue #456). A pin the
	// router says it cannot serve for this model is stood down for that model,
	// once, and the person is told in a sentence that stays.
	//
	// THE CALL THAT RETIRED IT WIDENS AND TRIES AGAIN RATHER THAN WALKING. One
	// retry, at rung one, which is the rung that takes the whole provider
	// object off — so the request that goes out is exactly the request `auto`
	// would have sent. It is not a re-entry into the ladder: the demand has
	// just been withdrawn, so what was wrong with this request is already
	// fixed, and climbing on to strip the reasoning knob and the tools would be
	// paying for a diagnosis nobody needs. And it is not the walk either: the
	// walk is gated on a purse and on a frontier, and when it declines there is
	// nothing underneath it — which is how the reported turn died with the
	// person's brief unanswered and the router's own sentence on the screen.
	if retirePinnedLane(ctx, model, refusal) {
		return c.widenPastTheRetiredPin(ctx, request, knobs, stream, began, status, peek)
	}
	// THE LANE IS TRIED BEFORE THE REQUEST IS RELAXED. A refusal is a fact
	// about the MACHINE that made it — one endpoint behind a model drops tool
	// calls, another has no room for the output cap, a third has the reasoning
	// knob switched off — and the model's other machines have said nothing.
	// Relaxing here would answer a question nobody asked (tools taken off a
	// request that only needed a different endpoint) while an endpoint that
	// would have taken it whole sat untried. So while the race still has a
	// serving lane that the purse will fund, the refusal is handed back and the
	// walk takes that machine (hedge.go's walk); the ladder runs on the LAST arm
	// the purse will fund — or on the primary when it will fund none — where the
	// evidence really is about the request rather than the endpoint.
	// That is rungs two and three of the ladder in docs/ARCHITECTURE.md, in the
	// order they are written down.
	if streamWatchFrom(ctx).canWalk() {
		return &http.Response{
			StatusCode: response.StatusCode,
			Header:     response.Header,
			Body:       rewound(peek, io.NopCloser(strings.NewReader(""))),
		}, nil
	}
	return c.recoverFromRefusal(ctx, request, knobs, stream, peek, carriedCeiling)
}

// sendRepaired encodes the request and sends it, recovering once from the 400s
// this adapter can answer by itself.
//
// They are the same shape of mistake: a request-shape decision made HERE, on a
// field the catalog cannot vouch for. That includes a disable a model refuses,
// a thinking budget or cache marker an endpoint does not accept, and assistant
// reasoning replay an OpenAI-compatible endpoint does not implement. They are
// repaired here because anywhere else they become a failed node the operator
// has to reconfigure around.
//
// The retry costs nothing: a 400 generated no tokens, and the answer is
// remembered so only the first call on a model pays for the discovery.
func (c *Client) sendRepaired(ctx context.Context, request *ai.Request, knobs callKnobs, stream bool) (*http.Response, error) {
	body, err := c.encodeRequest(request, knobs)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	began := logNow()
	response, err := c.send(ctx, request, knobs, body, stream)
	if err != nil || !endpointRefusalStatus(response.StatusCode) {
		return response, err
	}
	model := c.modelFor(request)
	// Two repairs read the body, and they are told apart by what they cost the
	// memo. A learned quirk is a 400 about a knob this adapter chose. Foreign
	// reasoning is a 404 (OpenRouter's spelling) about content the transcript
	// carried from a model the person has since switched away from — it is
	// true of this conversation, not of the model, so it is fixed for this
	// request and remembered nowhere.
	foreign := len(knobs.reasoning) > 0
	repairable := response.StatusCode == http.StatusBadRequest && c.repairable(model, knobs)
	if !foreign && !repairable {
		return response, nil
	}
	peek, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
	switch {
	case readErr != nil:
	case foreign && refusesForeignReasoning(peek):
		// THE WORDS STILL GO; ONLY THE OTHER MODEL'S THINKING STAYS HOME. A
		// sidecar tagged with its model never gets here (reasoning.go's
		// producedElsewhere); this is the journal written before the tag.
		//
		// The refused shape gets its own row before the repair, because "the
		// call that went out first was refused and the one that came back was a
		// different request" is precisely the fact a log with only the answer
		// on it cannot tell anybody (calllog.go).
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: stream,
			attempt: c.attemptsSoFar(knobs), began: began,
			status: response.StatusCode, err: apiError(response.StatusCode, peek),
			responseBody: peek,
		})
		knobs.reasoning = nil
		return c.resend(ctx, request, knobs, stream, response)
	case repairable:
		if learned := c.learn(model, knobs, peek); len(learned) > 0 {
			knobs.trace.note(learned...)
			c.record(recordFacts{
				ctx: ctx, request: request, knobs: knobs, stream: stream,
				attempt: c.attemptsSoFar(knobs), began: began,
				status: response.StatusCode, err: apiError(response.StatusCode, peek),
				learned: learned, responseBody: peek,
			})
			return c.resend(ctx, request, knobs, stream, response)
		}
	}
	// Not ours to fix. The body is handed back whole — the caller still has
	// to read the provider's own words to build the error it reports.
	response.Body = rewound(peek, response.Body)
	return response, nil
}

// resend closes a refused answer and sends the request again as the knobs now
// say to shape it. The refusal generated no tokens, so the retry is free.
func (c *Client) resend(ctx context.Context, request *ai.Request, knobs callKnobs, stream bool, refused *http.Response) (*http.Response, error) {
	refused.Body.Close()
	body, err := c.encodeRequest(request, knobs)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	return c.send(ctx, request, knobs, body, stream)
}

// repairable reports whether this request carried a knob whose refusal this
// adapter knows how to answer. It is asked before the error body is touched, so
// a 400 that could not be ours costs no extra read.
func (c *Client) repairable(model string, knobs callKnobs) bool {
	return c.resolveEffort(model, knobs.effort) == EffortOff ||
		c.resolveReasoningBudget(model, knobs.effort) > 0 ||
		c.dialectFor(model) == cacheDialectBreakpoints ||
		(len(knobs.reasoning) > 0 && !reasoningReplayRefused(model))
}

// learn reads a refusal for the facts this adapter can remember and names each
// one it learned, so that an empty list is "the next encode will be the same
// request" and a non-empty one is both the decision to resend AND the line the
// model-call log writes about why. Every memo is consulted rather than the
// first match winning, because a single 400 can name more than one field.
func (c *Client) learn(model string, knobs callKnobs, payload []byte) []string {
	var learned []string
	if c.resolveEffort(model, knobs.effort) == EffortOff && refusesDisabledReasoning(payload) {
		noteReasoningMandatory(model)
		learned = append(learned, learnedReasoningMandatory)
	}
	// THE BUDGET IS DROPPED AND THE LEVEL IS KEPT. An endpoint that will not take
	// a thinking allowance still takes the effort word, so the two top rungs of
	// the ladder degrade to the deepest thing this endpoint has a word for
	// instead of falling off it (wire.go's resolveReasoningBudget).
	if c.resolveReasoningBudget(model, knobs.effort) > 0 && refusesReasoningBudget(payload) {
		noteReasoningBudgetRefused(model)
		learned = append(learned, learnedReasoningBudgetRefused)
	}
	if c.dialectFor(model) == cacheDialectBreakpoints && refusesCacheControl(payload) {
		noteCacheControlRefused(model)
		learned = append(learned, learnedCacheControlRefused)
	}
	if len(knobs.reasoning) > 0 && !reasoningReplayRefused(model) && refusesReasoningReplay(payload, knobs.reasoning) {
		noteReasoningReplayRefused(model)
		learned = append(learned, learnedReasoningReplayRefused)
	}
	return learned
}

// attemptsSoFar is which attempt a row about one refused shape belongs to. It
// reads the transport's own count rather than keeping a second one, and answers
// 1 for a call whose knobs carry no trace — a document request, or a test that
// built its knobs by hand.
func (c *Client) attemptsSoFar(knobs callKnobs) int {
	if knobs.trace == nil || knobs.trace.attempts <= 0 {
		return 1
	}
	return knobs.trace.attempts
}

// rewound puts an already-read prefix back in front of a body, so peeking at a
// response cannot shorten what the caller goes on to read. Close still closes
// the underlying body, which is the half that owns a connection.
func rewound(peek []byte, rest io.ReadCloser) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{Reader: io.MultiReader(bytes.NewReader(peek), rest), Closer: rest}
}

func (c *Client) newRequest(messages []ai.Message, options []ai.Option) (*ai.Request, error) {
	request := &ai.Request{
		Messages: messages,
		Model:    c.config.Model,
	}
	// NO SAMPLING PARAMETER IS SET, here or anywhere else: an absent
	// temperature is omitted upstream and the provider applies its own
	// default, which is the behavior every call through this adapter wants —
	// nothing here chooses a temperature for somebody else's model.
	if c.config.MaxTokens > 0 {
		maxTokens := c.config.MaxTokens
		request.MaxTokens = &maxTokens
	}
	for _, option := range options {
		if err := option(request); err != nil {
			return nil, fmt.Errorf("apply option: %w", err)
		}
	}
	return request, nil
}

// CompleteWithMessages performs one completion.
//
// ── THE GUARD IS ARMED BY THE CALL, NEVER BY AN AUDIENCE ────────────────────
//
// Every completion is asked for as a STREAM, whether or not anybody attached an
// observer to watch it. The observer decides who is TOLD what arrives; it has
// never had anything to do with whether the call is watched, and until 2026-08
// it silently decided exactly that.
//
// What that cost, measured: a headless `aforge do` leaf attaches no observer, so
// every one of its calls took the request/response path, whose only bound is
// [adaptiveCompletionTimeout] — a total deadline that caps at fifteen minutes. A
// DeepSeek endpoint accepted a request and never answered; the leaf sat on it
// for 15m24s, a twenty-minute claim reaper then took the node away, and the run
// spent ninety minutes and $0.63 producing nothing. Every detector that would
// have caught it in ninety seconds already existed — streamguard.go's first
// delta bound, its mid-stream gap, its wall derived from the lane's own measured
// history — and every one of them was dormant because a stream nobody was
// reading was not a stream at all.
//
// A fail-safe that arms only when a person is looking is decoration
// (docs/design/failsafe/FAILSAFE.md). So the shape of the request is decided
// here, by what the adapter needs in order to see, and the observer is optional
// throughout the streamed path.
//
// The accumulated *ai.Response is byte-identical either way, which is what makes
// this a change of transport and not of contract. The one endpoint that cannot
// be served this way says so on the wire — a gateway that takes `stream: true`
// and answers one whole JSON completion — and [Client.unstreamable] remembers it
// from what actually happened, so the fallback is a memo rather than a guess.
func (c *Client) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	observer := streamObserverFrom(ctx)
	response, relearned, err := c.completeWithMessagesStreaming(ctx, observer, messages, options...)
	if err != nil {
		return nil, err
	}
	// AN EMPTY ANSWER AT THE CEILING IS A FACT, NOT A RESULT. The 400 path
	// elsewhere learns a model that refuses the disable; this is the other way
	// the same thing shows — the endpoint accepted the disable, thought anyway,
	// and the whole ceiling went to the pass. Learned once, the next encode
	// leaves room (wire.go's thinkingCeiling), so the call is made again with the
	// answer it was always going to need. Once, because a second empty answer
	// WITH the room is a model that has nothing to say, and that is the caller's
	// to hear.
	//
	// The reading is done inside the call so that the row it writes can SAY what
	// the answer taught; the decision to do it twice is made here.
	if relearned {
		again, _, againErr := c.completeWithMessagesStreaming(ctx, observer, messages, options...)
		return again, againErr
	}
	return response, nil
}

// learnFromAnswer reads one answer for the fact the adapter can act on and
// names it, so an empty list is "the next encode will be the same request" and
// a non-empty one is both the decision to ask again AND the line the
// model-call log writes about why. It stays silent for a model the memo
// already knows — the room was already there, so a blank answer says nothing
// new — and for a call that set no ceiling to spend.
func (c *Client) learnFromAnswer(model string, request *ai.Request, response *ai.Response) []string {
	if request.MaxTokens == nil || c.reasoningUnstoppable(model) {
		return nil
	}
	// THE MEMO IS FOREVER, SO IT IS WRITTEN ONLY ON EVIDENCE. An answer with
	// no usage block cannot be shown to have spent the ceiling, and a reply
	// that was cut while calling a tool has an answer — the call — that
	// merely has no text. Neither says the thinking pass ate the reply, and
	// a fact recorded on a guess would grow every ceiling this model ever
	// gets, on every run, with nothing to unlearn it.
	if response == nil || response.Usage == nil || answeredWithToolCalls(response) {
		return nil
	}
	if !EmptyAtCeiling(response, *request.MaxTokens) {
		return nil
	}
	NoteReasoningDisableIgnored(model)
	return []string{learnedReasoningDisableIgnored}
}

// answeredWithToolCalls reports a reply whose answer is a tool call rather
// than words, which is an answer all the same.
func answeredWithToolCalls(response *ai.Response) bool {
	return response != nil && len(response.Choices) > 0 && len(response.Choices[0].Message.ToolCalls) > 0
}

// completionInOnePiece parses a whole-completion body and folds it into every
// record and measurement one answer feeds.
//
// It is shared by the two paths that can meet one, and it is shared rather than
// copied because the divergence is what this fix is about: the request/response
// path always meets one, and the streamed path meets one when the endpoint
// ignored `stream: true` and answered anyway. Two copies of "read the answer,
// name who served it, rate it, learn from it, write the row" is two places for
// the next fact about an answer to be added to only one of.
//
// stream says which shape the request went out in, so the model-call row says
// what actually happened rather than what the parse looked like.
func (c *Client) completionInOnePiece(
	ctx context.Context,
	request *ai.Request,
	knobs callKnobs,
	payload []byte,
	status int,
	began time.Time,
	logBegan time.Time,
	stream bool,
) (*ai.Response, bool, error) {
	// ONE PARSE. The answer and the router's annotation on it come out of the
	// same decode, because the alternative was reading a megabyte of completion
	// twice to recover one short string from the second pass.
	var decoded servedResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, false, fmt.Errorf("unmarshal response: %w", err)
	}
	response := decoded.Response
	// The usage block was decoded through the shadow so the reasoning count
	// came with it; it goes straight back on the response, where every reader
	// below and above this adapter expects to find it.
	//
	// A whole body carries one usage block and so has nothing to accumulate
	// onto, but it is folded in through the same helper the streamed loop reads
	// its frames with ([usageWire.mergeInto]) rather than assigned here: two
	// readers of one wire shape is how one of them ends up with a law the other
	// has never heard of.
	var reasoningTokens int
	response.Usage, reasoningTokens = decoded.Usage.mergeInto(response.Usage, 0)
	// AND THE SAME SPLIT THE STREAM TAKES, taken over the whole body (answer.go).
	// A gateway that fences its working in `<think>` does it whether or not the
	// request asked for a stream, and an answer that carried the model's private
	// working into the transcript on one transport and not the other would be
	// two accounts of one law.
	c.splitOnePiece(ctx, &response, payload)
	// An answer delivered whole has no first token to wait for — the whole thing
	// arrives at once — so it is rated and never judged on TTFT, and it has no
	// mid-stream gaps to judge either. Passing zero says "unmeasured" rather
	// than "instant" (velocity.go).
	// ONE NAME, read off the decode above rather than from a second pass over
	// the payload, and handed to both readers of it: the affinity that keeps a
	// conversation on the endpoint holding its prompt cache, and the rating.
	served := servedProvider(decoded.Provider)
	// A completion teaches the lane its longest reply just as a stream does;
	// without this line a lane that only ever answered whole — every headless
	// worker's — never earned a wall at all.
	// AND THE BASE ITSELF IS READ, ONCE (#433). Whether it named the lane that
	// served this answer is what tells this build whether a routing preference
	// reaches its wire at all — the one reading there is, and prefcarry.go says
	// what it cannot tell apart.
	c.notePrefsFromAnswer(ctx, served)
	c.noteRun(c.modelFor(request), served, c.clock().Sub(began))
	noteServed(ctx, served, c.noteEndpointAffinity(ctx, c.modelFor(request), served, response.Usage))
	c.noteVelocity(
		c.modelFor(request),
		served,
		0,
		outputTokens(&response, ""),
		c.clock().Sub(began),
		0,
		response.Usage.CacheReadTokens(),
	)
	// Both epilogues or neither: the whole-body path reads the same fourth
	// failure plane the streamed one does, and for the same reason — every
	// headless worker answers whole, and a guard on one transport is a guard a
	// change of default silently removes.
	if cut := c.machineryCut(ctx, request, &response, served, began, responseText(&response)); cut != nil {
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: stream,
			began: logBegan, status: status, served: served, err: cut,
			responseBody: payload,
		})
		c.bill(ctx, c.modelFor(request), &response)
		return nil, false, cut
	}
	reasonWord, servedWell := answerOutcome(&response)
	// PAST EVERY GUARD, SO THIS LANE SERVED. It is the recovery half of the
	// quality axis and it is what keeps the axis from being a penalty box: a
	// lane demoted for one bad stretch walks back up on the answers it gets
	// right, without waiting for a clock (lanes.go's noteLaneOutcome).
	c.noteLaneOutcome(c.modelFor(request), served, reasonWord, servedWell)
	// What the answer itself taught, read before the row is written so the row
	// can carry it. The caller decides whether to ask again.
	relearned := c.learnFromAnswer(c.modelFor(request), request, &response)
	c.record(recordFacts{
		ctx: ctx, request: request, knobs: knobs, stream: stream,
		began: logBegan, status: status, served: served,
		response: &response, reasoningTokens: reasoningTokens,
		learned: relearned, responseBody: payload,
	})
	// The money, banked at the same instant the log row is written and for the
	// same reason: this is where the fact is known. See billing.go.
	c.bill(ctx, c.modelFor(request), &response)
	return &response, len(relearned) > 0, nil
}

// clock is the client's time source, defaulting to the wall clock so a Client
// assembled without one still measures.
func (c *Client) clock() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

// servedResponse is one completion plus the field the router adds beside it.
//
// The wrapper exists rather than the field being added to ai.Response: the SDK's
// response type is the OpenAI shape, `provider` is the router's own annotation
// on it, and something this adapter reads for its own bookkeeping does not
// belong in a type the whole harness passes around.
type servedResponse struct {
	ai.Response
	// Provider is raw for the reason errorBody's `code` is raw: ONE FIELD OF AN
	// UNEXPECTED TYPE MUST NOT FAIL THE DECODE OF THE WHOLE ANSWER. An endpoint
	// that spelled the name as anything but a string leaves a completion that
	// still parses and a sighting with nobody to attribute, which is what it was
	// when the name was read by a second pass of its own.
	Provider json.RawMessage `json:"provider"`
	// Usage shadows the SDK's own usage block so the one figure its type has no
	// field for is read from THE SAME DECODE as the answer; see [usageWire]. It
	// is folded straight back onto the response below, so nothing downstream
	// sees that it was ever shadowed.
	Usage *usageWire `json:"usage"`
}

// usageWire is the provider's usage block: everything the SDK's own type holds,
// plus the reasoning-token count OpenRouter nests one level down under
// completion_tokens_details and the SDK has no field for.
//
// It shadows rather than re-reads for the reason servedResponse.Provider is
// decoded in one pass: recovering one number by unmarshalling a megabyte of
// completion a second time is the cost this whole shape exists to avoid. The
// embedded value promotes every field the SDK declares, so decoding a usage
// object into this type populates both halves at once.
//
// The reasoning count is the figure that turns "the answer came back empty"
// into "the thinking pass spent the whole ceiling", which is the bug that made
// the model-call log worth building.
type usageWire struct {
	ai.Usage
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// usage is the SDK-shaped half, addressable, and nil when the provider sent no
// usage block at all — which is "unknown" and never "zero".
func (u *usageWire) usage() *ai.Usage {
	if u == nil {
		return nil
	}
	block := u.Usage
	return &block
}

// reasoningTokens is what the thinking pass cost, or zero when the provider did
// not break its output down.
func (u *usageWire) reasoningTokens() int {
	if u == nil {
		return 0
	}
	return u.CompletionTokensDetails.ReasoningTokens
}

// mergeInto folds one usage frame into what the call has counted so far, and
// returns the accumulated block together with the reasoning count that survives
// the frame. It is how a streamed chat and a whole-body one alike fold a usage
// block in, so a second reader cannot drift from the first.
//
// USAGE ACCUMULATES ACROSS THE FRAMES OF ONE CALL: A LATER FRAME NEVER ZEROES A
// COUNT AN EARLIER FRAME CARRIED. Every provider aforge drives today sends its
// token counts and its price together in one terminal frame, so replacing the
// block wholesale looked right for as long as that held — but that is a property
// of today's endpoints and not of the protocol. An endpoint that reports the
// counts when the answer ends and the price a frame later would have left the
// ledger billing a call whose prompt and completion tokens were both zero, and
// taken the thinking pass's cost down with it.
func (u *usageWire) mergeInto(into *ai.Usage, reasoning int) (*ai.Usage, int) {
	merged := mergeUsage(into, u.usage())
	// The reasoning count rides alongside rather than inside ai.Usage, which has
	// no field for it, and it obeys the same law: a frame that does not break the
	// output down does not erase a breakdown an earlier frame gave.
	if count := u.reasoningTokens(); count != 0 {
		reasoning = count
	}
	return merged, reasoning
}

// mergeUsage folds one usage frame into the block a call has accumulated, under
// the law stated on [usageWire.mergeInto]. It is written over the SDK's own type
// rather than over the wire shadow so that every reader of a streamed usage
// block — chat here, music in music.go — folds by the one rule.
//
// A field is taken from the frame when the frame states it and kept otherwise,
// rather than summed: each frame carries the running TOTALS for the call, so
// adding them would double-count an endpoint that reports twice.
func mergeUsage(into, frame *ai.Usage) *ai.Usage {
	if frame == nil {
		// No usage block on this frame at all, which leaves the call exactly as
		// it was — including a nil block, because a call that never saw a usage
		// frame ends "unknown" and never "zero" (the emptiness law).
		return into
	}
	merged := ai.Usage{}
	if into != nil {
		merged = *into
	}
	takeCount(&merged.PromptTokens, frame.PromptTokens)
	takeCount(&merged.CompletionTokens, frame.CompletionTokens)
	takeCount(&merged.TotalTokens, frame.TotalTokens)
	takeCount(&merged.CacheReadInputTokens, frame.CacheReadInputTokens)
	takeCount(&merged.CacheCreationInputTokens, frame.CacheCreationInputTokens)
	// The OpenAI-style nesting is copied rather than aliased: the frame it came
	// out of is one decode of one event and does not outlive the loop reading it,
	// while the block being built here is handed to the ledger and the journal.
	if nested := frame.PromptTokensDetails; nested != nil && nested.CachedTokens != 0 {
		cached := *nested
		merged.PromptTokensDetails = &cached
	}
	// Cost is a pointer because nil means "unknown" rather than "free", so what
	// counts as stated here is a pointer that is set. A frame is still allowed to
	// say a call was free — but only when nothing before it reported a real
	// price, which is the same law the counts follow.
	if frame.Cost != nil && (*frame.Cost != 0 || merged.Cost == nil) {
		cost := *frame.Cost
		merged.Cost = &cost
	}
	return &merged
}

// takeCount folds one count of a usage frame into the call's own, under the law
// stated on [usageWire.mergeInto]: a frame that states the figure wins, and a
// frame that is silent about it leaves what was already there.
func takeCount(into *int, frame int) {
	if frame != 0 {
		*into = frame
	}
}

// servedProvider reads the endpoint the router says answered. An absent field is
// not an error — every non-router endpoint sends none — it is simply nobody to
// attribute the measurement to.
func servedProvider(raw json.RawMessage) string {
	var served string
	if err := json.Unmarshal(raw, &served); err != nil {
		return ""
	}
	return strings.TrimSpace(served)
}

// outputTokens is what an answer was worth, by the provider's own count when it
// sent one and by the four-bytes-a-token approximation when it did not.
//
// The approximation is deliberately crude and only ever feeds the rate: it
// decides whether an endpoint is above or below a threshold three times lower
// than any healthy endpoint's real rate, and it is never billed, never shown as
// a token count, and never folded into usage.
func outputTokens(response *ai.Response, text string) int {
	if response != nil && response.Usage != nil && response.Usage.CompletionTokens > 0 {
		return response.Usage.CompletionTokens
	}
	if text == "" && response != nil {
		for _, choice := range response.Choices {
			for _, part := range choice.Message.Content {
				text += part.Text
			}
		}
	}
	return tokensIn(text)
}

// stampCut writes onto a cut the three facts only the read loop holds: who the
// stream said was serving it, how long it had been open, and how much answer had
// arrived. See [StreamCut.Provider] for who reads them.
//
// The token figure is the estimate, because a cut stream never delivered a usage
// block — the provider counts at the end and there was no end. It is the same
// estimator a finished stream falls back to (outputTokens), so a row that says
// "eleven hundred tokens in eighteen minutes" is comparable with the call rows
// beside it.
func (c *Client) stampCut(cut *StreamCut, served string, began time.Time, text string) {
	if cut == nil {
		return
	}
	cut.Provider = strings.TrimSpace(served)
	cut.Ran = c.clock().Sub(began)
	cut.Tokens = outputTokens(nil, text)
}

// machineryCut reads a COMPLETE answer for the fourth failure plane — the
// reply that is the model's own tool grammar written as text ([MachineryLeak])
// — and returns the cut to fail the call with, or nil for a clean answer.
//
// It runs at the epilogue rather than inside the read loop because the shape
// can only be judged whole: a healthy tool-calling reply and a leak can share
// their first five hundred bytes. And it does everything the mid-stream cuts
// do, for the same reasons written at their site: the lane is struck so the
// ladder's next ask lands somewhere else, and the endpoint pin is released
// because an endpoint serving unparsed grammar is failing this lineage.
func (c *Client) machineryCut(ctx context.Context, request *ai.Request, response *ai.Response, served string, began time.Time, text string) *StreamCut {
	// It answers to the same switch the degeneration guard does, because it is
	// the same kind of judgment — a reading of the reply's shape — and `reply
	// guard off` promises the person sees whatever arrives.
	if !babbleGuardOn(ctx) || !MachineryLeak(request, response) {
		return nil
	}
	cut := &StreamCut{Reason: CutMachinery}
	c.stampCut(cut, served, began, text)
	cut.Rerouted = c.noteCutProvider(c.modelFor(request), served)
	// AND THE BELIEF LEARNS THAT THIS LANE SERVED SOMETHING UNUSABLE, which is
	// the claim the strike above cannot make: a strike expires in five minutes
	// and says only "not now", while an endpoint serving its model's unparsed
	// chat template is one whose ANSWERS are wrong, and that is the quality
	// axis (lanes.go's noteLaneOutcome).
	c.noteLaneOutcome(c.modelFor(request), served, cut.Reason.word(), false)
	c.releaseEndpoint(ctx, c.modelFor(request))
	return cut
}

// rescuedStreamCut reads a COMPLETE rescue for the F20 failure plane — a
// stream that closed after a hedge or walk and is not language — and
// returns the cut to fail the arm with, or nil for a clean answer.
//
// It answers only on a rescue (`hedgeLane` is set). The primary of a
// hedged race is judged in [hedgeRace.refuseCorrupt], because that is
// the moment the race would otherwise name a winner.
func (c *Client) rescuedStreamCut(ctx context.Context, request *ai.Request, response *ai.Response, served string, began time.Time, text string) *StreamCut {
	if hedgeLaneFrom(ctx) == "" {
		return nil
	}
	err := rescuedStreamError(response)
	if err == nil {
		return nil
	}
	cut, ok := CutFrom(err)
	if !ok {
		cut = &StreamCut{Reason: CutBabble}
	}
	c.stampCut(cut, served, began, text)
	cut.Rerouted = c.noteCutProvider(c.modelFor(request), served)
	c.noteLaneOutcome(c.modelFor(request), served, cut.Reason.word(), false)
	c.releaseEndpoint(ctx, c.modelFor(request))
	return cut
}

// completeWithMessagesStreaming performs one completion over a GUARDED stream:
// the silence bounds, the wall and the degeneration guard in streamguard.go all
// ride on it. The accumulated response is the same shape callers already parse
// after the stream closes.
//
// The observer may be nil, and on every headless run it is. Whether a person is
// watching decides who is TOLD what arrives and nothing else — see the law on
// [Client.CompleteWithMessages] — so it is filled in with a no-op here rather
// than guarded at forty call sites inside the read loop.
//
// The second return value is the relearn flag [Client.learnFromAnswer] produces:
// this answer taught the adapter that the model ignores the reasoning disable,
// so the caller should ask once more with the room left for it.
func (c *Client) completeWithMessagesStreaming(
	ctx context.Context,
	observer StreamObserver,
	messages []ai.Message,
	options ...ai.Option,
) (*ai.Response, bool, error) {
	if observer == nil {
		observer = func(StreamEvent) {}
	}
	relearned := false
	request, err := c.newRequest(messages, append(append([]ai.Option(nil), options...), ai.WithStream()))
	if err != nil {
		return nil, false, err
	}
	request.Stream = true
	// THE LANE CHOICE IS MADE ONCE, HERE, AND EVERYTHING DOWNSTREAM READS IT.
	//
	// It is a sampled decision (`internal/lane`'s Thompson draw), so asking for
	// it twice gives two different answers — and this call would have asked
	// twice: once for the watch that decides whether to hedge and where to, and
	// once inside the encoder for the `provider.order` that actually goes on the
	// wire. A watch waiting on a lane the wire never asked for is a hedge fired
	// at the wrong moment toward the wrong alternative, and nothing in either
	// half would look wrong on its own. Made here, the two are the same choice
	// by construction — and so is every rung of the endpoint ladder and every
	// retry, which each re-encode the same request.
	ctx = c.withLaneChoice(ctx, request)
	// AND THE PHASE CLOCK, ONCE, FOR THE WHOLE REQUEST (phase.go). It is
	// created here rather than below the race because a raced request is still
	// ONE request: two arms making two clocks would have the surface told
	// "writing" by the arm nobody is hearing while the other was still waiting
	// for its first word. An arm inherits the clock from the context and the
	// outermost call is the only one that ends it.
	phase := phaseClockFrom(ctx)
	if phase == nil {
		phase = c.newPhaseClock(ctx, c.modelFor(request))
		ctx = withPhaseClock(ctx, phase)
		defer phase.done()
	}
	// ONLY THE ARM THE PERSON IS HEARING NARRATES. An arm of a race runs this
	// same function on a child context and inherits the same clock; if it told
	// its own story the surface would be shown "connecting" by the rescue while
	// the answer it is drawing was already being written (hedge.go's one-voice
	// rule, said here about the clock instead of about the deltas).
	if streamWatchFrom(ctx).speaking() {
		phase.enter(PhaseConnecting, "")
	}
	// THE CONTROLLER, ON EVERY CALL, AND THE ONE PLACE IT IS BUILT (hedge.go).
	//
	// Every token-generating call that goes through this function is watched by
	// one controller — with or without a routing choice, with or without an
	// alternative, whatever the role. The race with one arm is still a race: it
	// observes, it reports, and it can act; what a cold ledger costs is an act
	// of [control.Report] rather than one of [control.Hedge], and never the
	// absence of a clock. The old gate asked the router for an opinion first,
	// so the case that most needed a deadline — a model nobody has measured —
	// was the one case that got none, and a turn waited three minutes.
	//
	// A build with no controller installed falls straight through to the loop
	// below, byte for byte as it was: that is the legal empty state
	// `internal/lane`'s seam documents, and each arm of a race reaches this
	// same line on a child context and passes it for the same reason.
	if race, watched := c.raceFor(ctx, observer, lanes.Controller()); watched {
		return race.run(ctx, messages, options...)
	}
	began := c.clock()
	// The log's own start, on the world's clock rather than the measurement
	// seam (calllog.go's logNow).
	logBegan := logNow()
	// THE GUARD'S OWN CANCEL, above send's. A stream that has to be cut — the
	// endpoint gone quiet, the reply gone to soup — is cut by cancelling the
	// request, because the reader is parked inside Read on a socket and only the
	// transport can unblock it (the same reasoning transport.go's idle watchdog
	// gives). It is deferred so every path releases the request goroutine,
	// including the ordinary clean end.
	guardCtx, cutStream := context.WithCancel(ctx)
	defer cutStream()
	// Resolved once and held, because every row this stream writes is built
	// from them and the trace inside them is what counts its attempts
	// (calllog.go).
	knobs := knobsFrom(ctx)
	// The lane watch this stream reports to, nil on every call that is not an
	// arm of a race (hedge.go). Every use of it below is a nil-safe method
	// call, so an unwatched stream pays one nil check per delta.
	watch := streamWatchFrom(ctx)
	httpResponse, err := c.sendShaped(guardCtx, request, knobs, true)
	if err != nil {
		return nil, false, err
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(httpResponse.Body, maxErrorPeek))
		refusal := apiError(httpResponse.StatusCode, payload)
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: true,
			began: logBegan, status: httpResponse.StatusCode,
			err: refusal, responseBody: payload,
		})
		c.refuseUpstream(request, knobs, refusal)
		return nil, false, refusal
	}
	// AN ENDPOINT THAT ANSWERED IN ONE PIECE IS NOT A STREAM, AND SAYS SO IN ITS
	// CONTENT TYPE. A gateway behind AFORGE_BASE_URL may take `stream: true` and
	// serve a whole completion anyway; feeding that body to the SSE decoder finds
	// no `data:` frames and would hand the caller an empty answer for a call it
	// paid for.
	//
	// THE ANSWER IS KEPT. It is a real completion, already generated and already
	// billed, and asking again to get it in a shape we prefer would pay for it
	// twice. So it is parsed here through the same door the request/response path
	// uses, and the ENDPOINT is remembered instead: from the next call on, this
	// client stops asking a gateway for a dialect it has demonstrated it does not
	// speak, and stops paying the streaming transport's header deadline to find
	// that out again.
	if !isEventStream(httpResponse.Header.Get("Content-Type")) {
		c.unstreamable.Store(true)
		payload, readErr := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes))
		if readErr != nil {
			// A body that breaks off after the headers is an attempt that
			// failed, and it leaves its end row like any other; without one the
			// log shows the call in flight forever.
			readErr = fmt.Errorf("read response: %w", readErr)
			c.record(recordFacts{
				ctx: ctx, request: request, knobs: knobs, stream: true,
				began: logBegan, status: httpResponse.StatusCode, err: readErr,
			})
			return nil, false, readErr
		}
		return c.completionInOnePiece(ctx, request, knobs, payload, httpResponse.StatusCode, began, logBegan, true)
	}
	// The silence watchdog starts the moment the headers land, which is the
	// moment the endpoint has accepted the request and owes an answer
	// (streamguard.go). Only the MODEL WRITING moves its clock; keepalives buy
	// bounded patience instead — the decoder reports them through the alive
	// seam below, and streamguard.go says exactly what they are worth.
	//
	// AND THE WALL STARTS WITH IT (streamguard.go's THE WALL). It opens at the
	// lineage's widest — no chunk has named a serving endpoint yet — and
	// narrows to the lane's own the moment one does, below.
	stall := newStallWatch(ctx, cutStream, c.streamWall(c.modelFor(request), ""))
	defer stall.stop()
	// THE MOMENT THE ENDPOINT OWES AN ANSWER is the moment this arm's wait
	// really began, and it is taken from the reading above rather than from a
	// clock read of this seam's own: the controller runs on the world's clock
	// (hedge.go's waitNow) and a measurement seam a test has scripted must not
	// be spent on bookkeeping.
	watch.opened(waitNow())
	// THE ENDPOINT HAS ACCEPTED THE REQUEST AND OWES AN ANSWER, which is a
	// different wait from the handshake before it and the only one a hedge
	// deadline belongs to. The consequence rides with it when there really is
	// one — a lane to go to and a moment to go at — and is absent otherwise,
	// because a countdown that expires and does nothing is the surface lying
	// about the machinery (phase.go).
	if watch.speaking() {
		deadline, alt := watch.consequence()
		if alt == "" || deadline.IsZero() {
			phase.firstWord(time.Time{}, "")
		} else {
			phase.firstWord(deadline, strings.ToLower(alt))
		}
	}
	// And the degeneration guard, unless this call has it switched off. It is
	// nil rather than dormant when off, so a call that is not watching pays
	// nothing per delta for the fact.
	//
	// There are TWO of it, one per channel. `babble` reads the answer, which
	// is what the person is watching; `thoughts` reads the WORKING — the
	// reasoning channel, and working that arrived fenced inside `content`
	// (answer.go) — because the commonest shape of this failure on the open
	// models lives there: a serving stack that loops one token inside the
	// thinking pass (sgl-project/sglang#36669 is the reproduced case) shows a
	// person nothing at all while it runs to the output ceiling and bills
	// every token of it. A loop nobody can see is still a loop.
	var babble, thoughts *babbleWatch
	if babbleGuardOn(ctx) {
		babble, thoughts = &babbleWatch{}, &babbleWatch{}
	}
	// THE ONLY PLACE TTFT IS REALLY OBSERVABLE. The two facts the ledger wants
	// are separated by the stream itself: how long the endpoint took to say
	// anything, and how fast it wrote once it had started. Timing them together
	// would price a warm endpoint behind a long prompt as a slow one.
	var served string
	var firstToken time.Time
	// What the thinking pass cost, off the same terminal usage frame the token
	// counts come from. Zero until one arrives, which is "the provider did not
	// break its output down" and never "it did not think".
	reasoningTokens := 0

	// Read once per call rather than once per event: the session does not
	// change mid-stream, and this loop already runs against the connection's
	// idle watchdog (see the note below on why the observer stays trivial).
	session := streamSessionFrom(ctx)
	observer(StreamEvent{Kind: StreamStarted, Session: session})
	finished := false
	defer func() {
		if !finished {
			observer(StreamEvent{Kind: StreamFailed, Session: session})
		}
	}()

	response := &ai.Response{Model: request.Model}
	var content strings.Builder
	var tools toolCallAccumulator
	// THE ONE DECISION ABOUT WHAT IS ANSWER AND WHAT IS WORKING, taken here so
	// that the observer the surface draws from and the `content` that becomes
	// the assistant message are two spendings of ONE reading (answer.go). It is
	// per response and not per client: the state it holds is about the reply
	// being read now.
	var split answerSplit
	finishReason := ""
	thinking := false
	// thoughtBegan is when this run of reasoning started, and it is held so that
	// how long the whole phase lasted can be FOLDED BACK when the first word of
	// answer ends it.
	//
	// A DURATION MODEL THAT IS NEVER MEASURED IS A PRIOR FOREVER. The controller
	// judges a run of thought against how long this model's thinking usually
	// lasts, and that belief is only worth having if the finished ones teach it:
	// without this, a model that deliberates for a minute by design and one that
	// has hung look alike for as long as the prior is wide.
	var thoughtBegan time.Time
	// The rung it was asked at, resolved once: how long a model deliberates is a
	// property of the model AND of the effort it was told to spend.
	effortRung := c.recordedEffort(c.modelFor(request), knobsFrom(ctx))
	// The decoder is ours rather than the SDK's, and sse.go says why: the SDK's
	// accumulation is quadratic in the length of a single message, which costs
	// about a gigabyte of copying to deliver one four-megabyte reasoning block.
	// It decodes the same framing to the same chunks — that equivalence is the
	// whole of its test — so the only difference here is the copying.
	//
	// The loop below is deliberately trivial: the observer is called
	// synchronously and in order, so it must not work. The one live observer
	// (chat's head stream) does nothing but translate the event and hand it to
	// a buffered channel with a ctx escape, which is the contract to keep —
	// anything heavier would be paid per token, in the read loop, against the
	// connection's idle watchdog.
	decoder := newSSEDecoder(httpResponse.Body)
	decoder.alive = stall.alive
	if watch != nil {
		// A ROUTER'S COMMENT LINE IS PROOF ABOUT THE PATH. It buys the stall
		// guard bounded patience (streamguard.go) and it tells the lane watch
		// that the connection is alive but the model has not started — which is
		// exactly the difference between a slow lane and a dead path.
		decoder.alive = func() {
			stall.alive()
			// A BEAT IS PROOF ABOUT THE PATH AND ABOUT NOTHING ELSE. It never
			// resets the silence clock: a clock it reset would be a clock a
			// router could hold open forever by saying nothing in a well-formed
			// way. It is reported AS a beat, in the same reading the words and
			// the thoughts arrive in, so the controller can tell a dead path
			// from a slow lane without a second door.
			watch.note(control.Reading{At: waitNow(), Beat: true})
		}
	}
	// lastWrite and widestGap watch the same deltas the stall guard does, for
	// the ledger rather than for a cut: an endpoint that finished its answer
	// but delivered it in lumps is working, slowly, and "working slowly" is
	// the lag law's department (velocity.go's LagGap).
	var lastWrite time.Time
	var widestGap time.Duration
	// soup is the one road out for a reply that stopped being language,
	// whichever channel it stopped on. The lane is struck, the quality belief
	// learns the plainest thing it can, the pin moves, and nothing past this
	// point becomes a response — so no soup is ever returned to the turn loop
	// and none of it reaches the transcript.
	soup := func() (*ai.Response, bool, error) {
		cut := &StreamCut{Reason: CutBabble}
		c.stampCut(cut, served, began, content.String())
		cut.Rerouted = c.noteCutProvider(c.modelFor(request), served)
		// Soup is the plainest possible statement that this lane's answers
		// cannot be used, so it is the plainest thing the quality belief can
		// learn (lanes.go's noteLaneOutcome).
		c.noteLaneOutcome(c.modelFor(request), served, cut.Reason.word(), false)
		// An endpoint producing soup has failed this lineage as surely as one
		// that went quiet, so the pin moves too.
		c.releaseEndpoint(ctx, c.modelFor(request))
		return nil, false, cut
	}
	for {
		chunk, decodeErr := decoder.DecodeChunk()
		if decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			// A WATCHDOG'S CUT IS NOT A TORN CONNECTION, and it outranks the
			// decode error it caused: cancelling the request is how the cut is
			// made, so the read always fails afterwards and the failure it
			// reports is a symptom. The person's own interrupt is checked
			// against the CALLER'S context and never against this one, so a
			// stop that lands while a watchdog is firing still reads as a stop.
			if cut := stall.cut(); cut != nil && ctx.Err() == nil {
				// HOW FAR IT GOT, ON THE CUT ITSELF. A journal row about a cut
				// that cannot say who was serving or how much answer had
				// arrived is the row that made this whole bound guesswork the
				// first time ([StreamCut.Provider]).
				c.stampCut(cut, served, began, content.String())
				// Whether the ledger took the lane away travels ON the cut: the
				// turn loop decides how many more times to ask this model from
				// it, and it has no other way to know ([StreamCut.Rerouted]).
				cut.Rerouted = c.noteCutProvider(c.modelFor(request), served)
				// AND THE BELIEF LEARNS IT TOO. A stream this process gave up on
				// produced no usable answer, whichever bound decided, and that is
				// a claim about the lane's ANSWERS rather than about its speed —
				// so it decays the quality belief and recovers by service, rather
				// than expiring on a five-minute clock (lanes.go).
				c.noteLaneOutcome(c.modelFor(request), served, cut.Reason.word(), false)
				// A stream that went quiet is an endpoint failing this lineage,
				// which is the one thing that moves a pin (affinity.go).
				c.releaseEndpoint(ctx, c.modelFor(request))
				// AND IT IS HOW THE CALL ENDED, so it ends the call's row too.
				// A cut is the single outcome a log most needs to carry: the
				// endpoint accepted the request, answered 200, and then said
				// nothing for long enough to be given up on.
				c.record(recordFacts{
					ctx: ctx, request: request, knobs: knobs, stream: true,
					began: logBegan, status: httpResponse.StatusCode, served: served, err: cut,
				})
				return nil, false, cut
			}
			// A stream that broke off for any reason but a cut — the caller
			// gave up on it, or the connection went — still ends its row;
			// without one the log shows the call in flight forever.
			decodeErr = fmt.Errorf("decode stream: %w", decodeErr)
			c.record(recordFacts{
				ctx: ctx, request: request, knobs: knobs, stream: true,
				began: logBegan, status: httpResponse.StatusCode, served: served, err: decodeErr,
			})
			return nil, false, decodeErr
		}
		if response.ID == "" {
			response.ID = chunk.ID
			response.Object = chunk.Object
			response.Created = chunk.Created
		}
		if chunk.Model != "" {
			response.Model = chunk.Model
		}
		if chunk.Provider != "" {
			watch.serve(chunk.Provider)
			if watch.speaking() {
				phase.serve(chunk.Provider)
			}
			if served == "" {
				// The first naming is what narrows the wall onto the lane that
				// is actually serving; [stallWatch.rewall] does it once and
				// measures the new bound from when the stream opened.
				stall.rewall(c.streamWall(c.modelFor(request), chunk.Provider))
				// And it narrows the SILENCE bound the same way, onto what this
				// lane's measured rate says a gap between two tokens should be
				// ([stallWatch.regap]). The two travel together because they
				// answer the same question about the same lane at the same
				// moment: one about how long a reply may go on, one about how
				// long it may stop.
				stall.regap(c.streamGap(c.modelFor(request), chunk.Provider))
			}
			served = chunk.Provider
		}
		// A REFUSAL DELIVERED INSIDE A 200 IS STILL A REFUSAL. The router accepted
		// the request, sent its headers, and then said the upstream broke; every
		// other layer here would read the stream that follows as a short answer.
		// It ends the call as an error, releases the pin and takes the lane away,
		// exactly as the same refusal arriving before the headers would (sse.go
		// says what it cost when this field was not read at all).
		if refusal := streamRefusal(chunk.Error); refusal != nil {
			if named, ok := RefusalFrom(refusal); ok && named.Provider == "" && served != "" {
				// The stream named who was serving it even when the error object
				// did not, and that name is what the ledger and the journal need.
				named.Provider = served
			}
			c.record(recordFacts{
				ctx: ctx, request: request, knobs: knobs, stream: true,
				began: logBegan, status: httpResponse.StatusCode, served: served, err: refusal,
			})
			c.refuseUpstream(request, knobs, refusal)
			c.releaseEndpoint(ctx, c.modelFor(request))
			return nil, false, refusal
		}
		if chunk.Usage != nil {
			// FOLDED IN, NEVER SWAPPED IN: a frame carrying only the price does
			// not zero the counts the frame before it carried (see
			// [usageWire.mergeInto], which both transports read a usage frame
			// through).
			response.Usage, reasoningTokens = chunk.Usage.mergeInto(response.Usage, reasoningTokens)
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			// THE SPLIT IS TAKEN FIRST AND ONCE (answer.go), because every line
			// below wants its answer rather than the channel the bytes came in
			// on: what the person is shown, what the response accumulates, what
			// the babble guard reads, and what the phase clock calls this moment.
			answerText, workingText := split.content(choice.Delta.Content)
			// Reasoning counts as the first token. It is the endpoint writing —
			// billed, streamed, and the thing the person is waiting through —
			// and a reasoning model that thinks for a minute before its first
			// word of answer is not an endpoint that took a minute to respond.
			if firstToken.IsZero() && (choice.Delta.Content != "" || choice.Delta.thinking()) {
				firstToken = c.clock()
			}
			// THE MODEL WRITING IS THE ONLY THING THAT COUNTS AS PROGRESS. A
			// token of answer, a token of thought, a fragment of a call — the
			// three things an endpoint that is working produces, and nothing
			// else on this wire.
			if choice.Delta.Content != "" || choice.Delta.thinking() || len(choice.Delta.ToolCalls) > 0 {
				now := c.clock()
				if !lastWrite.IsZero() && now.Sub(lastWrite) > widestGap {
					widestGap = now.Sub(lastWrite)
				}
				lastWrite = now
				stall.progress()
				// AND THE SAME PROGRESS IS ONE READING FOR THE CONTROLLER.
				//
				// THE TWO COUNTS ARE NOT INTERCHANGEABLE. A token of answer is
				// text on the screen and is the only thing that resets the
				// deadline; a token of thought — or a fragment of a call being
				// assembled — is billed, streamed work that shows nothing, so
				// it keeps the stream alive and moves the phase without
				// counting as progress a person could watch disappear. A
				// reasoning delta reported as a first token is what let a stall
				// sixty seconds into a run of thought wait on a transport bound
				// two and a half minutes away.
				//
				// AND WHAT COUNTS AS ANSWER IS THE SPLIT'S ANSWER, not the
				// channel the bytes arrived on (answer.go). A model whose
				// working comes fenced inside `content` is THINKING, so its
				// deltas are HIDDEN to the controller: reading them as visible
				// would reset the very clock that is supposed to be running
				// through a run of thought. The split decides answer from
				// working; this decides waiting.
				visible, hidden := 0, 1
				if answerText != "" {
					visible, hidden = 1, 0
				}
				watch.note(control.Reading{At: waitNow(), Visible: visible, Hidden: hidden})
				// AND THE SAME PROGRESS MOVES THE PHASE CLOCK, which is the
				// only thing on the wire that can tell a person the difference
				// between a model thinking and a model writing. Only the arm
				// the person is HEARING may move it: the other one's deltas are
				// held (hedge.go's one-voice rule) and a story told from them
				// would be about text nobody is reading.
				//
				// THE CLOCK IS TOLD WHAT THE SPLIT DECIDED and not what channel
				// the bytes arrived on (answer.go): a model whose working comes
				// fenced inside `content` is THINKING, and a clock that read the
				// channel would tell the person it was writing their reply.
				if watch.speaking() {
					if answerText != "" {
						phase.enter(PhaseWriting, "")
					} else if workingText != "" || choice.Delta.thinking() {
						phase.enter(PhaseThinking, "")
					}
					phase.wrote()
				}
			}
			if answerText != "" {
				if thinking {
					// THE FIRST WORD OF ANSWER IS WHAT ENDS A THOUGHT, and it is
					// the only thing that does: a phase that ends with the stream
					// is a phase that never finished, and a run of thought that
					// was cut off is not evidence about how long thinking takes.
					//
					// IT IS THE SPLIT'S FIRST WORD OF ANSWER (answer.go), not the
					// first byte on the content channel: a model that fences its
					// working inside `content` is still thinking, and closing the
					// phase on that byte would teach the duration clock that this
					// model deliberates for no time at all.
					lanes.NoteThought(c.modelFor(request), effortRung, c.clock().Sub(thoughtBegan), c.clock())
				}
				thinking = false
				content.WriteString(answerText)
				observer(StreamEvent{Kind: StreamDelta, Delta: answerText, Session: session})
				// AND THE JUNK STOPS HERE. The delta has already been handed to
				// the observer — a person watches text arrive and the surface
				// throws away what a cut turn streamed — but nothing past this
				// point becomes a response, so no soup is ever returned to the
				// turn loop and none of it reaches the transcript.
				if babble != nil && babble.write(answerText) {
					return soup()
				}
			}
			// AND THE WORKING IS READ THE SAME WAY, whichever channel carried
			// it. The split has already handed the person what there was to
			// hand (nothing — working is never drawn as answer); this only
			// decides whether the stream goes on.
			if workingText != "" && thoughts != nil && thoughts.write(workingText) {
				return soup()
			}
			// The run of reasoning is announced ONCE — that boundary is what a
			// surface drawing "thinking…" needs — and the text of it follows per
			// delta as StreamReasoning, for a surface that shows the thought.
			// It is not accumulated into answer Content. Its wire field and details
			// ride the event so the session can replay it as assistant metadata.
			if choice.Delta.thinking() {
				if !thinking {
					thinking, thoughtBegan = true, c.clock()
					observer(StreamEvent{Kind: StreamThinking, Session: session})
				}
				events, count := choice.Delta.reasoningEvents()
				for _, event := range events[:count] {
					event.Session = session
					split.reasoning(event.Delta)
					observer(event)
					if thoughts != nil && event.Delta != "" && thoughts.write(event.Delta) {
						return soup()
					}
				}
			}
			// AND THE WORKING THE SPLIT CARVED OUT OF THE ANSWER CHANNEL rides
			// the same events, so a surface has exactly one notion of what
			// thinking looks like whether the endpoint fenced it or fielded it.
			//
			// IT CARRIES NO WIRE FIELD, AND [StreamEvent.FromAnswer] SAYS WHY:
			// this working never travelled on a reasoning field, so there is no
			// field to replay it under, and a continuation that invented one
			// would be handing the endpoint back a message it never sent.
			if workingText != "" {
				if !thinking {
					// AND IT OPENS THE DURATION CLOCK'S PHASE exactly as a
					// reasoning field does. How long a model deliberates is a
					// property of the model, not of which channel its provider
					// put the deliberation on, so a fenced thought is timed and
					// folded back like any other (answer.go, and the close above).
					thinking, thoughtBegan = true, c.clock()
					observer(StreamEvent{Kind: StreamThinking, Session: session})
				}
				observer(StreamEvent{Kind: StreamReasoning, Delta: workingText, FromAnswer: true, Session: session})
			}
			for _, fragment := range choice.Delta.ToolCalls {
				// A response that asked for a call is a response that behaved,
				// however little it said in words, so the split must never read
				// its silence as a lost answer (answer.go's [answerSplit.promote]).
				split.sawTools()
				// One call finishing is worth saying before the whole message
				// does, so a consumer can start on it. The marshal is per
				// completed call rather than per token, which is the budget this
				// loop has for work.
				if ready, complete := tools.add(fragment); complete {
					observeToolCallReady(observer, session, ready)
				}
				// And the call this fragment GREW, after any call it closed:
				// the two events are about different calls, and a forming
				// event for the successor must not land before its
				// predecessor was announced ready.
				observeToolCallForming(observer, session, &tools)
			}
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
		}
	}
	// The last call has no successor to close it, so the clean end of the stream
	// does. This runs only past the decode loop's error returns: a stream that
	// died mid-call announces nothing, because the fragment it stopped on may be
	// half an instruction.
	for _, ready := range tools.flush() {
		observeToolCallReady(observer, session, ready)
	}
	// AND THE SPLIT'S OWN LAST TWO ACTS, in this order and only here — the
	// stream is over, so both questions now have answers they will not have to
	// take back (answer.go).
	//
	// First whatever was withheld against a fence that never completed: a `<th`
	// at the end of the last delta is text the model wrote, and the person gets
	// it rather than losing it to a tag that was never coming.
	if heldAnswer, heldWorking := split.flush(); heldAnswer != "" || heldWorking != "" {
		if heldAnswer != "" {
			content.WriteString(heldAnswer)
			observer(StreamEvent{Kind: StreamDelta, Delta: heldAnswer, Session: session})
		}
		if heldWorking != "" {
			observer(StreamEvent{Kind: StreamReasoning, Delta: heldWorking, FromAnswer: true, Session: session})
		}
	}
	// Then the promotion: a response that asked for nothing and said nothing put
	// its reply on the working channel, and the reply is what the person asked
	// for. It goes out as a delta BEFORE the message is assembled, so the words
	// the surface draws and the words the transcript keeps are the same words
	// arriving by the same road — which is the property a replay depends on.
	if promoted, ok := split.promote(ProseAnswerAsked(ctx)); ok {
		content.WriteString(promoted)
		observer(StreamEvent{Kind: StreamDelta, Delta: promoted, Session: session})
	}
	message := ai.Message{
		Role:      "assistant",
		Content:   []ai.ContentPart{{Type: "text", Text: content.String()}},
		ToolCalls: tools.assembled(),
	}
	response.Choices = []ai.Choice{{Index: 0, Message: message, FinishReason: finishReason}}
	// The rate is measured over the GENERATION window — first token to last —
	// and not over the call, so the wait to be served is charged to TTFT once
	// rather than to both figures. A stream that never produced a token is
	// still a sighting: its TTFT is the whole call, which is exactly the
	// complaint a person has about it.
	generation := c.clock()
	// THE REPLY FINISHED, SO ITS LENGTH IS EVIDENCE. It is the whole request —
	// the wait to be served plus the writing — because that is what the wall
	// bounds, and it is recorded whatever the routing preference says
	// (velocity.go's [Client.noteRun]).
	// AND THE BASE ITSELF IS READ, ONCE (#433). Whether it named the lane that
	// served this answer is what tells this build whether a routing preference
	// reaches its wire at all — the one reading there is, and prefcarry.go says
	// what it cannot tell apart.
	c.notePrefsFromAnswer(ctx, served)
	c.noteRun(c.modelFor(request), served, generation.Sub(began))
	noteServed(ctx, served, c.noteEndpointAffinity(ctx, c.modelFor(request), served, response.Usage))
	if !firstToken.IsZero() {
		c.noteVelocity(
			c.modelFor(request),
			served,
			firstToken.Sub(began),
			outputTokens(response, content.String()),
			generation.Sub(firstToken),
			widestGap,
			response.Usage.CacheReadTokens(),
		)
	} else {
		c.noteVelocity(c.modelFor(request), served, generation.Sub(began), 0, 0, 0, 0)
	}
	// A reply that is the model's own tool grammar as text ends the call as a
	// cut even though every stream bound was met: the endpoint answered 200 and
	// served something no reader of it can use. The row carries the cut, and
	// the money is still banked — the provider counted these tokens whether or
	// not the answer was language. It sits on BOTH epilogues or on neither,
	// exactly as the learning below: the leak is a property of the endpoint,
	// not of the transport that carried it.
	if cut := c.machineryCut(ctx, request, response, served, began, content.String()); cut != nil {
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: true,
			began: logBegan, status: httpResponse.StatusCode, served: served, err: cut,
		})
		c.bill(ctx, c.modelFor(request), response)
		return nil, false, cut
	}
	// A RESCUE IS NOT THE TURN UNTIL IT READS AS LANGUAGE. The hedge used
	// to accept the first arm that closed, and F20 persisted the mojibake
	// that arrived after a 429. The check is here, before StreamFinished,
	// so a corrupt rescue leaves as a failed arm and never as a response.
	if cut := c.rescuedStreamCut(ctx, request, response, served, began, content.String()); cut != nil {
		c.record(recordFacts{
			ctx: ctx, request: request, knobs: knobs, stream: true,
			began: logBegan, status: httpResponse.StatusCode, served: served, err: cut,
		})
		c.bill(ctx, c.modelFor(request), response)
		return nil, false, cut
	}
	// PAST EVERY GUARD, SO THIS LANE SERVED — the recovery half of the quality
	// axis, on both epilogues for the reason the guard above sits on both: the
	// standing belongs to the endpoint and not to the transport that carried it.
	servedReason, servedWell := answerOutcome(response)
	c.noteLaneOutcome(c.modelFor(request), served, servedReason, servedWell)
	// What the answer itself taught, read before the row is written so the row
	// can carry it — the same reading [Client.completionInOnePiece] makes about the
	// same fact.
	// It belongs on BOTH paths or on neither: the ceiling that a thinking pass
	// eats is a property of the model, not of the transport that carried it, and
	// leaving it off the streamed path is how a fix landed at one seam gets
	// silently un-landed by a change of default at another.
	learned := c.learnFromAnswer(c.modelFor(request), request, response)
	relearned = len(learned) > 0
	c.record(recordFacts{
		ctx: ctx, request: request, knobs: knobs, stream: true,
		began: logBegan, status: httpResponse.StatusCode, served: served,
		response: response, reasoningTokens: reasoningTokens, learned: learned,
	})
	// Both paths or neither, exactly as the learning above: a streamed answer
	// is billed by the provider the same way a whole-body one is, and a ledger
	// blind to one of the two transports is a ledger nobody can reconcile.
	c.bill(ctx, c.modelFor(request), response)
	finished = true
	observer(StreamEvent{Kind: StreamFinished, Session: session})
	return response, relearned, nil
}

// isEventStream reports whether a response body is server-sent events, from the
// only thing that can say so before a byte of it is read.
//
// A STREAM MUST DECLARE ITSELF, and the ambiguous answer is "not a stream". The
// SSE specification requires `text/event-stream` and every router that speaks it
// sends it, so requiring it costs nothing real; what it buys is that a response
// which says nothing about its type is parsed as a whole completion, which is
// the shape it almost certainly is and the behaviour that existed before any of
// this. The alternative reading loses a real answer silently — the SSE decoder
// finds no frames in a JSON body and hands back an empty reply for a call that
// was paid for — and a silent empty answer is the worst failure in this file.
//
// Sniffing the body instead was considered and rejected: the first byte cannot
// be read until it arrives, and the read that waits for it would happen before
// the stall watch is armed, which is a gap in exactly the bound this whole
// change exists to close.
//
// Everything but the media type is read leniently: a `charset` parameter, odd
// spacing and any capitalisation are the same stream.
func isEventStream(contentType string) bool {
	media := strings.TrimSpace(strings.ToLower(contentType))
	if semicolon := strings.IndexByte(media, ';'); semicolon >= 0 {
		media = strings.TrimSpace(media[:semicolon])
	}
	return media == "text/event-stream"
}

// observeToolCallReady announces one whole tool call. The call rides as JSON
// because the consumer is code: a gloss would be a second, lossier vocabulary
// for something the wire already spells exactly once.
//
// A call that will not marshal is dropped rather than announced empty. Nothing
// is lost by that — the response's own ToolCalls() carries it a moment later,
// and this event promises only earliness, never delivery.
func observeToolCallReady(observer StreamObserver, session string, call ai.ToolCall) {
	payload, err := json.Marshal(call)
	if err != nil {
		return
	}
	observer(StreamEvent{Kind: StreamToolCallReady, Delta: string(payload), Session: session})
}

// observeToolCallForming says the call the last fragment grew is still growing.
//
// It is raised for EVERY fragment, including the ones that carry only an id or
// only a name, because "the model has started asking for something" is the first
// thing worth saying and it is exactly what those fragments mean. Nothing is
// marshaled and nothing is parsed: the accumulator already holds the text, and
// half-sent arguments are not JSON to parse anyway.
func observeToolCallForming(observer StreamObserver, session string, tools *toolCallAccumulator) {
	forming, open := tools.current()
	if !open {
		return
	}
	observer(StreamEvent{
		Kind:    StreamToolCallForming,
		Delta:   forming.Args,
		Session: session,
		Index:   forming.Index,
		ID:      forming.ID,
		Tool:    forming.Name,
	})
}

// StreamComplete performs one streaming completion over a single user prompt.
// It mirrors the SDK's channel contract exactly so the harness's stream pump is
// unchanged.
func (c *Client) StreamComplete(ctx context.Context, prompt string, options ...ai.Option) (<-chan ai.StreamChunk, <-chan error) {
	chunks := make(chan ai.StreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)
		// The consumer is ranging over two channels it did not spawn. A fault
		// here must reach it as an error, not as a dead process.
		defer func() {
			if recovered := recover(); recovered != nil {
				select {
				case errs <- guard.Note("provider/stream", recovered):
				default:
				}
			}
		}()

		messages := []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: prompt}}}}
		request, err := c.newRequest(messages, append(append([]ai.Option(nil), options...), ai.WithStream()))
		if err != nil {
			errs <- err
			return
		}
		request.Stream = true
		// Retrying happens entirely before the first byte of the stream is
		// handed over, so a reconnect can never duplicate delivered chunks.
		httpResponse, err := c.sendShaped(ctx, request, knobsFrom(ctx), true)
		if err != nil {
			errs <- err
			return
		}
		defer httpResponse.Body.Close()
		if httpResponse.StatusCode >= 400 {
			payload, _ := io.ReadAll(io.LimitReader(httpResponse.Body, maxErrorPeek))
			errs <- apiError(httpResponse.StatusCode, payload)
			return
		}

		decoder := newSSEDecoder(httpResponse.Body)
		for {
			chunk, err := decoder.Decode()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					errs <- fmt.Errorf("decode stream: %w", err)
				}
				return
			}
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case chunks <- chunk:
			}
		}
	}()

	return chunks, errs
}

func (c *Client) newHTTPRequest(ctx context.Context, request *ai.Request, body []byte, stream bool) (*http.Request, error) {
	endpoint := strings.TrimSuffix(strings.TrimSpace(c.config.BaseURL), "/") + "/chat/completions"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	apiKey, err := c.apiKeyNow()
	if override := strings.TrimSpace(request.APIKeyOverride); override != "" {
		apiKey, err = override, nil
	}
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	if stream {
		httpRequest.Header.Set("Accept", "text/event-stream")
	}
	// THE HINT AND NOT THE PREFERENCE ANSWER. These headers are read by one
	// machine's ranking page and by nothing else, so the question really is
	// "is this that machine" (prefcarry.go says why every other site moved).
	if c.shippedRouterHint() {
		ApplyAttribution(httpRequest.Header)
	}
	// The header half of cache affinity. Routers that ignore the body field
	// still honour a session header, and a router that honours neither is
	// unharmed by an extra one.
	if key := CacheKeyFrom(ctx); key != "" {
		httpRequest.Header.Set("X-Session-Affinity", key)
	}
	return httpRequest, nil
}

// shippedRouterHint reports whether this client is talking to THE SHIPPED
// ROUTER, recognised by its hostname or by a model spelled `openrouter/…`.
//
// IT IS A HINT ABOUT ONE MACHINE AND IT IS NOT A DECISION ABOUT ANY BEHAVIOUR.
// It was named `isOpenRouter` and it WAS the decision — whether a routing
// preference went on the wire, whether a lane was measured, whether a probe was
// bought — and every one of those is now [Client.carriesPreferences], which is
// what the base itself answered (prefcarry.go, issue #433). A capability
// decided by where a base lives rather than by what it answers is the defect
// class #373 and #433 both closed, and a reader who mistakes this for the
// decision would reopen it.
//
// WHAT IS LEFT TO IT IS THE TWO THINGS THAT REALLY ARE ABOUT THAT ONE MACHINE:
// the attribution headers OpenRouter's own ranking page reads
// ([Client.newHTTPRequest]), and the choice of this adapter's transport over
// the SDK's ([Client.ExecuteToolCallLoop]). Neither is a preference and neither
// is a claim about lanes.
func (c *Client) shippedRouterHint() bool {
	return strings.Contains(strings.ToLower(c.config.BaseURL), "openrouter.ai") ||
		strings.HasPrefix(normalizeModel(c.config.Model), "openrouter/")
}

// adaptiveCompletionTimeout accounts for reasoning and output tokens being
// generated serially. It scales at one second per 64 requested tokens, keeps
// the old five-minute timeout as its floor, and caps wedged calls at 15 minutes.
func adaptiveCompletionTimeout(maxTokens int, configuredFloor time.Duration) time.Duration {
	const (
		floor   = 5 * time.Minute
		ceiling = 15 * time.Minute
	)
	if configuredFloor < floor {
		configuredFloor = floor
	}
	scaled := time.Duration(maxTokens/64) * time.Second
	if scaled < configuredFloor {
		scaled = configuredFloor
	}
	if scaled > ceiling {
		return ceiling
	}
	return scaled
}

// APIError is one refusal from the model provider, with the two things about it
// that are facts rather than prose: the status it came back under, and — when
// the body decoded — the provider's own sentence about why.
//
// It exists because the only carrier those facts ever had was the formatted
// string, and everything downstream that wanted to say something honest about a
// failure had to go mining in it. A room row that reads "API error (404):
// {"error":{"message":"No endpoints found ...\"sh\"..." is that mining not
// happening: a JSON blob delivered to a person as an explanation. With the
// message in a field, the sentence a reader gets is composed from parts rather
// than cut out of transport.
//
// Error() keeps its OPENING byte-for-byte. That is deliberate and load-bearing:
// the harness's provider-error taxonomy recovers a status code by reading the
// text, so a rewording of the `API error (404): …` head would silently disable
// rate-limit and transient-failure retries. What may be added is a tail, and
// [APIError.upstream] is the only thing that adds one.
//
// ── THE MEASURED FAILURE THAT PUT Provider AND Raw ON HERE ──────────────────
//
// SWE-Marathon run s2, 22:45 UTC: a turn died with the whole of what anybody
// was ever told being `error: after 3 retries: API error (400): Provider
// returned error`. That sentence names no provider, carries no upstream body,
// and left five hours of a benchmark's budget unspent with NOTHING in the
// session journal to autopsy — no error row, no endpoint, no status. "Provider
// returned error" is OpenRouter saying that somebody ELSE refused, and the
// somebody and the refusal are both in the JSON it sent: `error.metadata`
// carries `provider_name` and `raw`, and this client threw them away.
type APIError struct {
	// Status is the HTTP status the refusal arrived under.
	Status int
	// Message is the provider's own words, decoded out of the error body. Empty
	// when the body did not decode, in which case Body carries it whole.
	Message string
	// Body is the undecoded payload, kept so nothing is lost when the provider
	// answered with something this client does not know the shape of.
	Body string
	// Provider is the UPSTREAM the router handed this request to, exactly as
	// OpenRouter spells it in `error.metadata.provider_name`. It is EMPTY when
	// the router refused on its own account, and that emptiness is a fact rather
	// than a gap — see [APIError.OurRequest].
	Provider string
	// Raw is the upstream's own answer, out of `error.metadata.raw`, clipped to
	// [maxRawClip]. It is the sentence that says what the 400 actually was, and
	// it is the one thing "Provider returned error" never contains.
	Raw string
}

// Error keeps the SDK's exact error phrasing, and names the upstream when the
// router told us there was one.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = e.Body
	}
	return fmt.Sprintf("API error (%d): %s%s", e.Status, message, e.upstream())
}

// upstream is the short tail Error() adds when the router named who refused: the
// provider's name, and the FIRST SENTENCE of what it said. It is one sentence
// rather than the clip because this string is read by a person on one line of a
// terminal — the whole of Raw is on the journal's error row, where an autopsy
// looks for it.
func (e *APIError) upstream() string {
	if e == nil || strings.TrimSpace(e.Provider) == "" {
		return ""
	}
	said := firstSentence(e.Raw)
	if said == "" {
		return " (via " + e.Provider + ")"
	}
	return " (via " + e.Provider + ": " + said + ")"
}

// FromUpstream reports that THE ENDPOINT THE ROUTER CHOSE is what refused, not
// the router. It is the presence of a provider name and nothing else: OpenRouter
// puts `provider_name` in the metadata exactly when it is relaying somebody
// else's refusal, and leaves it out when it is answering for itself.
//
// It is the distinction that decides whether another endpoint is worth asking.
func (e *APIError) FromUpstream() bool {
	return e != nil && strings.TrimSpace(e.Provider) != ""
}

// OurRequest reports that THE REQUEST IS WHAT IS WRONG, so no endpoint will do
// better with it.
//
// IT IS A SHAPE, NEVER A STATUS LIST. A 4xx that named an upstream is that
// upstream's refusal and another one may well serve it; a 4xx that named nobody
// is the router reading our own bytes and saying no, and asking again — anywhere
// — spends the deadline to be told the same thing. 429 is excluded because it is
// pacing rather than a verdict on the request, and it has its own patience
// (retry.go).
func (e *APIError) OurRequest() bool {
	if e == nil || e.FromUpstream() {
		return false
	}
	return e.Status >= 400 && e.Status < 500 && e.Status != http.StatusTooManyRequests
}

// RefusalFrom recovers the provider's refusal from anywhere in an error chain,
// which is how a caller several wraps away asks the two questions above rather
// than grepping the sentence.
func RefusalFrom(err error) (*APIError, bool) {
	var refusal *APIError
	if errors.As(err, &refusal) && refusal != nil {
		return refusal, true
	}
	return nil, false
}

// firstSentence is the readable head of an upstream body: its first sentence, or
// its first line when it punctuates nothing. A body that is JSON all the way down
// has no sentence in it and answers with its clipped head, which is still more
// than "Provider returned error" ever said.
func firstSentence(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if line, _, found := strings.Cut(raw, "\n"); found {
		raw = strings.TrimSpace(line)
	}
	if head, _, found := strings.Cut(raw, ". "); found {
		raw = strings.TrimSpace(head) + "."
	}
	if len(raw) > maxSentenceClip {
		raw = strings.TrimSpace(raw[:maxSentenceClip]) + "…"
	}
	return raw
}

// errorBody is the shape a refusal arrives in, read for the one field anybody
// downstream can use.
//
// It is declared here rather than reusing the SDK's ai.ErrorResponse, and that
// is a fix rather than a preference: ai.ErrorDetail types `code` as a string,
// OpenRouter sends it as a number, and json.Unmarshal fails the WHOLE object on
// that one field. So every OpenRouter refusal — the routing 404s, which are the
// ones a person most needs explained — fell through to the raw-payload arm and
// arrived as a JSON blob with the readable sentence trapped inside it. Reading
// only the field that is used, and leaving the rest as raw bytes, is what makes
// the message reachable regardless of what a provider types its own codes as.
type errorBody struct {
	Error struct {
		Message string          `json:"message"`
		Code    json.RawMessage `json:"code,omitempty"`
		// Metadata is the router's own dialect and is decoded leniently, for
		// [pacedProviderName]'s reason: a provider that shapes its errors
		// differently simply leaves these empty and behaves exactly as it did
		// before they were read. `raw` is typed as raw JSON because upstreams
		// send it both ways — a string of their body, and their body itself.
		Metadata struct {
			ProviderName string          `json:"provider_name"`
			Raw          json.RawMessage `json:"raw,omitempty"`
		} `json:"metadata"`
	} `json:"error"`
	// Some providers put the sentence at the top level instead.
	Message string `json:"message"`
}

// maxRawClip bounds the upstream body kept on a refusal and written to the
// journal's error row. An upstream can answer with a whole HTML page, and a
// transcript line is not the place for one; two kilobytes is several paragraphs
// of any real provider error and is bounded enough to sit on every failed call.
const maxRawClip = 2 << 10

// maxSentenceClip bounds the ONE SENTENCE a person is shown. It is a line in a
// terminal beside a status code, not a report.
const maxSentenceClip = 160

// apiError decodes one refusal into its parts, the upstream's included.
//
// THE UPSTREAM IS THE POINT. "Provider returned error" is a sentence about
// nothing until it says which provider and what they said, and both are in the
// metadata OpenRouter already sends (see [APIError]).
func apiError(status int, payload []byte) error {
	failure := &APIError{Status: status, Body: string(payload)}
	var decoded errorBody
	if err := json.Unmarshal(payload, &decoded); err == nil {
		if message := strings.TrimSpace(decoded.Error.Message); message != "" {
			failure.Message = message
		} else {
			failure.Message = strings.TrimSpace(decoded.Message)
		}
		failure.Provider = strings.TrimSpace(decoded.Error.Metadata.ProviderName)
		failure.Raw = clipRaw(decoded.Error.Metadata.Raw)
	}
	return failure
}

// clipRaw reads the upstream's own body out of the metadata, whichever of the
// two shapes it arrived in, and bounds it.
func clipRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	text := string(raw)
	// A quoted string is the common shape — the upstream's body, escaped — and it
	// is unquoted so the journal carries prose rather than an escaped blob. A
	// value that is not a string is kept exactly as it was sent.
	var quoted string
	if err := json.Unmarshal(raw, &quoted); err == nil {
		text = quoted
	}
	text = strings.TrimSpace(text)
	if len(text) > maxRawClip {
		text = strings.TrimSpace(text[:maxRawClip]) + "…"
	}
	return text
}
