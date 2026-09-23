//go:build !windows

package orclient

// One HTTP request, one stream.
//
// ── the four abort layers ─────────────────────────────────────────────────
//
// A model may stream useful reasoning for longer than ten minutes. Elapsed
// request age is therefore not evidence that it has stalled. senior-dev leaves the
// total-request deadline disabled by default and composes cancellation through
// the caller context plus a progress-sensitive reader watchdog:
//
//	callerCtx                                                    ← run lifetime
//	  └─ ctxTotal = callerCtx                                   ← default
//	       or context.WithTimeout(callerCtx, configured duration)  ← explicit opt-in
//	       └─ ctxChunk = context.WithCancelCause(ctxTotal)       ← inactivity
//	            └─ req = req.WithContext(ctxChunk)
//	                 └─ fetcher may derive its own context
//
//   - The caller context is the authoritative lifetime bound: the run can
//     cancel every request at once, and Stream.Close cancels an abandoned
//     request immediately.
//   - TotalTimeoutMS > 0 is an explicit operator override. Its cause remains
//     `errors.New("The operation timed out.")`, preserving timeout routing and
//     retry classification for configured deployments.
//   - The 120 s reader watchdog resets after every read, including reasoning
//     tokens and keepalives. It aborts only when transport progress stops, with
//     `errors.New("SSE read timed out")`.
//
// ── early teardown must cancel, not Close ────────────────────────────────
//
// Closing a response body does not by itself cancel an in-flight request, so
// `Stream.Close` cancels the request context FIRST and treats `Body.Close()`
// as best-effort cleanup. That is what lets a stream be abandoned mid-way
// (for example when compaction is needed) without leaking a connection.
//
// ── router registration ──────────────────────────────────────────────────
//
// The route lease taken for a request is settled exactly once: a finished or
// failed stream registers its outcome, a caller-cancelled stream releases the
// lease without health credit, and a stream abandoned through Close before it
// finished is released the same way so the router's in-flight count never
// leaks.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/retrysched"
	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

// Timeout defaults. Total request age is not a stall signal; only the
// progress-sensitive reader watchdog is enabled by default.
const (
	// DefaultTimeoutMS disables the total-request deadline. A positive
	// TotalTimeoutMS on Client opts back into one.
	DefaultTimeoutMS float64 = -1
	// DefaultChunkTimeoutMS is the reader watchdog's inactivity bound.
	DefaultChunkTimeoutMS float64 = 120_000
)

// Abort cause messages. Both are matched by `adaptive.IsLikelyTimeout` and
// `retrysched.IsTimeoutError`, which is the whole reason they are literals.
var (
	// ErrOperationTimedOut is the total-request deadline's cause.
	ErrOperationTimedOut = errors.New("The operation timed out.")
	// ErrSSEReadTimedOut is the reader watchdog's cause.
	ErrSSEReadTimedOut = errors.New("SSE read timed out")
)

// ── seams ─────────────────────────────────────────────────────────────────

// Fetcher performs one HTTP round trip. The default is a plain
// http.DefaultClient; the CLI installs its own configured client per Client.
type Fetcher func(req *http.Request) (*http.Response, error)

var fetcher Fetcher = http.DefaultClient.Do

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

// Timer / TimerFactory are the reader watchdog's timer seam, so tests can
// drive it on a virtual clock.
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

// RouterRegistrar is the narrow slice of *adaptive.AdaptiveModelRouter this
// package uses to settle a route lease.
type RouterRegistrar interface {
	Register(choice adaptive.RouteChoice, elapsedSeconds, completionTokens float64, err error) adaptive.AdaptiveRouteEvent
	RegisterCanceled(choice adaptive.RouteChoice)
}

// ── client ────────────────────────────────────────────────────────────────

// Client is one configured OpenRouter endpoint.
type Client struct {
	// BaseURL defaults to https://openrouter.ai/api/v1.
	BaseURL string
	// Headers is BuildHeaders' output.
	Headers []HeaderPair
	// Compatibility selects whether `stream_options` is emitted; senior-dev uses
	// "compatible".
	Compatibility string

	// TotalTimeoutMS / ChunkTimeoutMS control the optional total-request bound
	// and the progress-sensitive reader watchdog. Zero means the default; a
	// negative value disables the corresponding mechanism.
	TotalTimeoutMS float64
	ChunkTimeoutMS float64

	// Fetcher overrides the package fetch seam for this client only. Embedders
	// use it to retain their configured HTTP transport without mutating the
	// process-wide testing seam.
	Fetcher Fetcher

	// Router and RouteChoice drive the exactly-once registration. Both nil
	// means no routing was performed and registration is skipped entirely.
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

	ctx       context.Context
	callerCtx context.Context
	cancel    context.CancelCauseFunc
	stopTot   context.CancelFunc
	chunkMS   float64

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
	// release settles the route lease for a stream abandoned before it
	// finished: no success, failure or latency sample is attributed.
	release func()
}

// Response exposes the HTTP response (status + headers) for the error taxonomy
// the caller layers on top. The body is owned by the Stream.
func (s *Stream) Response() *http.Response { return s.response }

// DoStream issues the request and returns a Stream positioned before the first
// part, with the abort layers and the router bookkeeping installed.
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
		callerCtx:  ctx,
	}
	stream.register = func(completionTokens float64, failure error) {
		if c.Router == nil || c.RouteChoice == nil {
			return
		}
		if stream.callerCanceled(failure) {
			c.Router.RegisterCanceled(*c.RouteChoice)
			return
		}
		elapsed := (currentNow()() - routeStart) / 1000
		// Register invokes the configured event hook, so nothing is emitted here.
		c.Router.Register(*c.RouteChoice, elapsed, completionTokens, failure)
	}
	stream.release = func() {
		if c.Router != nil && c.RouteChoice != nil {
			c.Router.RegisterCanceled(*c.RouteChoice)
		}
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
		// BuildHeaders produced go out as-is; HTTP header names are
		// case-insensitive on the wire.
		req.Header[h.Name] = []string{h.Value}
	}

	// Start the inactivity clock before dispatch so a connection that never
	// produces response headers cannot occupy the rest of the run. Once headers
	// arrive, the same watchdog is reset and follows every response-body read.
	stream.armWatchdog()
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
		// Error classification is the caller's; this layer only has to make
		// the status classifiable and release the route.
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
	stream.bumpWatchdog()
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
	// Decode `{error:{message}}` and fall back to the status line. An empty
	// payload becomes "<status> (no body)", which the overflow classifier
	// recognises.
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
				// A mid-stream READ error surfaces as an `error` PART at
				// flush, never as a returned error.
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
					// An in-band error releases the route immediately. A
					// later finish/Close is suppressed by Once.
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
	if obj, err := ParseObject(raw); err == nil {
		if message, ok := obj.Get("message"); ok && rawString(message) != "" {
			return streamProviderError{message: rawString(message), body: string(raw)}
		}
		if data, ok := obj.Get("data"); ok {
			if nested, nestedErr := ParseObject(data); nestedErr == nil {
				if message, ok := nested.Get("message"); ok && rawString(message) != "" {
					return streamProviderError{message: rawString(message), body: string(raw)}
				}
			}
		}
	}
	if len(raw) == 0 {
		return errors.New("openrouter stream error")
	}
	return fmt.Errorf("openrouter stream error: %s", raw)
}

// streamProviderError is an in-band error payload from a model stream. The
// raw body is exposed as detail so the router classifiers can match provider
// fields such as error_type.
type streamProviderError struct {
	message string
	body    string
}

func (failure streamProviderError) Error() string       { return failure.message }
func (failure streamProviderError) ErrorDetail() string { return failure.body }

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

// finishRegister fires the exactly-once router registration with the stream's
// output token count (0 on failure).
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

// Close abandons the stream. It cancels the request context first and only
// then closes the body as best-effort cleanup. Safe to call from another
// goroutine, and safe to call twice.
func (s *Stream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		// Preserve the cancellation origin before teardown manufactures a
		// local Close cause. Closing a caller-aborted stream still owns its
		// lease even when Next did not drain the abort part.
		if cause := s.abortCause(); s.callerCanceled(cause) {
			s.finishRegister(cause)
		}
		s.closedByAPI.Store(true)
		s.teardown()
		err = s.closeBody()
		// A stream abandoned before it finished releases its route lease.
		s.registerOnce.Do(func() {
			if s.release != nil {
				s.release()
			}
		})
	})
	return err
}

func (s *Stream) callerCanceled(failure error) bool {
	if failure == nil || s.callerCtx == nil || s.callerCtx.Err() == nil || s.ctx == nil {
		return false
	}
	cause := context.Cause(s.callerCtx)
	// A child watchdog/provider timeout that won the cancellation race must
	// still count as a provider failure even if the parent cancels later.
	return errors.Is(context.Cause(s.ctx), cause) &&
		(errors.Is(failure, cause) || errors.Is(failure, s.callerCtx.Err()))
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
	})
	s.watchdogMu.Lock()
	if s.watchdogStopped.Load() || s.watchdogGeneration.Load() != generation {
		timer.Stop()
	} else {
		s.watchdog = timer
	}
	s.watchdogMu.Unlock()
}

// bumpWatchdog re-arms the per-read timer on every read, so a slow but
// progressing stream never trips it.
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
// or whatever the caller cancelled its own context with (layer 1).
//
// A caller-initiated Close is NOT a failure: post-Close reads report the end
// of the stream.
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
// body read, including comment-only keepalives and partial SSE
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

// readErrorValue renders a mid-stream reader error as `{name, message}` so the
// error part carries the reason.
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
