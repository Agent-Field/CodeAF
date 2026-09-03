// Package lanestub is a fake router with lanes of a scripted speed: the shared
// measuring instrument for everything in `internal/lane`.
//
// ── WHY ONE INSTRUMENT AND NOT FIVE ─────────────────────────────────────────
//
// Five lanes of work are built against this design at once — the sheet and the
// belief, the choice, the watch, the surface, the ledger and the bench — and
// every one of them needs the same three things to exist before it can be
// tested at all: a sheet with plausible percentiles, a stream that starts when
// it says it will and writes at the rate it says it will, and a way to see what
// went out on the wire. Written five times those would be five slightly
// different routers, and the first disagreement between them would look like a
// bug in the code under test.
//
// So it is written once, here, and it is deliberately a LITTLE more than a
// test fixture: it counts requests and cancels per lane, records the routing
// preference of every ask, and can run on a clock that costs nothing, so a
// scenario scripted in seconds finishes in microseconds.
//
// ── WHAT IT SERVES ──────────────────────────────────────────────────────────
//
//	GET  /api/v1/models/{author}/{slug}/endpoints   the sheet, in the router's
//	                                                own field names
//	GET  /api/v1/models                             a minimal catalog
//	POST /api/v1/chat/completions                   a streamed answer from
//	                                                whichever lane the
//	                                                preference selected
//
// [Server.URL] is what a client's BaseURL is set to; it already carries the
// `/api/v1` the router's own URL carries.
//
// ── TWO THINGS TO KNOW BEFORE WRITING A TEST ────────────────────────────────
//
// FIRST, THIS STUB IS A ROUTER BECAUSE OF WHAT IT ANSWERS AND NOT BECAUSE OF
// WHERE IT LIVES, and a test writes nothing to make that true. It serves an
// endpoints page, and a base that serves one carries a routing preference by
// the router's own contract (issue #419 for the sheet, #433 for the
// preference), so a client pointed at the plain [Server.URL] gets a sheet, a
// frontier, and `provider.order` or `provider.only` on the wire — which is what
// [Server.Preference] is here to read back.
//
// That was not true until those two landed. The transport decided both
// questions from the base URL for `openrouter.ai` or from a model spelled
// `openrouter/…`, a loopback address is neither, and so every test that wanted
// to see a preference on the wire had to dress itself up as the shipped router.
// This stub carried that costume — a second mount under a path spelled
// `/openrouter.ai`, handed out by a second accessor — and it is gone (#426):
// there is one address now, and if a preference does not arrive at it, that is
// the product answering.
//
// SECOND, the fast clock is ONE TIMELINE. Its Wait returns at once and advances
// a shared offset, which is exactly right for a scripted single stream and
// meaningless for two streams in flight at the same time. A hedge test — where
// the whole point is that two requests overlap and one is cancelled — uses the
// real clock with millisecond-scale profiles, which still runs a full scenario
// in well under a second.
package lanestub

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// ── THE SCRIPT ──────────────────────────────────────────────────────────────

// Profile is how one lane behaves and what the sheet says about it.
//
// The stream half and the sheet half are separate on purpose: a lane that the
// sheet calls quick and that then takes four seconds is the single most
// interesting thing this package can stage, and it is how the belief gets
// tested against the prior it started from.
type Profile struct {
	// TTFT is the wait before the first token, and Rate how fast tokens come
	// after it. Tokens is how long the answer is, defaulting to
	// [DefaultTokens] and capped by the request's own max_tokens.
	TTFT   time.Duration
	Rate   float64
	Tokens int
	// Reasoning is how many THINKING deltas this lane writes before its first
	// visible word. They are billed, streamed tokens like any other — the
	// endpoint IS writing — but nothing a person can read appears while they
	// run, which is the whole reason the watch counts them apart from the
	// answer ([internal/lane.Watch.Token]).
	Reasoning int
	// Fenced writes that run of thought on the CONTENT channel, wrapped in
	// `<think>` … `</think>`, instead of on the reasoning field.
	//
	// IT IS A REAL SHAPE AND NOT A CURIOSITY. Several gateways hand back a
	// model's working inside the answer channel rather than stripping it, which
	// is why `internal/provider`'s answer.go carves it back out — and the
	// carving has to reach the waiting policy too, or a model that fences its
	// thoughts looks to the controller like a model writing an answer and its
	// silence clock never runs.
	Fenced bool
	// StallAfter and StallFor stage a lane that goes quiet mid-answer:
	// after StallAfter deltas — counting the reasoning run first — nothing is
	// written for StallFor. A zero StallAfter stalls nothing.
	StallAfter int
	StallFor   time.Duration
	// StallUntil holds the stall open on a SIGNAL instead of a duration: after
	// StallAfter deltas nothing is written until this channel closes or the
	// request is cancelled. It overrides StallFor when set, and its zero value
	// is today's behaviour, so nothing that only names a duration changes.
	//
	// IT EXISTS BECAUSE A SCRIPTED ARM MUST ORDER ITSELF BY A SIGNAL AND NEVER
	// BY ELAPSED TIME. A hedge test that wants the rescue to take the answer is
	// asserting a rule — the first arm to finish cleanly commits — and a stalled
	// arm timed to resume a little after the rescue lands asserts nothing but
	// the slack between two wall-clock figures, which a starved machine eats.
	// Held on a channel the test never closes in time, the primary CANNOT
	// finish first, and the assertion is about the rule again.
	StallUntil <-chan struct{}
	// FailWith is an HTTP status this lane answers with instead of streaming.
	// Zero serves normally.
	FailWith int
	// Heartbeats emits the router's own comment lines before the first token,
	// which is the free signal that tells a dead path apart from a slow lane.
	Heartbeats bool

	// Tools, Quant, Context, MaxOut, Uptime and Caches are the gate facts the
	// sheet publishes. Uptime is a percentage.
	Tools   bool
	Quant   string
	Context int
	MaxOut  int
	Uptime  float64
	Caches  bool
	// Status is the router's own health word for the lane: zero is healthy.
	Status int

	// PriceIn, PriceOut and PriceCache are dollars per token, the unit the
	// router publishes.
	PriceIn    float64
	PriceOut   float64
	PriceCache float64

	// TTFTms and Rates are the sheet's four percentiles — p50, p75, p90, p99 —
	// in milliseconds and tokens per second. LEAVE THEM ZERO and the sheet
	// describes a lane that behaves exactly as scripted, with a plausible
	// spread around it; set them to stage a sheet that is WRONG about a lane,
	// which is the case worth most of the tests here.
	TTFTms [4]float64
	Rates  [4]float64
}

// DefaultTokens is how long an answer is when a profile does not say.
const DefaultTokens = 24

// Lane is one named machine behind a model.
type Lane struct {
	Name string
	Profile

	// SheetOnly is a lane the endpoints page PUBLISHES and the completion
	// endpoint will not serve.
	//
	// IT IS THE ONE DISAGREEMENT THIS STUB COULD NOT STAGE, and it is the
	// disagreement issue #266 was measured on. A real router publishes a
	// model's endpoints page under one id and resolves the completion under
	// another, so the machines on the sheet and the machines on the wire are
	// two sets that overlap rather than one set read twice: on 2026-09-01
	// three of five tool-capable lanes on the sheet were not in the router's
	// serving set, a pin was chosen from the sheet, and every request carrying
	// it came back `…but your request's provider.only preference permits only:
	// coreweave`. Serving the sheet and the completions from one slice made
	// that state unreachable, so the defect had no replication a stranger
	// could run.
	//
	// A SheetOnly lane therefore appears in [Lane.row] exactly like any other
	// and is invisible to [pick]. A request that merely RANKS it (`order`)
	// lands on the next lane and never notices; a request that DEMANDS it
	// (`only`) gets the router's real refusal, in the router's own words.
	SheetOnly bool
}

// ── THE CLOCK ───────────────────────────────────────────────────────────────

// Clock is how the stub spends time. It is a seam so that a scenario written in
// seconds does not have to be waited through.
type Clock interface {
	// Now is the moment.
	Now() time.Time
	// Wait passes d, or gives up when ctx is done. It reports whether the wait
	// finished: false is a client that went away, which is what a hedge does to
	// its loser and the one thing this stub counts most carefully.
	Wait(ctx context.Context, d time.Duration) bool
}

// realClock is wall time.
type realClock struct{}

// Real is the wall clock: what a test uses when two requests must overlap.
func Real() Clock { return realClock{} }

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Fast is a clock that costs nothing: waiting advances it and returns at once.
//
// It is one timeline shared by every request in flight — see the note at the
// top of this file about which tests may use it.
type Fast struct {
	mu   sync.Mutex
	at   time.Time
	step time.Duration
}

// NewFast starts a fast clock at a stated moment. Starting it somewhere fixed
// rather than at time.Now is what makes a scenario's timestamps comparable
// between runs.
func NewFast(start time.Time) *Fast { return &Fast{at: start} }

// Now is where the fast clock has got to.
func (f *Fast) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.at
}

// Elapsed is how much scripted time has been spent.
func (f *Fast) Elapsed() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.step
}

// Wait advances and returns immediately.
func (f *Fast) Wait(ctx context.Context, d time.Duration) bool {
	if d > 0 {
		f.mu.Lock()
		f.at = f.at.Add(d)
		f.step += d
		f.mu.Unlock()
	}
	return ctx.Err() == nil
}

// ── WHAT WENT OUT ───────────────────────────────────────────────────────────

// Ask is one request as the wire spelled it, kept so a test can assert on the
// preference rather than on the lane that happened to answer.
type Ask struct {
	Model        string
	Stream       bool
	Tools        bool
	MaxTokens    int
	PromptTokens int
	Sort         string
	Order        []string
	Only         []string
	Ignore       []string
	// At is when the request arrived, on the wall clock and never the scripted
	// one: it is what a test measures a GAP with — how long a run sat between
	// two requests — and a gap measured on a clock the stub itself advances
	// would be a measurement of the script rather than of the code under it.
	At time.Time
}

// ── THE SERVER ──────────────────────────────────────────────────────────────

// Server is the fake router.
type Server struct {
	mu       sync.Mutex
	models   map[string][]Lane
	aliases  map[string]string
	requests map[string]int
	cancels  map[string]int
	sheets   map[string]int
	served   []string
	asks     []Ask
	clock    Clock
	http     *httptest.Server
	next     int
	// sheetless takes the endpoints route away altogether, so every ask for a
	// page answers the 404 a base that is not a router — a bare proxy, a mirror
	// of the completions route alone — answers for an address it has never
	// heard of: a plain HTML not-found page, and never the router's JSON
	// envelope. It is the state a test needs to stage "there is no sheet here"
	// as distinct from "this router does not publish that model", which the
	// route answers on its own for an unknown model.
	sheetless bool
	// anonymous takes the `provider` field OFF every answer, which is what a
	// plain OpenAI-compatible endpoint looks like: it serves the completion and
	// says nothing about which machine did it. It is the state issue #433's
	// honest limit is about — an answer with no lane information cannot be told
	// apart from a routing preference silently ignored — so it is what a test
	// stages to assert that this build takes the safe reading and SAYS so.
	anonymous bool
	// refusesPrefs makes this base answer 400 to any request carrying a
	// `provider` object, in the sentence OpenAI's own API answers with. It is
	// the other half of #433: a base that refuses the field outright rather
	// than ignoring it, whose refusal must cost the request in hand nothing
	// more than one widened retry.
	refusesPrefs bool
	// refusesAll makes this base answer 400 to EVERY request, whether or not it
	// carries a `provider` object. It stages the case that proves #433's retry
	// really is the test: a base whose refusal was never about the field must
	// teach nothing at all, so the widened retry fails too and the question
	// stays open.
	refusesAll bool
}

// New starts a router serving one model over the given lanes, in the order they
// are written: that order is what a request with no preference gets.
func New(model string, lanes ...Lane) *Server {
	server := &Server{
		models:   map[string][]Lane{},
		aliases:  map[string]string{},
		requests: map[string]int{},
		cancels:  map[string]int{},
		sheets:   map[string]int{},
		clock:    Real(),
	}
	server.Model(model, lanes...)
	mux := http.NewServeMux()
	// ONE MOUNT, AT THE ONE ADDRESS [Server.URL] HANDS OUT. There was a second
	// one for a while, under a path spelled `/openrouter.ai`, so that a build
	// which read a router out of its hostname would fetch this stub's sheet;
	// nothing reads a hostname for that any more (#419, #433), so the dress is
	// gone and a test that wants lanes points at the plain URL.
	mux.HandleFunc("GET /api/v1/models/{author}/{slug}/endpoints", server.serveSheet)
	mux.HandleFunc("GET /api/v1/models", server.serveCatalog)
	mux.HandleFunc("POST /api/v1/chat/completions", server.serveCompletion)
	server.http = httptest.NewServer(mux)
	return server
}

// Model adds another model with its own lanes.
func (s *Server) Model(model string, lanes ...Lane) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models[model] = lanes
}

// Alias makes the router ANSWER for a floating id without PUBLISHING one.
//
// That asymmetry is the whole point and it is the router's real behaviour: a
// completion sent with `~deepseek/deepseek-v4-flash-latest` is resolved on
// OpenRouter's side and served by the machines of whatever it currently points
// at, while `/models/~deepseek/deepseek-v4-flash-latest/endpoints` is a 404 —
// the endpoints page exists only under the concrete id. A build that keys its
// beliefs on the spelling it sent therefore holds a ledger about a model no
// sheet will ever describe, which is exactly what a test needs to be able to
// stage.
//
// Both spellings are accepted, with the "~" and without, because the alias
// marker is a prefix on a name rather than part of one.
func (s *Server) Alias(alias, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aliases[alias] = target
	s.aliases[strings.TrimPrefix(alias, "~")] = target
	s.aliases["~"+strings.TrimPrefix(alias, "~")] = target
}

// Sheetless makes this router publish no endpoints route at all: every ask for
// a page is the 404 a base with no such route answers — an HTML not-found page
// ([routelessBody]) — while the completions route keeps serving. It is set
// before any request is made.
//
// THE STUB'S TWO 404S ARE THE LIVE ROUTER'S TWO 404S, because the transport
// tells them apart by their bodies and a stub that answered the same body for
// both would be testing nothing (measured 2026-09-02):
//
//	GET /api/v1/models/nonexistent/model-xyz/endpoints
//	→ 404, {"error":{"message":"Not Found","code":404}}       an unknown model
//
//	GET /api/v1/nonexistent-route/x/endpoints
//	→ 404, <!DOCTYPE html>…<title>Not Found | OpenRouter</title>…   no route
//
// The first is what [serveSheet] already answers for a model this stub does not
// publish, through [writeError]; the second is what this mode answers for every
// model. It exists so that a test can stage the base issue #373 is about — one
// that answers completions and has no sheet — without a second server: the
// sheet must learn that from the answer and not from the hostname, and this is
// the answer.
func (s *Server) Sheetless() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sheetless = true
}

// Anonymous makes this router answer without naming the lane that served, the
// way a plain OpenAI-compatible endpoint does. The lanes still take their turns
// and [Server.Served] still records who answered — what changes is only what
// the WIRE says, which is what the build under test can see. It is set before
// any request is made.
func (s *Server) Anonymous() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.anonymous = true
}

// RefusesPreference makes this base answer 400 to any request that carries a
// `provider` object, in OpenAI's own words for an argument it does not know.
// The request is still recorded in [Server.Asks] before the refusal, so a test
// can see both the ask that was refused and the widened one that followed. It
// is set before any request is made.
func (s *Server) RefusesPreference() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusesPrefs = true
}

// RefusesEverything makes this base answer 400 to every request, in a sentence
// that is about nothing in particular. It is what a base whose 400 was never
// about the routing preference looks like from outside, and it is set before
// any request is made.
func (s *Server) RefusesEverything() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusesAll = true
}

// SetClock replaces the clock. It is set before any request is made.
func (s *Server) SetClock(clock Clock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if clock == nil {
		clock = Real()
	}
	s.clock = clock
}

// URL is what a client's BaseURL is set to. It carries the `/api/v1` prefix the
// real router's own URL carries, so nothing about a client has to be shaped
// differently for the stub.
func (s *Server) URL() string { return s.http.URL + "/api/v1" }

// Close shuts the router down.
func (s *Server) Close() { s.http.Close() }

// Requests is how many asks lane has been given.
func (s *Server) Requests(lane string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[lane]
}

// Cancels is how many of lane's streams the client walked away from. It is the
// figure a hedge is judged on: the loser must be cancelled, because cancelling
// is what stops the bill.
func (s *Server) Cancels(lane string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancels[lane]
}

// Sheets is how many times model's endpoint sheet has been fetched. A send-path
// test asserts that this does not move while requests are being sent.
func (s *Server) Sheets(model string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sheets[model]
}

// Served is every lane that answered, in order.
func (s *Server) Served() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.served...)
}

// Asks is every request as it arrived, in order.
func (s *Server) Asks() []Ask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Ask(nil), s.asks...)
}

// ── THE SHEET ───────────────────────────────────────────────────────────────

// sheetPricing is the router's own money block. The figures are STRINGS on the
// wire — dollars per token, with more precision than a float in JSON survives
// being read back by everybody's decoder — and the catalog reader in this repo
// already expects them that way.
type sheetPricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

type sheetPercentiles struct {
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P90 float64 `json:"p90"`
	P99 float64 `json:"p99"`
}

type sheetToolChoice struct {
	Function bool `json:"function"`
	Auto     bool `json:"auto"`
	None     bool `json:"none"`
	Required bool `json:"required"`
}

// sheetEndpoint is one lane's row, in the router's field names exactly.
type sheetEndpoint struct {
	ProviderName          string           `json:"provider_name"`
	Tag                   string           `json:"tag"`
	Quantization          string           `json:"quantization"`
	ContextLength         int              `json:"context_length"`
	MaxCompletionTokens   int              `json:"max_completion_tokens"`
	Pricing               sheetPricing     `json:"pricing"`
	SupportsToolChoice    sheetToolChoice  `json:"supports_tool_choice"`
	Status                int              `json:"status"`
	UptimeLast30m         float64          `json:"uptime_last_30m"`
	UptimeLast5m          float64          `json:"uptime_last_5m"`
	UptimeLast1d          float64          `json:"uptime_last_1d"`
	SupportsImplicitCache bool             `json:"supports_implicit_caching"`
	LatencyLast30m        sheetPercentiles `json:"latency_last_30m"`
	ThroughputLast30m     sheetPercentiles `json:"throughput_last_30m"`
}

type sheetBody struct {
	Data struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Endpoints []sheetEndpoint `json:"endpoints"`
	} `json:"data"`
}

func (s *Server) serveSheet(w http.ResponseWriter, r *http.Request) {
	model := r.PathValue("author") + "/" + r.PathValue("slug")
	s.mu.Lock()
	lanes, known := s.models[model]
	sheetless := s.sheetless
	s.sheets[model]++
	s.mu.Unlock()
	// A sheetless router counts the ask before refusing it, because the count
	// is the whole of what a test asserts: how many times a base that said
	// "no route here" was asked again. And it refuses as a base with no such
	// route does — a page, not an envelope — which is the one 404 the transport
	// may remember a base by ([Server.Sheetless]).
	if sheetless {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(routelessBody))
		return
	}
	if !known {
		writeError(w, http.StatusNotFound, "No endpoints found for that model", "")
		return
	}
	var body sheetBody
	body.Data.ID = model
	body.Data.Name = model
	for _, lane := range lanes {
		body.Data.Endpoints = append(body.Data.Endpoints, lane.row(model))
	}
	writeJSON(w, http.StatusOK, body)
}

// row is the sheet's account of one lane. Percentiles a profile did not state
// are derived from what it actually does, with the spread a real lane has —
// which makes "the sheet is right about this lane" the default and leaves
// "the sheet is wrong about this lane" as something a test must say out loud.
func (l Lane) row(model string) sheetEndpoint {
	latency := l.TTFTms
	if latency == [4]float64{} {
		base := float64(l.TTFT.Milliseconds())
		latency = [4]float64{base, base * 1.4, base * 2.2, base * 6}
	}
	rates := l.Rates
	if rates == [4]float64{} {
		rates = [4]float64{l.Rate, l.Rate * 1.2, l.Rate * 1.6, l.Rate * 2.2}
	}
	uptime := l.Uptime
	if uptime == 0 {
		uptime = 100
	}
	return sheetEndpoint{
		ProviderName:        l.Name,
		Tag:                 strings.ToLower(strings.ReplaceAll(l.Name, " ", "-")),
		Quantization:        l.Quant,
		ContextLength:       l.Context,
		MaxCompletionTokens: l.MaxOut,
		Pricing: sheetPricing{
			Prompt:         money(l.PriceIn),
			Completion:     money(l.PriceOut),
			InputCacheRead: money(l.PriceCache),
		},
		SupportsToolChoice: sheetToolChoice{
			Function: l.Tools, Auto: l.Tools, Required: l.Tools, None: true,
		},
		Status:                l.Status,
		UptimeLast30m:         uptime,
		UptimeLast5m:          uptime,
		UptimeLast1d:          uptime,
		SupportsImplicitCache: l.Caches,
		LatencyLast30m:        sheetPercentiles{latency[0], latency[1], latency[2], latency[3]},
		ThroughputLast30m:     sheetPercentiles{rates[0], rates[1], rates[2], rates[3]},
	}
}

// money spells a per-token price the way the router does.
func money(price float64) string { return fmt.Sprintf("%.10g", price) }

// serveCatalog is the minimal model list, so that a catalog warm-up run against
// this stub finds the model rather than a 404.
func (s *Server) serveCatalog(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	models := make([]map[string]any, 0, len(s.models))
	for id, lanes := range s.models {
		entry := map[string]any{
			"id":             id,
			"name":           id,
			"context_length": 0,
			"architecture": map[string]any{
				"input_modalities":  []string{"text"},
				"output_modalities": []string{"text"},
			},
			"pricing":              map[string]string{"prompt": "0", "completion": "0"},
			"supported_parameters": []string{"max_tokens", "tools"},
		}
		if len(lanes) > 0 {
			entry["context_length"] = lanes[0].Context
			entry["pricing"] = map[string]string{
				"prompt":     money(lanes[0].PriceIn),
				"completion": money(lanes[0].PriceOut),
			}
		}
		models = append(models, entry)
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": models})
}

// ── THE STREAM ──────────────────────────────────────────────────────────────

// wireAsk is as much of the request as this stub reads.
type wireAsk struct {
	Model     string            `json:"model"`
	Stream    bool              `json:"stream"`
	MaxTokens *int              `json:"max_tokens"`
	Tools     []json.RawMessage `json:"tools"`
	Messages  []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Provider *struct {
		Sort   string   `json:"sort"`
		Order  []string `json:"order"`
		Only   []string `json:"only"`
		Ignore []string `json:"ignore"`
	} `json:"provider"`
}

func (s *Server) serveCompletion(w http.ResponseWriter, r *http.Request) {
	var ask wireAsk
	if err := json.NewDecoder(r.Body).Decode(&ask); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request", "")
		return
	}
	record := Ask{
		Model: ask.Model, Stream: ask.Stream, Tools: len(ask.Tools) > 0,
		PromptTokens: promptTokens(ask), At: time.Now(),
	}
	if ask.MaxTokens != nil {
		record.MaxTokens = *ask.MaxTokens
	}
	if ask.Provider != nil {
		record.Sort = ask.Provider.Sort
		record.Order, record.Only, record.Ignore = ask.Provider.Order, ask.Provider.Only, ask.Provider.Ignore
	}

	s.mu.Lock()
	s.asks = append(s.asks, record)
	if s.refusesAll {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "this base is having an afternoon", "")
		return
	}
	if s.refusesPrefs && ask.Provider != nil {
		s.mu.Unlock()
		// THE ASK IS ON THE RECORD AND NO LANE IS CHARGED FOR IT. Nothing
		// served this request, so counting it against a lane would be a
		// measurement of a machine that never saw it.
		writeError(w, http.StatusBadRequest, "Unrecognized request argument supplied: provider", "")
		return
	}
	// A floating id is resolved here and nowhere else: the sheet handler does
	// not consult the aliases, because the router publishes no endpoints page
	// for one. See [Server.Alias].
	served := ask.Model
	if target, floating := s.aliases[served]; floating {
		served = target
	}
	lanes := s.models[served]
	clock := s.clock
	s.mu.Unlock()

	lane, found := pick(lanes, record)
	if !found {
		// A DEMAND THAT NAMED NOBODY THE ROUTER SERVES GETS THE ROUTER'S OWN
		// SENTENCE ABOUT IT, which is a different refusal from an empty set
		// with no demand in it and is answered differently by everything
		// downstream (internal/provider's classifier, issue #266). The ask is
		// still counted against every lane it demanded — the request WAS
		// addressed to them — so a test can assert that a refused lane is never
		// asked a second time.
		if len(record.Only) > 0 {
			s.mu.Lock()
			for _, demanded := range record.Only {
				s.requests[canonical(lanes, demanded)]++
			}
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, permitsOnly(served, lanes, record.Only), "")
			return
		}
		// The router's own words when a preference has emptied the set. It is
		// the shape the endpoint-refusal ladder in `internal/provider` reads,
		// so a relaxation test gets the real thing.
		writeError(w, http.StatusNotFound, "No endpoints found matching your data policy", "")
		return
	}

	s.mu.Lock()
	s.requests[lane.Name]++
	s.served = append(s.served, lane.Name)
	s.next++
	id := fmt.Sprintf("gen-%d", s.next)
	// WHAT THE WIRE SAYS AND WHAT REALLY HAPPENED ARE TWO THINGS HERE. The lane
	// answered and the ledger above records that it did; `named` is only what
	// the ANSWER admits to, which [Server.Anonymous] empties.
	named := lane.Name
	if s.anonymous {
		named = ""
	}
	s.mu.Unlock()

	if lane.FailWith != 0 {
		writeError(w, lane.FailWith, "the lane refused", lane.Name)
		return
	}
	if !ask.Stream {
		s.serveWhole(w, r, clock, id, named, ask, lane, record)
		return
	}
	s.serveStream(w, r, clock, id, named, ask, lane, record)
}

// pick is the preference honoured: `only` is a demand, `ignore` is a veto, and
// `order` is a ranking among whatever survives both. With no preference at all
// the first lane declared answers, which is what makes a test's scripted order
// mean something.
//
// A [Lane.SheetOnly] lane is not here at all: the completion endpoint has never
// heard of it, whatever the endpoints page says.
func pick(lanes []Lane, ask Ask) (Lane, bool) {
	allowed := make([]Lane, 0, len(lanes))
	for _, lane := range lanes {
		if lane.SheetOnly {
			continue
		}
		if len(ask.Only) > 0 && !names(ask.Only, lane.Name) {
			continue
		}
		if names(ask.Ignore, lane.Name) {
			continue
		}
		allowed = append(allowed, lane)
	}
	if len(allowed) == 0 {
		return Lane{}, false
	}
	for _, wanted := range ask.Order {
		for _, lane := range allowed {
			if equalName(lane.Name, wanted) {
				return lane, true
			}
		}
	}
	return allowed[0], true
}

// permitsOnly is the router's real sentence when `provider.only` named nothing
// the router will serve this model from, copied from the body a live run
// collected on 2026-09-01 (issue #266) down to its punctuation.
//
// IT NAMES THE SET IT DOES SERVE, and that is the half worth staging: the
// refusal carries the answer to "then who?" while the layer that reads it is
// deciding whether the demanded lane is worth asking again. It is prose, and no
// classification in this repository is allowed to be parsed out of it — the
// lane a refusal is ABOUT comes from the request's own `only`, never from these
// words.
func permitsOnly(model string, lanes []Lane, only []string) string {
	serving := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		if !lane.SheetOnly {
			serving = append(serving, strings.ToLower(lane.Name))
		}
	}
	demanded := make([]string, 0, len(only))
	for _, name := range only {
		demanded = append(demanded, strings.ToLower(name))
	}
	return "No endpoints found for " + model + ". Providers serving " + model + ": " +
		strings.Join(serving, ", ") +
		", but your request's provider.only preference permits only: " +
		strings.Join(demanded, ", ")
}

// canonical is a demanded lane spelled the way this stub declared it, so that
// [Server.Requests] counts one machine under one name however the wire spelled
// it. A word naming no declared lane is counted under itself, because a test
// that demanded a machine the router never had still asked for something.
func canonical(lanes []Lane, demanded string) string {
	for _, lane := range lanes {
		if equalName(lane.Name, demanded) {
			return lane.Name
		}
	}
	return demanded
}

// names reports whether a list of preference words names this lane. The router
// matches its own slugs case-insensitively and treats a hyphen and a space
// alike, and so does this.
func names(list []string, lane string) bool {
	for _, word := range list {
		if equalName(word, lane) {
			return true
		}
	}
	return false
}

func equalName(a, b string) bool {
	norm := func(word string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(word), "-", " "))
	}
	return norm(a) == norm(b)
}

// serveStream writes the answer as the router does: comment lines while nothing
// has happened yet, one chunk per token with the serving lane named on every
// one of them, a usage frame, and the sentinel.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, clock Clock, id, named string, ask wireAsk, lane Lane, record Ask) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flush := http.NewResponseController(w)
	_ = flush.Flush()

	write := func(line string) bool {
		if _, err := fmt.Fprint(w, line); err != nil {
			return false
		}
		return flush.Flush() == nil
	}

	// The wait before the first token, spent in pieces when there are
	// heartbeats to emit through it. A heartbeat is a claim about the PATH and
	// not about the endpoint, which is why it is emitted even by a lane that is
	// about to be very slow.
	if lane.Heartbeats && lane.TTFT > 0 {
		const beats = 3
		for beat := 0; beat < beats; beat++ {
			if !clock.Wait(ctx, lane.TTFT/beats) {
				s.cancelled(lane.Name)
				return
			}
			if !write(": OPENROUTER PROCESSING\n\n") {
				s.cancelled(lane.Name)
				return
			}
		}
	} else if !clock.Wait(ctx, lane.TTFT) {
		s.cancelled(lane.Name)
		return
	}

	total := lane.Tokens
	if total <= 0 {
		total = DefaultTokens
	}
	if ask.MaxTokens != nil && *ask.MaxTokens > 0 && *ask.MaxTokens < total {
		total = *ask.MaxTokens
	}
	gap := time.Duration(0)
	if lane.Rate > 0 {
		gap = time.Duration(float64(time.Second) / lane.Rate)
	}

	// THE RUN OF THOUGHT COMES FIRST AND IS DELTAS LIKE ANY OTHER. A reasoning
	// model writes its thinking on the same stream, at the same rate, billed
	// the same way — and a person reads none of it, which is exactly the
	// difference the watch's commitment rule turns on.
	delta := 0
	pause := func() bool {
		if delta == 0 {
			delta++
			return true
		}
		wait := gap
		// A stall held on a signal spends NO scripted time, so the clock's own
		// ledger of elapsed time stays a sum of the durations the script named
		// — the wait here is the ordinary gap between two tokens and nothing
		// more. Only after that gap is spent does the arm hang on the channel.
		stalling := lane.StallAfter > 0 && delta == lane.StallAfter
		held := stalling && lane.StallUntil != nil
		if stalling && !held {
			wait += lane.StallFor
		}
		delta++
		if !clock.Wait(ctx, wait) {
			return false
		}
		if held {
			select {
			case <-lane.StallUntil:
			case <-ctx.Done():
				return false
			}
		}
		return true
	}
	for thought := 0; thought < lane.Reasoning; thought++ {
		if !pause() {
			s.cancelled(lane.Name)
			return
		}
		text := fmt.Sprintf("r%d ", thought)
		frame := reasoningJSON(id, ask.Model, named, text)
		if lane.Fenced {
			// The whole run inside one pair of tags: opened on the first delta
			// and closed on the last, which is how a gateway that does not strip
			// its model's working delivers it.
			if thought == 0 {
				text = "<think>" + text
			}
			if thought == lane.Reasoning-1 {
				text += "</think>"
			}
			frame = chunkJSON(id, ask.Model, named, text)
		}
		if !write("data: " + frame + "\n\n") {
			s.cancelled(lane.Name)
			return
		}
	}
	for token := 0; token < total; token++ {
		if !pause() {
			s.cancelled(lane.Name)
			return
		}
		if !write("data: " + chunkJSON(id, ask.Model, named, fmt.Sprintf("t%d ", token)) + "\n\n") {
			s.cancelled(lane.Name)
			return
		}
	}
	if !write("data: " + finishJSON(id, ask.Model, named) + "\n\n") {
		s.cancelled(lane.Name)
		return
	}
	cost := lane.PriceIn*float64(record.PromptTokens) + lane.PriceOut*float64(total)
	if !write("data: " + usageJSON(id, ask.Model, named, record.PromptTokens, total, cost) + "\n\n") {
		s.cancelled(lane.Name)
		return
	}
	write("data: [DONE]\n\n")
}

// serveWhole answers a request that did not ask to stream. It exists so that
// nothing in this stub has to be special-cased by a caller that streams
// sometimes; the timings are honoured the same way.
func (s *Server) serveWhole(w http.ResponseWriter, r *http.Request, clock Clock, id, named string, ask wireAsk, lane Lane, record Ask) {
	total := lane.Tokens
	if total <= 0 {
		total = DefaultTokens
	}
	whole := lane.TTFT
	if lane.Rate > 0 {
		whole += time.Duration(float64(total) / lane.Rate * float64(time.Second))
	}
	if !clock.Wait(r.Context(), whole) {
		s.cancelled(lane.Name)
		return
	}
	var answer strings.Builder
	for token := 0; token < total; token++ {
		fmt.Fprintf(&answer, "t%d ", token)
	}
	cost := lane.PriceIn*float64(record.PromptTokens) + lane.PriceOut*float64(total)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"object":   "chat.completion",
		"created":  clock.Now().Unix(),
		"model":    ask.Model,
		"provider": named,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": answer.String()},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     record.PromptTokens,
			"completion_tokens": total,
			"total_tokens":      record.PromptTokens + total,
			"cost":              cost,
		},
	})
}

// cancelled records a client that walked away. It is counted per lane because
// the question a hedge test asks is not "was anything cancelled" but "was the
// LOSER cancelled", and on about twenty lanes that is what stops the bill.
func (s *Server) cancelled(lane string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancels[lane]++
}

// chunkJSON is one delta, with the serving lane named on it. THE NAME IS ON
// EVERY CHUNK, as the router puts it there: it is the only way a stream says
// who is answering, and the watch reads it from the first one.
func chunkJSON(id, model, lane, text string) string {
	body, _ := json.Marshal(map[string]any{
		"id":       id,
		"object":   "chat.completion.chunk",
		"created":  0,
		"model":    model,
		"provider": lane,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"role": "assistant", "content": text},
			"finish_reason": nil,
		}},
	})
	return string(body)
}

// reasoningJSON is one THINKING delta, in the field OpenRouter spells it with.
// It is the same frame as [chunkJSON] with the text under `reasoning` instead
// of `content`: on the wire the two are one stream, and telling them apart is
// the reader's job rather than the router's.
func reasoningJSON(id, model, lane, text string) string {
	body, _ := json.Marshal(map[string]any{
		"id":       id,
		"object":   "chat.completion.chunk",
		"created":  0,
		"model":    model,
		"provider": lane,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"role": "assistant", "reasoning": text},
			"finish_reason": nil,
		}},
	})
	return string(body)
}

func finishJSON(id, model, lane string) string {
	body, _ := json.Marshal(map[string]any{
		"id":       id,
		"object":   "chat.completion.chunk",
		"created":  0,
		"model":    model,
		"provider": lane,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
	})
	return string(body)
}

// usageJSON is the terminal frame, and it carries the EXACT cost the router
// billed rather than a figure anybody has to derive from a price table.
func usageJSON(id, model, lane string, prompt, completion int, cost float64) string {
	body, _ := json.Marshal(map[string]any{
		"id":       id,
		"object":   "chat.completion.chunk",
		"created":  0,
		"model":    model,
		"provider": lane,
		"choices":  []any{},
		"usage": map[string]any{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      prompt + completion,
			"cost":              cost,
		},
	})
	return string(body)
}

// promptTokens is roughly how long the conversation was, at about four
// characters to the token. It is an approximation and it is stated as one: what
// the tests here turn on is that the figure moves with the prompt, never that
// it matches anybody's tokenizer.
func promptTokens(ask wireAsk) int {
	characters := 0
	for _, message := range ask.Messages {
		characters += len(message.Content)
	}
	return int(math.Ceil(float64(characters) / 4))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError is the router's own error envelope, with the lane named when there
// is one to name — which is what tells a refusal by an endpoint apart from a
// refusal by the router itself.
// routelessBody is the 404 a base with no endpoints route answers: the shape
// of the live router's own not-found page for an address it does not serve,
// cut to the two tags that make it a page and not an envelope.
const routelessBody = "<!DOCTYPE html><title>Not Found</title>"

func writeError(w http.ResponseWriter, status int, message, lane string) {
	body := map[string]any{"error": map[string]any{"message": message, "code": status}}
	if lane != "" {
		body["provider"] = lane
		body["error"].(map[string]any)["metadata"] = map[string]any{"provider_name": lane}
	}
	writeJSON(w, status, body)
}
