package orclient

// One HTTP request, one stream — ENGINE-DESIGN §3.6 and F1.
//
// ── the four abort layers ─────────────────────────────────────────────────
//
// TS stacks four abort mechanisms on every OpenRouter request. Go composes them
// through one context chain and one already-ported fetch seam:
//
//	callerCtx                                                    ← layer 1
//	  └─ ctxTotal = context.WithTimeout(callerCtx, 600s)         ← layer 2 total
//	       └─ ctxChunk = context.WithCancelCause(ctxTotal)       ← layers 2-chunk + 3
//	            └─ req = req.WithContext(ctxChunk)
//	                 └─ orfetch derives its own WithCancelCause  ← layer 4
//
//   - Layer 1 is codeaf's own AbortController (`llm.ts:513-517`), released by
//     the Effect scope. In Go it is simply the caller's context.
//   - Layer 2's 600 s (`provider.ts:1529-1541`, DEFAULT_TIMEOUT_MS) is a
//     WithTimeout whose CAUSE is `errors.New("The operation timed out.")` — the
//     literal message a `TimeoutError` DOMException carries. That string
//     contains "timed out", so `adaptive.IsLikelyTimeout` and
//     `retry.isTimeoutError` (both `/timeout|timed out|deadline exceeded/i`)
//     classify it as a timeout, which is what decides the 120 s router cooldown.
//     (ENGINE-DESIGN R4 asked whether the string matches. It does — via
//     "timed out", not "timeout".)
//   - Layers 2-chunk (120 s on the fetch wrapper) and 3 (`wrapSSE`'s per-read
//     timer) both mean "no SSE read for N ms". They COLLAPSE into one
//     reader-side watchdog whose cause is `errors.New("SSE read timed out")` —
//     the layer-3 message (`provider.ts:51`). BUGS-KEPT #8: TS could produce
//     either message here, Go produces one. Both match IsLikelyTimeout.
//   - Layer 4 needs nothing: `orfetch` reads `req.Context()` (`orfetch.go:425`)
//     and chains it, and it returns the abort REASON rather than
//     context.Canceled (`orfetch.go:459-461`), so what arrives here is already
//     router-classifiable. It is NOT wrapped with %w: the message bytes are the
//     contract.
//
// ── R2: early teardown must cancel, not Close ────────────────────────────
//
// `orfetch`'s `Body.Close()` neither closes upstream nor cancels the context
// (`orfetch.go:707-720`, BUGS-KEPT #7): it only clears the watchdogs, the eager
// pump keeps reading, and a late chunk RE-ARMS the timers that were just
// cleared. If no further chunk arrives the pump parks on the upstream read
// forever, holding the connection. So `Stream.Close` cancels the request context
// FIRST and treats `Body.Close()` as best-effort cleanup. This is the design
// change ENGINE-DESIGN R2 calls for, and it is what makes
// `Stream.takeUntil(() => ctx.needsCompaction)` (`processor.ts:702`) abandonable
// mid-stream without leaking a connection.
//
// ── router registration ──────────────────────────────────────────────────
//
// `registerRoute(completionTokens, error)` fires from the finish or error path
// (`llm.ts:139-146`). By default, an Effect-style interruption that reaches
// neither path does NOT register, so the router's in-flight slot leaks and
// permanently penalises the model through `pressurePen` (BUGS-KEPT #10), just
// as in TS. Setting CODEAF_GO_FIX_ROUTER_INFLIGHT_LEAK exactly to "1" opts into
// the Go fix: Close also registers, releasing the slot. The flag is read live
// on every Close call.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/calc"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/retrysched"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/logshim"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/orfetch"
)

var log = logshim.Create(map[string]any{"service": "orclient"})

// Timeout defaults, from `provider.ts:1529-1541`.
const (
	// DefaultTimeoutMS is `DEFAULT_TIMEOUT_MS` — layer 2's total-request bound.
	DefaultTimeoutMS float64 = 600_000
	// DefaultChunkTimeoutMS is `DEFAULT_CHUNK_TIMEOUT_MS` — layers 2-chunk and
	// 3, collapsed.
	DefaultChunkTimeoutMS float64 = 120_000
)

// Abort cause messages. Both are matched by `adaptive.IsLikelyTimeout` and
// `retrysched.IsTimeoutError`, which is the whole reason they are literals.
var (
	// ErrOperationTimedOut is `AbortSignal.timeout`'s DOMException message.
	ErrOperationTimedOut = errors.New("The operation timed out.")
	// ErrSSEReadTimedOut is `wrapSSE`'s abort reason (`provider.ts:51`).
	ErrSSEReadTimedOut = errors.New("SSE read timed out")
)

// ── seams ─────────────────────────────────────────────────────────────────

// Fetcher is the `orfetch.OpenRouterAimdFetch` seam (ENGINE-DESIGN F6).
type Fetcher func(req *http.Request) (*http.Response, error)

var fetcher Fetcher = orfetch.OpenRouterAimdFetch

// SetFetcherForTesting swaps the fetch seam. Returns a restore func.
func SetFetcherForTesting(f Fetcher) func() {
	seamMu.Lock()
	prev := fetcher
	fetcher = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		fetcher = prev
		seamMu.Unlock()
	}
}

func currentFetcher() Fetcher {
	seamMu.Lock()
	defer seamMu.Unlock()
	return fetcher
}

// Timer / TimerFactory mirror orfetch's setTimeout seam so the reader watchdog
// can be driven on the same virtual clock the orfetch tests already use.
type Timer interface{ Stop() }

type TimerFactory func(ms float64, fn func()) Timer

// TimeoutContextFactory mirrors context.WithTimeoutCause for the layer-2
// total-request deadline. Keeping this as a separate seam preserves the
// production context's real Deadline while allowing tests to fire the
// deadline without waiting on wall-clock time.
type TimeoutContextFactory func(context.Context, time.Duration, error) (context.Context, context.CancelFunc)

type realTimer struct{ t *time.Timer }

func (r realTimer) Stop() { r.t.Stop() }

var timerFactory TimerFactory = func(ms float64, fn func()) Timer {
	return realTimer{t: time.AfterFunc(time.Duration(ms)*time.Millisecond, fn)}
}

var timeoutContextFactory TimeoutContextFactory = context.WithTimeoutCause

// SetTimerFactoryForTesting swaps the watchdog timer. Returns a restore func.
func SetTimerFactoryForTesting(f TimerFactory) func() {
	seamMu.Lock()
	prev := timerFactory
	timerFactory = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		timerFactory = prev
		seamMu.Unlock()
	}
}

func currentTimerFactory() TimerFactory {
	seamMu.Lock()
	defer seamMu.Unlock()
	return timerFactory
}

// SetTimeoutContextFactoryForTesting swaps the total-request timeout seam.
// Returns a restore func.
func SetTimeoutContextFactoryForTesting(f TimeoutContextFactory) func() {
	seamMu.Lock()
	prev := timeoutContextFactory
	timeoutContextFactory = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		timeoutContextFactory = prev
		seamMu.Unlock()
	}
}

func currentTimeoutContextFactory() TimeoutContextFactory {
	seamMu.Lock()
	defer seamMu.Unlock()
	return timeoutContextFactory
}

// ── router ────────────────────────────────────────────────────────────────

// RouterRegistrar is the narrow slice of `*adaptive.AdaptiveModelRouter` this
// package uses. `Register` takes a `*adaptive.JSValue`, never a Go error,
// because adaptive.ts probes `unknown` with `instanceof Error`, bracket property
// access and JSON.stringify (ENGINE-DESIGN F8); the adapter is
// `retrysched.ToRouterValue`.
type RouterRegistrar interface {
	Register(choice adaptive.RouteChoice, elapsedSeconds, completionTokens float64, err *adaptive.JSValue) adaptive.AdaptiveRouteEvent
}

// ── client ────────────────────────────────────────────────────────────────

// Client is one configured OpenRouter endpoint.
type Client struct {
	// BaseURL defaults to https://openrouter.ai/api/v1 — `createOpenRouter`'s
	// `withoutTrailingSlash(baseURL ?? baseUrl) ?? "https://openrouter.ai/api/v1"`.
	BaseURL string
	// Headers is BuildHeaders' output.
	Headers []HeaderPair
	// Compatibility selects whether `stream_options` is emitted; codeaf gets
	// "compatible" (R11).
	Compatibility string

	// TotalTimeoutMS / ChunkTimeoutMS are abort layers 2 and 2-chunk/3.
	// Zero means the default; a negative value disables the layer.
	TotalTimeoutMS float64
	ChunkTimeoutMS float64

	// Fetcher overrides the package fetch seam for this client only. Embedders
	// use it to retain their configured HTTP transport without mutating the
	// process-wide testing seam.
	Fetcher Fetcher

	// Router and RouteChoice drive the exactly-once `register()`. Both nil
	// means no routing was performed (`routeChoice === undefined`, `llm.ts:140`)
	// and registration is skipped entirely.
	Router      RouterRegistrar
	RouteChoice *adaptive.RouteChoice
}

// Stream is one in-flight response. It is NOT safe for concurrent use; the one
// concurrency rule that matters is that Close may be called from another
// goroutine, which is exactly what an early teardown needs.
type Stream struct {
	parts []StreamPart
	next  int

	translator *Translator
	decoder    *SSEDecoder
	body       io.ReadCloser
	response   *http.Response

	ctx     context.Context
	cancel  context.CancelCauseFunc
	stopTot context.CancelFunc
	chunkMS float64

	closeOnce sync.Once
	bodyOnce  sync.Once
	bodyErr   error

	closedByAPI atomic.Bool

	watchdogMu         sync.Mutex
	watchdog           Timer
	watchdogGeneration atomic.Uint64
	watchdogStopped    atomic.Bool

	finished bool
	failed   error

	registerOnce sync.Once
	register     func(completionTokens float64, err error)
}

// Response exposes the HTTP response (status + headers) for the error taxonomy
// the caller layers on top. The body is owned by the Stream.
func (s *Stream) Response() *http.Response { return s.response }

// DoStream issues the request and returns a Stream positioned before the first
// part. It is `OpenRouterChatLanguageModel.doStream` plus codeaf's four abort
// layers plus the router bookkeeping.
func (c *Client) DoStream(ctx context.Context, params RequestParams) (*Stream, error) {
	if params.Compatibility == "" {
		params.Compatibility = c.Compatibility
	}
	body, err := BuildRequestBody(params)
	if err != nil {
		return nil, err
	}

	base := c.BaseURL
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	base = strings.TrimRight(base, "/")

	routeStart := currentNow()()
	stream := &Stream{
		translator: NewTranslator(),
		chunkMS:    c.chunkTimeout(),
	}
	stream.register = func(completionTokens float64, failure error) {
		if c.Router == nil || c.RouteChoice == nil {
			return
		}
		elapsed := (currentNow()() - routeStart) / 1000
		var value *adaptive.JSValue
		if failure != nil {
			value = retrysched.ToRouterValue(failure)
		}
		// `register()` invokes the configured on_event hook, so nothing is
		// emitted here (`llm.ts:141-142`).
		c.Router.Register(*c.RouteChoice, elapsed, completionTokens, value)
	}

	// Layer 2 total, then layers 2-chunk/3.
	ctxTotal := ctx
	if total := c.totalTimeout(); total > 0 {
		ctxTotal, stream.stopTot = currentTimeoutContextFactory()(ctx,
			time.Duration(total)*time.Millisecond, ErrOperationTimedOut)
	}
	ctxChunk, cancel := context.WithCancelCause(ctxTotal)
	stream.ctx = ctxChunk
	stream.cancel = cancel

	req, err := http.NewRequestWithContext(ctxChunk, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		stream.teardown()
		stream.registerOnce.Do(func() { stream.register(0, err) })
		return nil, err
	}
	for _, h := range c.Headers {
		// Assigned directly rather than through Set so the lowercase names
		// `withUserAgentSuffix` produced survive; HTTP header names are
		// case-insensitive on the wire, but the fixture set is lowercase.
		req.Header[h.Name] = []string{h.Value}
	}

	fetch := c.Fetcher
	if fetch == nil {
		fetch = currentFetcher()
	}
	resp, err := fetch(req)
	if err != nil {
		stream.teardown()
		stream.registerOnce.Do(func() { stream.register(0, err) })
		return nil, err
	}
	stream.response = resp
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The failed-response handler is the caller's taxonomy
		// (`openrouterFailedResponseHandler`); this layer only has to make the
		// status classifiable and release the route.
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		stream.teardown()
		body := string(payload)
		failure := retrysched.NewProviderError(
			statusMessage(resp, payload), float64(resp.StatusCode),
			retrysched.HeaderPairs(resp.Header), &body,
		)
		stream.registerOnce.Do(func() { stream.register(0, failure) })
		return nil, failure
	}
	if resp.Body == nil {
		stream.teardown()
		failure := errors.New("Empty response body")
		stream.registerOnce.Do(func() { stream.register(0, failure) })
		return nil, failure
	}

	stream.body = resp.Body
	stream.armWatchdog()
	stream.decoder = NewSSEDecoder(watchdogReader{stream: stream, body: resp.Body})
	return stream, nil
}

func (c *Client) totalTimeout() float64 {
	if c.TotalTimeoutMS == 0 {
		return DefaultTimeoutMS
	}
	return c.TotalTimeoutMS
}

func (c *Client) chunkTimeout() float64 {
	if c.ChunkTimeoutMS == 0 {
		return DefaultChunkTimeoutMS
	}
	return c.ChunkTimeoutMS
}

func statusMessage(resp *http.Response, payload []byte) string {
	// `openrouterFailedResponseHandler` decodes `{error:{message}}` and falls
	// back to `response.statusText`.
	// The provider/error overflow parser also recognizes the AI SDK's empty
	// APICallError form ("400 (no body)") for providers that omit a payload.
	if len(bytes.TrimSpace(payload)) == 0 {
		return fmt.Sprintf("%d (no body)", resp.StatusCode)
	}
	if chunk := ParseChunk(string(payload)); chunk.Success && chunk.Value != nil && chunk.Value.ErrorField != nil {
		if obj, err := ParseObject(chunk.Value.ErrorField); err == nil {
			if msg, ok := obj.Get("message"); ok {
				return rawString(msg)
			}
		}
	}
	if resp.Status != "" {
		return resp.Status
	}
	return fmt.Sprintf("%d", resp.StatusCode)
}

// ── the drain ─────────────────────────────────────────────────────────────

// Next returns the next stream part. It returns io.EOF exactly once, after the
// `finish` part.
//
// An abort — from any of the four layers — surfaces as `{type:"abort",
// reason:getErrorMessage(cause)}` and the stream then ends cleanly. The cause
// still goes to router registration, where its exact message is what the
// cooldown and retry classifiers substring-match.
func (s *Stream) Next() (StreamPart, error) {
	if s.next < len(s.parts) {
		p := s.parts[s.next]
		s.next++
		return p, nil
	}
	if s.failed != nil {
		return nil, s.failed
	}
	if s.finished {
		return nil, io.EOF
	}
	if s.closedByAPI.Load() {
		s.finished = true
		return nil, io.EOF
	}

	for {
		ev, err := s.decoder.Next()
		if err != nil {
			if s.closedByAPI.Load() {
				s.finished = true
				s.stopAndClose()
				return nil, io.EOF
			}
			if cause := s.abortCause(); cause != nil {
				s.parts = []StreamPart{AbortPart{Reason: cause.Error(), HasReason: true}}
				s.next = 1
				s.finished = true
				s.finishRegister(cause)
				s.stopAndClose()
				return s.parts[0], nil
			}
			var registerFailure error
			if err != io.EOF {
				// A mid-stream READ error is caught by withStreamErrorHandling
				// (`:2551-2573`) and surfaces as an `error` PART at flush —
				// never as a thrown exception.
				s.translator.SetStreamError(readErrorValue(err))
				registerFailure = err
			}
			s.parts = s.translator.Flush()
			s.finished = true
			s.finishRegister(registerFailure)
			s.stopAndClose()
			if len(s.parts) == 0 {
				return nil, io.EOF
			}
			s.next = 1
			return s.parts[0], nil
		}
		if ev.Data == DoneSentinel {
			continue
		}
		emitted, err := s.translator.Transform(ParseChunk(ev.Data))
		if len(emitted) > 0 {
			s.parts = emitted
			s.next = 1
			for _, part := range emitted {
				if streamError, ok := part.(ErrorPart); ok {
					// llm.ts:417-421: streamText's onError releases the route
					// immediately. A later finish/Close is suppressed by Once.
					s.finishRegister(errorPartFailure(streamError.Error))
					break
				}
			}
			if err != nil {
				// A throw tears the stream down: the parts already enqueued
				// are delivered, then the error.
				s.failed = err
				s.finishRegister(err)
				s.stopAndClose()
			}
			return s.parts[0], nil
		}
		if err != nil {
			s.failed = err
			s.finishRegister(err)
			s.stopAndClose()
			return nil, err
		}
	}
}

func errorPartFailure(raw []byte) error {
	routerValue := retrysched.JSONToRouterValue(raw)
	if obj, err := ParseObject(raw); err == nil {
		if message, ok := obj.Get("message"); ok && rawString(message) != "" {
			return streamProviderError{message: rawString(message), value: routerValue}
		}
		if data, ok := obj.Get("data"); ok {
			if nested, nestedErr := ParseObject(data); nestedErr == nil {
				if message, ok := nested.Get("message"); ok && rawString(message) != "" {
					return streamProviderError{message: rawString(message), value: routerValue}
				}
			}
		}
	}
	if len(raw) == 0 {
		return errors.New("openrouter stream error")
	}
	return fmt.Errorf("openrouter stream error: %s", raw)
}

type streamProviderError struct {
	message string
	value   *adaptive.JSValue
}

func (failure streamProviderError) Error() string { return failure.message }

func (failure streamProviderError) RouterValue() *adaptive.JSValue {
	return failure.value
}

// Parts drains the whole stream. Convenience for callers that do not need
// incremental delivery — and for tests.
func (s *Stream) Parts() ([]StreamPart, error) {
	var out []StreamPart
	for {
		p, err := s.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, p)
	}
}

// finishRegister fires the exactly-once router registration with the SDK's
// flattened `usage.outputTokens ?? 0` (`llm.ts:423-425`).
func (s *Stream) finishRegister(failure error) {
	s.registerOnce.Do(func() {
		completion := float64(0)
		if failure == nil {
			for _, p := range s.parts {
				finish, ok := p.(FinishPart)
				if !ok {
					continue
				}
				flat := calc.AsLanguageModelUsage(finish.Usage)
				if flat.OutputTokens != nil {
					completion = *flat.OutputTokens
				}
			}
		}
		s.register(completion, failure)
	})
}

// Close abandons the stream. It CANCELS THE REQUEST CONTEXT first — orfetch's
// Body.Close() alone would leave the eager pump reading (R2, BUGS-KEPT #7) —
// and only then closes the body as best-effort cleanup. Safe to call from
// another goroutine, and safe to call twice.
func (s *Stream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.closedByAPI.Store(true)
		s.teardown()
		err = s.closeBody()
		if os.Getenv("CODEAF_GO_FIX_ROUTER_INFLIGHT_LEAK") == "1" {
			// The opt-in fix records the abandoned stream as a SUCCESS with
			// zero completion tokens, matching the old unconditional cleanup.
			s.registerOnce.Do(func() { s.register(0, nil) })
		}
	})
	return err
}

func (s *Stream) teardown() {
	s.watchdogStopped.Store(true)
	s.watchdogGeneration.Add(1)
	s.watchdogMu.Lock()
	if s.watchdog != nil {
		s.watchdog.Stop()
		s.watchdog = nil
	}
	s.watchdogMu.Unlock()
	if s.cancel != nil {
		s.cancel(context.Canceled)
	}
	if s.stopTot != nil {
		s.stopTot()
	}
}

// armWatchdog starts the collapsed layer-2-chunk / layer-3 timer.
func (s *Stream) armWatchdog() {
	if s.chunkMS <= 0 || s.watchdogStopped.Load() {
		return
	}
	generation := s.watchdogGeneration.Add(1)
	timer := currentTimerFactory()(s.chunkMS, func() {
		if s.watchdogStopped.Load() || s.watchdogGeneration.Load() != generation {
			return
		}
		if s.cancel != nil {
			s.cancel(ErrSSEReadTimedOut)
		}
		log.Warn("sse read timed out")
	})
	s.watchdogMu.Lock()
	if s.watchdogStopped.Load() || s.watchdogGeneration.Load() != generation {
		timer.Stop()
	} else {
		s.watchdog = timer
	}
	s.watchdogMu.Unlock()
}

// bumpWatchdog re-arms the per-read timer. `wrapSSE` re-creates its timer on
// every read (`provider.ts:41-87`), so a slow but progressing stream never
// trips it.
func (s *Stream) bumpWatchdog() {
	if s.chunkMS <= 0 || s.watchdogStopped.Load() {
		return
	}
	s.watchdogMu.Lock()
	if s.watchdog != nil {
		s.watchdog.Stop()
		s.watchdog = nil
	}
	s.watchdogMu.Unlock()
	s.armWatchdog()
}

// abortCause resolves the context cause to the manufactured abort reason —
// `The operation timed out.` (layer 2), `SSE read timed out` (layers 2-chunk/3),
// or whatever the caller cancelled its own context with (layer 1). Layer 4's
// four messages never come through here: orfetch surfaces them as a READ error
// carrying the reason, which is why they are not wrapped.
//
// A caller-initiated Close is NOT a failure: post-Close reads resolve like a
// cancelled ReadableStream (`{done:true}`), so the stream just ends.
func (s *Stream) abortCause() error {
	if s.closedByAPI.Load() || s.ctx == nil || s.ctx.Err() == nil {
		return nil
	}
	cause := context.Cause(s.ctx)
	if cause == nil {
		return s.ctx.Err()
	}
	return cause
}

func (s *Stream) closeBody() error {
	s.bodyOnce.Do(func() {
		if s.body != nil {
			s.bodyErr = s.body.Close()
		}
	})
	return s.bodyErr
}

func (s *Stream) stopAndClose() {
	s.teardown()
	_ = s.closeBody()
}

// watchdogReader resets the collapsed layer-2/3 timer for every underlying
// ReadableStream read, including comment-only keepalives and partial SSE
// frames. Resetting only after a complete event would time out a healthy
// OpenRouter stream whose `: OPENROUTER PROCESSING` comments keep arriving.
type watchdogReader struct {
	stream *Stream
	body   io.Reader
}

func (r watchdogReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.stream.bumpWatchdog()
	}
	return n, err
}

// readErrorValue renders a mid-stream reader error the way
// `withStreamErrorHandling`'s captured value serialises: an Error object with
// only `name` and `message` enumerable is `{}` under JSON.stringify, so the
// shape recorded here carries the message deliberately — it is the only place
// the abort reason would otherwise be lost.
func readErrorValue(err error) []byte {
	w := newObjectWriter()
	w.str("name", "Error")
	w.str("message", err.Error())
	out, encodeErr := w.done()
	if encodeErr != nil {
		return []byte(`{"name":"Error"}`)
	}
	return out
}
