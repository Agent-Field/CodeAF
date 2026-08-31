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
// FIRST, the transport only sends a routing preference to something it believes
// is a router, and it decides that from the base URL or the configured model
// (`internal/provider/client.go`, isOpenRouter). A loopback address is neither,
// so a test that wants to see `provider.order` on the wire configures its
// client with a model spelled `openrouter/…` — the per-request model stays
// whatever the test is really about.
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
	// StallAfter and StallFor stage a lane that goes quiet mid-answer:
	// after StallAfter tokens nothing is written for StallFor. A zero
	// StallAfter stalls nothing.
	StallAfter int
	StallFor   time.Duration
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
}

// ── THE SERVER ──────────────────────────────────────────────────────────────

// Server is the fake router.
type Server struct {
	mu       sync.Mutex
	models   map[string][]Lane
	requests map[string]int
	cancels  map[string]int
	sheets   map[string]int
	served   []string
	asks     []Ask
	clock    Clock
	http     *httptest.Server
	next     int
}

// New starts a router serving one model over the given lanes, in the order they
// are written: that order is what a request with no preference gets.
func New(model string, lanes ...Lane) *Server {
	server := &Server{
		models:   map[string][]Lane{},
		requests: map[string]int{},
		cancels:  map[string]int{},
		sheets:   map[string]int{},
		clock:    Real(),
	}
	server.Model(model, lanes...)
	mux := http.NewServeMux()
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
	s.sheets[model]++
	s.mu.Unlock()
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
	record := Ask{Model: ask.Model, Stream: ask.Stream, Tools: len(ask.Tools) > 0, PromptTokens: promptTokens(ask)}
	if ask.MaxTokens != nil {
		record.MaxTokens = *ask.MaxTokens
	}
	if ask.Provider != nil {
		record.Sort = ask.Provider.Sort
		record.Order, record.Only, record.Ignore = ask.Provider.Order, ask.Provider.Only, ask.Provider.Ignore
	}

	s.mu.Lock()
	s.asks = append(s.asks, record)
	lanes := s.models[ask.Model]
	clock := s.clock
	s.mu.Unlock()

	lane, found := pick(lanes, record)
	if !found {
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
	s.mu.Unlock()

	if lane.FailWith != 0 {
		writeError(w, lane.FailWith, "the lane refused", lane.Name)
		return
	}
	if !ask.Stream {
		s.serveWhole(w, r, clock, id, ask, lane, record)
		return
	}
	s.serveStream(w, r, clock, id, ask, lane, record)
}

// pick is the preference honoured: `only` is a demand, `ignore` is a veto, and
// `order` is a ranking among whatever survives both. With no preference at all
// the first lane declared answers, which is what makes a test's scripted order
// mean something.
func pick(lanes []Lane, ask Ask) (Lane, bool) {
	allowed := make([]Lane, 0, len(lanes))
	for _, lane := range lanes {
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
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, clock Clock, id string, ask wireAsk, lane Lane, record Ask) {
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

	for token := 0; token < total; token++ {
		if token > 0 {
			pause := gap
			if lane.StallAfter > 0 && token == lane.StallAfter {
				pause += lane.StallFor
			}
			if !clock.Wait(ctx, pause) {
				s.cancelled(lane.Name)
				return
			}
		}
		if !write("data: " + chunkJSON(id, ask.Model, lane.Name, fmt.Sprintf("t%d ", token)) + "\n\n") {
			s.cancelled(lane.Name)
			return
		}
	}
	if !write("data: " + finishJSON(id, ask.Model, lane.Name) + "\n\n") {
		s.cancelled(lane.Name)
		return
	}
	cost := lane.PriceIn*float64(record.PromptTokens) + lane.PriceOut*float64(total)
	if !write("data: " + usageJSON(id, ask.Model, lane.Name, record.PromptTokens, total, cost) + "\n\n") {
		s.cancelled(lane.Name)
		return
	}
	write("data: [DONE]\n\n")
}

// serveWhole answers a request that did not ask to stream. It exists so that
// nothing in this stub has to be special-cased by a caller that streams
// sometimes; the timings are honoured the same way.
func (s *Server) serveWhole(w http.ResponseWriter, r *http.Request, clock Clock, id string, ask wireAsk, lane Lane, record Ask) {
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
		"provider": lane.Name,
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
func writeError(w http.ResponseWriter, status int, message, lane string) {
	body := map[string]any{"error": map[string]any{"message": message, "code": status}}
	if lane != "" {
		body["provider"] = lane
		body["error"].(map[string]any)["metadata"] = map[string]any{"provider_name": lane}
	}
	writeJSON(w, status, body)
}
