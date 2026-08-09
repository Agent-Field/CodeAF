// Package orfetch is a bug-for-bug port of src/router/openrouter-fetch.ts —
// the custom fetch that wraps every OpenRouter HTTP call with the AIMD
// limiter and four nested timeout watchdogs.
//
// Why this module matters more than its 296 TS lines suggest: the four abort
// reasons it manufactures are *strings the router classifies on*. adaptive.ts
// `isLikelyTimeout` lowercases the error text and substring-matches
// ["timeout", "timed out", "deadline exceeded"]; a hit puts the model on a
// 120s cooldown and the next router.pick() switches model. So every byte of
// every message below is load-bearing, and testdata/fixtures.json pins each
// message together with the classification the REAL adaptive.ts gives it.
//
// ── seams (things TS gets from the runtime that Go has to be handed) ──────
//
//   - setTimeout/clearTimeout → timerFactory / Timer. Injectable so the
//     fixture replay can run a virtual clock and compare the exact
//     set/clear transcript against the one the TS module produced.
//   - global fetch → Doer (an http.Client-shaped interface).
//   - AbortController → context.WithCancelCause. The abort *reason* is the
//     cause, and every error this package returns is the reason itself (not
//     Go's context.Canceled wrapper) so the router sees the same text fetch
//     rejects with in TS.
//   - Date.now() is reached only by the Retry-After HTTP-date branch, which
//     lives in internal/router/ratehead (ratehead.SetNowMSForTesting).
//
// ── sibling ports ─────────────────────────────────────────────────────────
//
// openrouter-fetch.ts imports two sibling modules, both ported separately:
// aimd-limiter.ts is internal/router/aimdlimiter and
// openrouter-rate-headers.ts is internal/router/ratehead. This package
// consumes them through a narrow Limiter interface (the five methods the
// fetch wrapper calls) so tests can record the call sequence;
// *aimdlimiter.AIMDConcurrencyLimiter satisfies it as-is.
//
// testdata/fixtures.json independently pins parseRateLimitHeaders /
// isLowRemaining against the REAL TS module, so the two Go ports of that
// module cross-check each other on every run.
//
// ── fidelity notes (deliberate, do not "fix") ─────────────────────────────
//
//   - TS relies on the single-threaded event loop for mutual exclusion
//     between the four timer callbacks and the stream pump. Go has real
//     concurrency, so watchdogs carries a mutex. The OBSERVABLE contract —
//     which handle is live, in what order set/clear happen — is unchanged.
//   - clearAll() does NOT null out byteTimer/contentTimer/totalTimer (only
//     clearFirstContent nulls its handle), so a second clearAll re-issues
//     clearTimeout on the same handles. Reproduced: a double Close() emits
//     the same duplicate clears TS does.
//   - The stream pump is EAGER, like the TS `start(ctrl)` loop: it pulls from
//     upstream as fast as upstream yields and buffers into a channel, so the
//     byte-idle timer tracks *arrival*, not consumption. A plain io.Reader
//     wrapper would have tied it to the consumer and changed when the
//     watchdogs fire.
//   - chunkHasContent is a per-chunk substring scan in both implementations,
//     so a `data:` token split across two transport chunks is missed by
//     both. Chunk boundaries themselves are transport-defined and differ
//     between Bun and net/http — that difference is inherent to the port, not
//     a behaviour change.
//   - `new Response(wrapped, {status, statusText, headers})` drops url /
//     redirected / type; the Go twin likewise returns a fresh *http.Response
//     carrying only Status/StatusCode/Header/Body (+ Proto and Request for
//     net/http's own sanity).
package orfetch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/fixflag"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/logshim"
	"github.com/Agent-Field/swe-pro-go/internal/router/aimdlimiter"
	"github.com/Agent-Field/swe-pro-go/internal/router/ratehead"
)

var log = logshim.Create(map[string]any{"service": "openrouter-fetch"})

// ── timeout defaults ──────────────────────────────────────────────────────
//
// FIRST_CONTENT_DEFAULT_MS and CONTENT_IDLE_DEFAULT_MS are `export const` in
// TS (openrouter-fetch.test.ts asserts both), so they stay exported and keep
// the TS spelling. The other two are module-private in TS and stay private
// here.

const byteIdleDefaultMs float64 = 60_000

// FIRST_CONTENT_DEFAULT_MS bounds the head-of-stream gap: headers are in and
// keepalive comments may be flowing, but the model has emitted no `data:`
// chunk at all.
const FIRST_CONTENT_DEFAULT_MS float64 = 45_000

// CONTENT_IDLE_DEFAULT_MS was lowered from 5min to 90s (W7c) because
// keepalive-only "wedges" sat for minutes before the router failed over.
const CONTENT_IDLE_DEFAULT_MS float64 = 90_000

const totalReqDefaultMs float64 = 15 * 60_000

// envMsMax is the shared upper bound (30 minutes) every knob is clamped to.
const envMsMax float64 = 1_800_000

// ── injectable seams ──────────────────────────────────────────────────────

// Timer mirrors the opaque handle setTimeout returns. TS only ever tests it
// for truthiness and hands it to clearTimeout, so Stop() is the whole
// contract and a nil Timer is TS's `undefined`.
type Timer interface{ Stop() }

// TimerFactory mirrors global setTimeout(fn, ms). ms is a float64 because the
// env knobs go through JS Number(): "60.5" is a reachable, accepted value.
type TimerFactory func(ms float64, fn func()) Timer

type realTimer struct{ t *time.Timer }

func (r realTimer) Stop() { r.t.Stop() }

var timerFactory TimerFactory = func(ms float64, fn func()) Timer {
	return realTimer{t: time.AfterFunc(time.Duration(ms*float64(time.Millisecond)), fn)}
}

// SetTimerFactoryForTesting swaps the setTimeout seam. Returns a restore func.
func SetTimerFactoryForTesting(f TimerFactory) func() {
	prev := timerFactory
	timerFactory = f
	return func() { timerFactory = prev }
}

// Doer is the global-fetch seam.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

var httpDoer Doer = http.DefaultClient

// SetDoerForTesting swaps the HTTP client. Returns a restore func.
func SetDoerForTesting(d Doer) func() {
	prev := httpDoer
	httpDoer = d
	return func() { httpDoer = prev }
}

// Date.now() is reached only by the Retry-After HTTP-date branch, which lives
// in internal/router/ratehead — pin it with ratehead.SetNowMSForTesting.

// ── env knobs ─────────────────────────────────────────────────────────────

// envNumber is jscompat.ToNumber with two JS rules re-imposed that Go's
// strconv quietly relaxes:
//
//   - numeric separators. strconv.ParseFloat accepts "1_000" (valid Go
//     literal syntax); JS Number("1_000") is NaN, because separators are legal
//     only in source literals, never in string coercion. Caught by fixture
//     env/byte-idle-numeric-separator-falls-back.
//   - Go's spellings of infinity. ParseFloat accepts "inf"/"infinity"/"nan"
//     case-insensitively; JS accepts only the exact "Infinity" (with an
//     optional sign) and NaN for everything else.
//
// jscompat is off-limits to this port, so — same pattern as
// internal/session/cochange's sliceTopK workaround — the guard lives here.
// Any caller of jscompat.ToNumber on untrusted text has the same latent bug.
func envNumber(raw string) float64 {
	trimmed := jscompat.Trim(raw)
	if strings.ContainsRune(trimmed, '_') {
		return math.NaN()
	}
	unsigned := strings.TrimPrefix(strings.TrimPrefix(trimmed, "+"), "-")
	switch strings.ToLower(unsigned) {
	case "inf", "infinity", "nan":
		if unsigned != "Infinity" {
			return math.NaN()
		}
	}
	return jscompat.ToNumber(trimmed)
}

// envMs mirrors envMs(). Note the TS guard is `if (!raw) return def`: an unset
// var and an empty-string var are both falsy, so both fall back — and so does
// the literal string "0", which parses to 0 and then fails `n < 1`.
func envMs(name string, def float64) float64 {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	n := envNumber(raw)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > envMsMax {
		return def
	}
	return n
}

func byteIdleMs() float64 {
	return envMs("CODEAF_OPENROUTER_IDLE_MS", byteIdleDefaultMs)
}

func contentIdleMs() float64 {
	return envMs("CODEAF_OPENROUTER_CONTENT_IDLE_MS", CONTENT_IDLE_DEFAULT_MS)
}

func totalReqMs() float64 {
	return envMs("CODEAF_OPENROUTER_TOTAL_REQ_MS", totalReqDefaultMs)
}

// firstContentMs mirrors firstContentMs(). It cannot reuse envMs because 0
// means "disabled" here while envMs rejects 0 as out-of-range. Note `n === 0`
// is true for -0 as well, so CODEAF_OPENROUTER_FIRST_CONTENT_MS="-0" disables
// the deadline while "-1" falls back to the default.
func firstContentMs() float64 {
	raw, ok := os.LookupEnv("CODEAF_OPENROUTER_FIRST_CONTENT_MS")
	if !ok || raw == "" {
		return FIRST_CONTENT_DEFAULT_MS
	}
	n := envNumber(raw)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return FIRST_CONTENT_DEFAULT_MS
	}
	if n == 0 {
		return 0 // explicitly disabled
	}
	if n < 1 || n > envMsMax {
		return FIRST_CONTENT_DEFAULT_MS
	}
	return n
}

// ── SSE chunk classifier ──────────────────────────────────────────────────

var (
	dataPrefix   = []byte("data:")
	nlDataPrefix = []byte("\ndata:")
)

// chunkHasContent mirrors chunkHasContent(). The TS decodes the chunk as
// latin-1 (one byte -> one UTF-16 code unit) purely to dodge UTF-8 boundary
// surprises, then substring-matches two ASCII tokens; matching the raw bytes
// is byte-for-byte equivalent and skips the copy.
//
// Consequences kept: a chunk whose `data:` token starts mid-line (" data:")
// is keepalive, an uppercase "DATA:" is keepalive, but a CRLF-terminated
// stream still matches because "\r\ndata:" contains "\ndata:".
func chunkHasContent(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if bytes.HasPrefix(b, dataPrefix) {
		return true
	}
	return bytes.Contains(b, nlDataPrefix)
}

// ── abort reasons ─────────────────────────────────────────────────────────
//
// These four strings are the module's real public contract: adaptive.ts
// classifies on their text. `${ms}` is a JS template-literal interpolation,
// i.e. String(n), so it goes through jscompat.FormatNumber — "60.5ms", not
// "60.500000ms", and "1000ms" for the env value "1e3".

func byteIdleTimeoutMessage(ms float64) string {
	return "openrouter byte-idle timeout: no bytes for " + jscompat.FormatNumber(ms) + "ms"
}

func firstContentTimeoutMessage(ms float64) string {
	return "openrouter first-content timeout after " + jscompat.FormatNumber(ms) + "ms"
}

func contentIdleTimeoutMessage(ms float64) string {
	return "openrouter content-idle timeout: no data: chunks for " + jscompat.FormatNumber(ms) + "ms (keepalives only)"
}

func totalRequestTimeoutMessage(ms float64) string {
	return "openrouter total-request timeout: request exceeded " + jscompat.FormatNumber(ms) + "ms wall-clock"
}

// ── the four watchdogs ────────────────────────────────────────────────────

type watchdogs struct {
	mu sync.Mutex

	byteMs    float64
	firstMs   float64
	contentMs float64
	totalMs   float64

	byteTimer         Timer
	firstContentTimer Timer
	contentTimer      Timer
	totalTimer        Timer

	cancel  context.CancelCauseFunc
	aborted bool
	reason  error
}

// abort mirrors controller.abort(reason): first reason wins, later aborts are
// no-ops (AbortController keeps signal.reason from the first abort).
func (w *watchdogs) abort(reason error) {
	w.mu.Lock()
	if w.aborted {
		w.mu.Unlock()
		return
	}
	w.aborted = true
	w.reason = reason
	w.mu.Unlock()
	w.cancel(reason)
}

func (w *watchdogs) abortReason() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reason
}

func (w *watchdogs) clearFirstContent() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.firstContentTimer != nil {
		w.firstContentTimer.Stop()
		w.firstContentTimer = nil
	}
}

// clearAll mirrors clearAll(). Only firstContentTimer is nulled — the other
// three handles survive, so calling clearAll twice issues clearTimeout twice
// on the same handles. Kept.
func (w *watchdogs) clearAll() {
	w.mu.Lock()
	if w.byteTimer != nil {
		w.byteTimer.Stop()
	}
	first := w.firstContentTimer
	w.firstContentTimer = nil
	if first != nil {
		first.Stop()
	}
	if w.contentTimer != nil {
		w.contentTimer.Stop()
	}
	if w.totalTimer != nil {
		w.totalTimer.Stop()
	}
	w.mu.Unlock()
}

func (w *watchdogs) bumpByte() {
	w.mu.Lock()
	if w.byteTimer != nil {
		w.byteTimer.Stop()
	}
	ms := w.byteMs
	w.byteTimer = timerFactory(ms, func() {
		log.Warn("byte-idle timeout — aborting hung stream", map[string]any{"byteMs": ms})
		w.abort(errors.New(byteIdleTimeoutMessage(ms)))
	})
	w.mu.Unlock()
}

func (w *watchdogs) bumpContent() {
	w.mu.Lock()
	if w.contentTimer != nil {
		w.contentTimer.Stop()
	}
	ms := w.contentMs
	w.contentTimer = timerFactory(ms, func() {
		log.Warn("content-idle timeout — aborting (keepalives only, no data)", map[string]any{"contentMs": ms})
		w.abort(errors.New(contentIdleTimeoutMessage(ms)))
	})
	w.mu.Unlock()
}

// armFirstContent is called once at dispatch and never re-armed; firstMs <= 0
// disables the deadline entirely, leaving the handle nil forever.
func (w *watchdogs) armFirstContent() {
	if w.firstMs <= 0 {
		return
	}
	w.mu.Lock()
	ms := w.firstMs
	w.firstContentTimer = timerFactory(ms, func() {
		log.Warn("first-content timeout — aborting stream with no first token", map[string]any{"firstMs": ms})
		w.abort(errors.New(firstContentTimeoutMessage(ms)))
	})
	w.mu.Unlock()
}

func (w *watchdogs) armTotal() {
	w.mu.Lock()
	ms := w.totalMs
	w.totalTimer = timerFactory(ms, func() {
		log.Warn("total-request timeout — aborting overlong response", map[string]any{"totalMs": ms})
		w.abort(errors.New(totalRequestTimeoutMessage(ms)))
	})
	w.mu.Unlock()
}

// ── the wrapper ───────────────────────────────────────────────────────────

// errBodyReleased is the cancel cause used when the wrapped body is closed
// without an abort. TS has no analogue (a JS AbortController that is never
// aborted simply stays pending); Go must cancel the derived context or the
// transport connection and the caller-signal watcher leak.
var errBodyReleased = errors.New("openrouter response body closed")

// OpenRouterAimdFetch mirrors `openRouterAimdFetch(input, init)`. The TS
// signature's two halves collapse into *http.Request: input is the URL/method
// /headers/body, and `init.signal` is req.Context().
//
// Errors are returned as the abort *reason* (the manufactured timeout Error,
// or the caller's own cancellation cause) rather than Go's context.Canceled
// wrapper, matching TS where fetch rejects with exactly what was handed to
// controller.abort().
func OpenRouterAimdFetch(req *http.Request) (*http.Response, error) {
	limiter := limiterProvider()
	<-limiter.Acquire()
	// TS `finally { limiter.release() }` runs when the async function settles
	// — i.e. once headers are in and the wrapped Response is built, NOT when
	// the body finishes draining. defer has exactly that timing.
	defer limiter.Release()

	callerCtx := req.Context()
	ctx, cancel := context.WithCancelCause(callerCtx)

	w := &watchdogs{
		byteMs:    byteIdleMs(),
		firstMs:   firstContentMs(),
		contentMs: contentIdleMs(),
		totalMs:   totalReqMs(),
		cancel:    cancel,
	}

	// Chain the caller's signal. TS checks `aborted` first and otherwise
	// registers a once-listener; the goroutine is that listener, and it
	// retires when the derived context is done.
	if callerCtx.Err() != nil {
		w.abort(context.Cause(callerCtx))
	} else {
		go func() {
			select {
			case <-callerCtx.Done():
				w.abort(context.Cause(callerCtx))
			case <-ctx.Done():
			}
		}()
	}

	w.bumpByte()
	w.armFirstContent()
	w.bumpContent()
	w.armTotal()

	response, err := httpDoer.Do(req.WithContext(ctx))
	if err != nil {
		w.clearAll()
		if reason := w.abortReason(); reason != nil {
			err = reason
		}
		cancel(err)
		// Network failures, aborts, etc. — treat as throttle-equivalent so we
		// don't pile more requests into a failing pipe.
		log.Warn("fetch threw — treating as throttle", map[string]any{"error": sliceErrorText(err)})
		limiter.OnThrottle()
		return nil, err
	}

	// For streaming responses the rate-limit headers are on the initial
	// response object, so the cap can be adjusted without buffering the body.
	rate := ratehead.ParseRateLimitHeaders(response.Header)

	if response.StatusCode == 429 {
		log.Warn("429 throttle", map[string]any{
			"retryAfter": rateField(rate, func(r *ratehead.RateLimitInfo) *jscompat.JSNumber { return r.RetryAfterSeconds }),
			"remaining":  rateField(rate, func(r *ratehead.RateLimitInfo) *jscompat.JSNumber { return r.Remaining }),
			"limit":      rateField(rate, func(r *ratehead.RateLimitInfo) *jscompat.JSNumber { return r.Limit }),
		})
		limiter.OnThrottle()
	} else if response.StatusCode >= 200 && response.StatusCode < 300 {
		if ratehead.IsLowRemaining(rate) {
			log.Info("low remaining — decelerating", map[string]any{
				"remaining": rateField(rate, func(r *ratehead.RateLimitInfo) *jscompat.JSNumber { return r.Remaining }),
				"limit":     rateField(rate, func(r *ratehead.RateLimitInfo) *jscompat.JSNumber { return r.Limit }),
			})
			limiter.OnLowRemaining()
		} else {
			limiter.OnSuccess()
		}
	}
	// 4xx/5xx non-429: do not adjust the cap. These are application errors
	// (bad request, server error) unrelated to rate.

	if hasBody(response) {
		wrapped := newWrappedBody(response.Body, w)
		// Same headers/status so the consumer sees an indistinguishable
		// response object.
		return &http.Response{
			Status:        response.Status,
			StatusCode:    response.StatusCode,
			Proto:         response.Proto,
			ProtoMajor:    response.ProtoMajor,
			ProtoMinor:    response.ProtoMinor,
			Header:        response.Header,
			Body:          wrapped,
			ContentLength: response.ContentLength,
			Request:       response.Request,
		}, nil
	}

	w.clearAll()
	cancel(errBodyReleased)
	return response, nil
}

// hasBody mirrors `if (response.body)`. net/http never leaves Body nil on a
// client response; the "no body" shape (204/304/HEAD) arrives as
// http.NoBody instead.
func hasBody(response *http.Response) bool {
	return response.Body != nil && response.Body != http.NoBody
}

// sliceErrorText mirrors `String(err).slice(0, 200)`. Log-only — logshim
// discards it — but kept so the call shape stays honest. JS slices UTF-16
// code units; runes are close enough for a discarded log field.
func sliceErrorText(err error) string {
	text := "Error: " + err.Error()
	runes := []rune(text)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return text
}

// rateField renders one optional rate-limit field for a log line: TS reads it
// as `rate?.remaining`, which is undefined for both a missing info object and
// a missing field.
func rateField(info *ratehead.RateLimitInfo, pick func(*ratehead.RateLimitInfo) *jscompat.JSNumber) any {
	if info == nil {
		return nil
	}
	if value := pick(info); value != nil {
		return *value
	}
	return nil
}

// ── wrapped body ──────────────────────────────────────────────────────────

// readBufferSize is the pump's per-Read buffer. Sized so one transport flush
// (an SSE event or a keepalive comment) lands as one chunk, which is what
// chunkHasContent classifies over.
const readBufferSize = 32 * 1024

// wrappedBody is the Go twin of the ReadableStream the TS builds around the
// upstream body: an EAGER pump goroutine resets the byte-idle timer on every
// arriving chunk and the content-idle timer on every chunk carrying a `data:`
// line, buffering into a channel the consumer drains.
type wrappedBody struct {
	upstream io.ReadCloser
	w        *watchdogs

	chunks chan []byte
	closed chan struct{}

	// stateMu guards `settled`, the Go stand-in for the ReadableStream's
	// state leaving "readable". reader.cancel() only runs the underlying
	// cancel algorithm while the stream is still readable — on an already
	// closed or errored stream it is a no-op — so clearAll() must not fire a
	// second time once the pump has finished.
	stateMu sync.Mutex
	settled bool

	pending []byte

	errMu   sync.Mutex
	pumpErr error
}

// settle claims the "stream leaves the readable state" transition. Returns
// false if something already claimed it.
func (b *wrappedBody) settle() bool {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	if b.settled {
		return false
	}
	b.settled = true
	return true
}

func newWrappedBody(upstream io.ReadCloser, w *watchdogs) *wrappedBody {
	b := &wrappedBody{
		upstream: upstream,
		w:        w,
		chunks:   make(chan []byte),
		closed:   make(chan struct{}),
	}
	go b.pump()
	return b
}

func (b *wrappedBody) pump() {
	buf := make([]byte, readBufferSize)
	for {
		n, err := b.upstream.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			b.w.bumpByte()
			if chunkHasContent(chunk) {
				// First real token: retire the first-content deadline. Same
				// `data:` detection the content-idle timer uses, so keepalive
				// comments never satisfy it.
				b.w.clearFirstContent()
				b.w.bumpContent()
			}
			select {
			case b.chunks <- chunk:
			case <-b.closed:
				// The wrapped stream is already closed, so TS's
				// `ctrl.enqueue(value)` throws — and the pump's own catch
				// runs clearAll() a SECOND time before dying. Note the bumps
				// above already happened: a chunk that arrives after a
				// consumer cancel re-arms the timers clearAll just cleared.
				b.w.clearAll()
				_ = b.upstream.Close()
				return
			}
		}
		if err != nil {
			b.settle()
			b.w.clearAll()
			if err != io.EOF {
				// The abort reason, when there is one, is what TS's stream
				// errors with — not the transport's context.Canceled wrapper.
				if reason := b.w.abortReason(); reason != nil {
					err = reason
				}
				b.setPumpErr(err)
			}
			close(b.chunks)
			// Invisible to every observable: the pump has stopped, so no
			// further chunk, timer op or byte can be produced. Closing here
			// hands the connection back instead of parking it until GC.
			_ = b.upstream.Close()
			return
		}
	}
}

func (b *wrappedBody) setPumpErr(err error) {
	b.errMu.Lock()
	b.pumpErr = err
	b.errMu.Unlock()
}

func (b *wrappedBody) finalErr() error {
	b.errMu.Lock()
	defer b.errMu.Unlock()
	if b.pumpErr != nil {
		return b.pumpErr
	}
	return io.EOF
}

func (b *wrappedBody) Read(p []byte) (int, error) {
	for len(b.pending) == 0 {
		select {
		case chunk, ok := <-b.chunks:
			if !ok {
				return 0, b.finalErr()
			}
			b.pending = chunk
		case <-b.closed:
			// A cancelled ReadableStream resolves its reader's pending read
			// with {done: true}, not an error — so a post-Close read is EOF.
			return 0, io.EOF
		}
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

// Close mirrors the stream's `cancel(reason)`: clearAll(), then
// `return upstream.cancel(reason)`.
//
// SUSPECTED TS BUG KEPT (openrouter-fetch.ts:269-272). That second call is
// made on a ReadableStream whose reader the pump still holds, so it ALWAYS
// throws `TypeError: This ReadableStream is locked` — the upstream body is
// never cancelled, and the pump lives on. Two consequences, both pinned by
// fixtures cancel/chunk-after-cancel-rearms-then-pump-dies and
// cancel/keepalive-after-cancel-rearms-byte-only:
//
//   - the next chunk to arrive after a cancel RE-ARMS the byte-idle (and, if
//     it carries `data:`, the content-idle) timer that clearAll just cleared,
//     and only the failing enqueue afterwards tears the pump down;
//   - if no further chunk ever arrives, the pump stays parked on the upstream
//     read forever, holding the connection.
//
// Reproduced literally: Close() touches the timers and the wrapped stream and
// nothing else — it neither closes upstream nor cancels the derived context,
// exactly like the TS. (TS also leaves the AbortController un-aborted on this
// path, so the underlying request survives there too.)
func (b *wrappedBody) Close() error {
	if !b.settle() {
		// Already closed or errored — reader.cancel() is a no-op there, so no
		// second clearAll. Pinned by cancel/body-cancelled-after-timer-abort.
		return nil
	}
	b.w.clearAll()
	if fixflag.Enabled("CODEAF_GO_FIX_ORFETCH_CANCEL") {
		// Cancel the derived context so the pump goroutine unblocks from the
		// upstream read and exits, releasing the connection. Without this the
		// pump parks forever on Read — a goroutine + socket leak (BUGS-KEPT.md
		// "orfetch" first entry). Timer clear behaviour is unchanged.
		b.w.cancel(errBodyReleased)
	}
	close(b.closed)
	return nil
}

// ── sibling-port wiring ───────────────────────────────────────────────────

// Limiter is the slice of aimdlimiter.AIMDConcurrencyLimiter that
// openrouter-fetch.ts uses. Acquire returns a channel because the TS awaits a
// Promise — an already-closed channel is the resolved-immediately case.
type Limiter interface {
	Acquire() <-chan struct{}
	Release()
	OnSuccess()
	OnThrottle()
	OnLowRemaining()
}

// limiterProvider mirrors getOpenRouterLimiter() — the process singleton the
// sibling port owns.
var limiterProvider = func() Limiter { return aimdlimiter.GetOpenRouterLimiter() }

// SetLimiterProvider swaps getOpenRouterLimiter(). Tests use it to record the
// call sequence without touching the real singleton. Returns a restore func.
func SetLimiterProvider(p func() Limiter) func() {
	prev := limiterProvider
	limiterProvider = p
	return func() { limiterProvider = prev }
}

// compile-time proof the sibling port satisfies the seam as-is.
var _ Limiter = (*aimdlimiter.AIMDConcurrencyLimiter)(nil)
