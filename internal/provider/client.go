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
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Config configures the adapter. It is deliberately the same shape the
// AgentField SDK client takes, plus the two resolvers that let the adapter
// decide a request's economics without ever performing I/O on the hot path.
// It carries no attribution fields on purpose: who this binary reports itself
// as is a constant (attribution.go), and a config field for it is exactly how a
// caller ends up sending a different app — or none.
type Config struct {
	APIKey      string
	BaseURL     string
	Model       string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration

	// SupportsParameter answers "does this model accept this request field?"
	// from data already in memory. It must not block or perform I/O; an unknown
	// answer is reported by returning known=false, never by waiting.
	SupportsParameter func(model, parameter string) (bool, bool)

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
			APIKey:      key,
			BaseURL:     c.config.BaseURL,
			Model:       c.config.Model,
			Temperature: c.config.Temperature,
			MaxTokens:   c.config.MaxTokens,
			Timeout:     c.config.Timeout,
			SiteURL:     AppURL,
			SiteName:    AppName,
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
	if c.isOpenRouter() {
		return c.executeOwnToolCallLoop(ctx, messages, tools, config, call, options...)
	}
	base := c.sdkClient()
	if base == nil {
		return nil, nil, ErrNoAPIKey
	}
	return base.ExecuteToolCallLoop(ctx, messages, tools, config, call, options...)
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
	// relaxed is what this encode has been told to leave off the body, set only
	// by the endpoint-refusal chain (endpoints.go). Zero on every ordinary call,
	// which is what keeps a healthy request byte-for-byte what it always was.
	relaxed relaxSet
	// reasoning is aligned with the request's messages. It stays outside the SDK
	// values because ai.Message has no reasoning fields of its own.
	reasoning []MessageReasoning
}

func knobsFrom(ctx context.Context) callKnobs {
	return callKnobs{
		cacheKey:  CacheKeyFrom(ctx),
		effort:    effortFrom(ctx),
		intent:    routingIntentFrom(ctx),
		reasoning: MessageReasoningFrom(ctx),
	}
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
	response, err := c.sendRecovered(ctx, request, knobs, stream)
	if err != nil || (response != nil && response.StatusCode >= 400) {
		c.releaseEndpoint(ctx, c.modelFor(request))
	}
	return response, err
}

// sendRecovered is sendShaped's two recovery passes — the repairable 400s and
// the endpoint-refusal ladder — with nothing said about pins.
func (c *Client) sendRecovered(ctx context.Context, request *ai.Request, knobs callKnobs, stream bool) (*http.Response, error) {
	response, err := c.sendRepaired(ctx, request, knobs, stream)
	if err != nil {
		return c.recoverFromPacing(ctx, request, knobs, stream, err)
	}
	if !endpointRefusalStatus(response.StatusCode) {
		return response, nil
	}
	peek, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
	if readErr != nil || !endpointRefusal(peek) {
		// Not this class. The body is handed back whole — a peek must never
		// shorten what the caller goes on to read.
		response.Body = rewound(peek, response.Body)
		return response, nil
	}
	response.Body.Close()
	return c.recoverFromRefusal(ctx, request, knobs, stream, peek)
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
	response, err := c.send(ctx, request, body, stream)
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
		knobs.reasoning = nil
		return c.resend(ctx, request, knobs, stream, response)
	case repairable && c.learn(model, knobs, peek):
		return c.resend(ctx, request, knobs, stream, response)
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
	return c.send(ctx, request, body, stream)
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

// learn reads a refusal for the facts this adapter can remember and reports
// whether the next encode will differ. Every memo is consulted rather than the
// first match winning, because a single 400 can name more than one field.
func (c *Client) learn(model string, knobs callKnobs, payload []byte) bool {
	learned := false
	if c.resolveEffort(model, knobs.effort) == EffortOff && refusesDisabledReasoning(payload) {
		noteReasoningMandatory(model)
		learned = true
	}
	// THE BUDGET IS DROPPED AND THE LEVEL IS KEPT. An endpoint that will not take
	// a thinking allowance still takes the effort word, so the two top rungs of
	// the ladder degrade to the deepest thing this endpoint has a word for
	// instead of falling off it (wire.go's resolveReasoningBudget).
	if c.resolveReasoningBudget(model, knobs.effort) > 0 && refusesReasoningBudget(payload) {
		noteReasoningBudgetRefused(model)
		learned = true
	}
	if c.dialectFor(model) == cacheDialectBreakpoints && refusesCacheControl(payload) {
		noteCacheControlRefused(model)
		learned = true
	}
	if len(knobs.reasoning) > 0 && !reasoningReplayRefused(model) && refusesReasoningReplay(payload, knobs.reasoning) {
		noteReasoningReplayRefused(model)
		learned = true
	}
	return learned
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
	// Temperature is always sent: an operator who configured zero wants zero,
	// not whatever the endpoint happens to default to.
	temperature := c.config.Temperature
	request.Temperature = &temperature
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

// CompleteWithMessages performs one completion. Interactive callers may attach
// a stream observer while retaining the accumulated response contract.
func (c *Client) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if observer := streamObserverFrom(ctx); observer != nil {
		return c.completeWithMessagesStreaming(ctx, observer, messages, options...)
	}
	request, err := c.newRequest(messages, options)
	if err != nil {
		return nil, err
	}
	response, err := c.completeOnce(ctx, request)
	if err != nil {
		return nil, err
	}
	// AN EMPTY ANSWER AT THE CEILING IS A FACT, NOT A RESULT. The 400 path above
	// learns a model that refuses the disable; this is the other way the same
	// thing shows — the endpoint accepted the disable, thought anyway, and the
	// whole ceiling went to the pass. Learned once, the next encode leaves room
	// (wire.go's thinkingCeiling), so the call is made again with the answer it
	// was always going to need. Once, because a second empty answer WITH the
	// room is a model that has nothing to say, and that is the caller's to hear.
	if c.learnFromAnswer(c.modelFor(request), request, response) {
		return c.completeOnce(ctx, request)
	}
	return response, nil
}

// learnFromAnswer reads one answer for the fact the adapter can act on and
// reports whether the next encode will differ. It stays silent for a model
// the memo already knows — the room was already there, so a blank answer
// says nothing new — and for a call that set no ceiling to spend.
func (c *Client) learnFromAnswer(model string, request *ai.Request, response *ai.Response) bool {
	if request.MaxTokens == nil || ReasoningUnavoidable(model) {
		return false
	}
	if !EmptyAtCeiling(response, *request.MaxTokens) {
		return false
	}
	NoteReasoningDisableIgnored(model)
	return true
}

// completeOnce is one send and one parse: the request as shaped, the answer as
// served, and the measurements both feed. CompleteWithMessages owns the
// decision to do it twice.
func (c *Client) completeOnce(ctx context.Context, request *ai.Request) (*ai.Response, error) {
	began := c.clock()
	httpResponse, err := c.sendShaped(ctx, request, knobsFrom(ctx), false)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if httpResponse.StatusCode >= 400 {
		refusal := apiError(httpResponse.StatusCode, payload)
		// AND THE LANE GOES, so the caller's next attempt is encoded away from
		// the upstream that just refused (velocity.go's refuseUpstream). The pin
		// was already released on the way out of sendShaped; releasing a pin only
		// stops us ASKING for that endpoint, and the router chooses it again.
		c.refuseUpstream(c.modelFor(request), refusal)
		return nil, refusal
	}
	// ONE PARSE. The answer and the router's annotation on it come out of the
	// same decode, because the alternative was reading a megabyte of completion
	// twice to recover one short string from the second pass.
	var decoded servedResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	response := decoded.Response
	// A non-streamed answer has no first token to wait for — the whole thing
	// arrives at once — so it is rated and never judged on TTFT, and it has no
	// mid-stream gaps to judge either. Passing zero says "unmeasured" rather
	// than "instant" (velocity.go).
	// ONE NAME, read off the decode above rather than from a second pass over
	// the payload, and handed to both readers of it: the affinity that keeps a
	// conversation on the endpoint holding its prompt cache, and the rating.
	served := servedProvider(decoded.Provider)
	noteServed(ctx, served, c.noteEndpointAffinity(ctx, c.modelFor(request), served, response.Usage))
	c.noteVelocity(
		c.modelFor(request),
		served,
		0,
		outputTokens(&response, ""),
		c.clock().Sub(began),
		0,
	)
	return &response, nil
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
	return len(text) / 4
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

// completeWithMessagesStreaming preserves the completion interface while
// exposing each text delta to an interactive observer. The accumulated
// response is the same shape callers already parse after the stream closes.
func (c *Client) completeWithMessagesStreaming(
	ctx context.Context,
	observer StreamObserver,
	messages []ai.Message,
	options ...ai.Option,
) (*ai.Response, error) {
	request, err := c.newRequest(messages, append(append([]ai.Option(nil), options...), ai.WithStream()))
	if err != nil {
		return nil, err
	}
	request.Stream = true
	began := c.clock()
	// THE GUARD'S OWN CANCEL, above send's. A stream that has to be cut — the
	// endpoint gone quiet, the reply gone to soup — is cut by cancelling the
	// request, because the reader is parked inside Read on a socket and only the
	// transport can unblock it (the same reasoning transport.go's idle watchdog
	// gives). It is deferred so every path releases the request goroutine,
	// including the ordinary clean end.
	guardCtx, cutStream := context.WithCancel(ctx)
	defer cutStream()
	httpResponse, err := c.sendShaped(guardCtx, request, knobsFrom(ctx), true)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(httpResponse.Body, maxErrorPeek))
		refusal := apiError(httpResponse.StatusCode, payload)
		c.refuseUpstream(c.modelFor(request), refusal)
		return nil, refusal
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
	stall := newStallWatch(cutStream, c.streamWall(c.modelFor(request), ""))
	defer stall.stop()
	// And the degeneration guard, unless this call has it switched off. It is
	// nil rather than dormant when off, so a call that is not watching pays
	// nothing per delta for the fact.
	var babble *babbleWatch
	if babbleGuardOn(ctx) {
		babble = &babbleWatch{}
	}
	// THE ONLY PLACE TTFT IS REALLY OBSERVABLE. The two facts the ledger wants
	// are separated by the stream itself: how long the endpoint took to say
	// anything, and how fast it wrote once it had started. Timing them together
	// would price a warm endpoint behind a long prompt as a slow one.
	var served string
	var firstToken time.Time

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
	finishReason := ""
	thinking := false
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
	// lastWrite and widestGap watch the same deltas the stall guard does, for
	// the ledger rather than for a cut: an endpoint that finished its answer
	// but delivered it in lumps is working, slowly, and "working slowly" is
	// the lag law's department (velocity.go's LagGap).
	var lastWrite time.Time
	var widestGap time.Duration
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
				// A stream that went quiet is an endpoint failing this lineage,
				// which is the one thing that moves a pin (affinity.go).
				c.releaseEndpoint(ctx, c.modelFor(request))
				return nil, cut
			}
			return nil, fmt.Errorf("decode stream: %w", decodeErr)
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
			if served == "" {
				// The first naming is what narrows the wall onto the lane that
				// is actually serving; [stallWatch.rewall] does it once and
				// measures the new bound from when the stream opened.
				stall.rewall(c.streamWall(c.modelFor(request), chunk.Provider))
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
			c.refuseUpstream(c.modelFor(request), refusal)
			c.releaseEndpoint(ctx, c.modelFor(request))
			return nil, refusal
		}
		if chunk.Usage != nil {
			response.Usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
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
			}
			if choice.Delta.Content != "" {
				thinking = false
				content.WriteString(choice.Delta.Content)
				observer(StreamEvent{Kind: StreamDelta, Delta: choice.Delta.Content, Session: session})
				// AND THE JUNK STOPS HERE. The delta has already been handed to
				// the observer — a person watches text arrive and the surface
				// throws away what a cut turn streamed — but nothing past this
				// point becomes a response, so no soup is ever returned to the
				// turn loop and none of it reaches the transcript.
				if babble != nil && babble.write(choice.Delta.Content) {
					cut := &StreamCut{Reason: CutBabble}
					c.stampCut(cut, served, began, content.String())
					cut.Rerouted = c.noteCutProvider(c.modelFor(request), served)
					// An endpoint producing soup has failed this lineage as
					// surely as one that went quiet, so the pin moves too.
					c.releaseEndpoint(ctx, c.modelFor(request))
					return nil, cut
				}
			}
			// The run of reasoning is announced ONCE — that boundary is what a
			// surface drawing "thinking…" needs — and the text of it follows per
			// delta as StreamReasoning, for a surface that shows the thought.
			// It is not accumulated into answer Content. Its wire field and details
			// ride the event so the session can replay it as assistant metadata.
			if choice.Delta.thinking() {
				if !thinking {
					thinking = true
					observer(StreamEvent{Kind: StreamThinking, Session: session})
				}
				events, count := choice.Delta.reasoningEvents()
				for _, event := range events[:count] {
					event.Session = session
					observer(event)
				}
			}
			for _, fragment := range choice.Delta.ToolCalls {
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
		)
	} else {
		c.noteVelocity(c.modelFor(request), served, generation.Sub(began), 0, 0, 0)
	}
	finished = true
	observer(StreamEvent{Kind: StreamFinished, Session: session})
	return response, nil
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
	if c.isOpenRouter() {
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

func (c *Client) isOpenRouter() bool {
	return strings.Contains(strings.ToLower(c.config.BaseURL), "openrouter.ai") ||
		strings.HasPrefix(strings.ToLower(c.config.Model), "openrouter/")
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
