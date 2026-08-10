package orfetch

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Two suites live here:
//
//   - TestOpenRouterAimdFetch* — verbatim translations of
//     src/router/openrouter-fetch.test.ts, subtest names included. The TS
//     suite drives a hand-built streaming Response through a mocked global
//     fetch; the Go twin drives a real httptest SSE server over a real
//     net/http client, which is a strictly stronger test of the same
//     assertions.
//   - TestWire* — the wire semantics the fixtures cannot reach (real sockets,
//     real timers, real chunk boundaries): 429/5xx/low-remaining cap
//     adjustment, byte-idle and total-request aborts, pass-through fidelity,
//     caller cancellation, and dial failure.

// ── scaffolding ──────────────────────────────────────────────────────────

// newSSEServer starts an SSE endpoint whose body is driven by `write`. The
// handler must return when the request context is done so t.Cleanup's
// server.Close() (which waits for outstanding handlers) cannot hang.
func newSSEServer(t *testing.T, status int, headers map[string]string, write func(ctx context.Context, w io.Writer, flush func())) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets unavailable: %v", err)
	}
	server := &httptest.Server{
		Listener: listener,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			for name, value := range headers {
				w.Header().Set(name, value)
			}
			w.WriteHeader(status)
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			flusher.Flush()
			if write != nil {
				write(r.Context(), w, flusher.Flush)
			}
		})},
	}
	server.Start()
	t.Cleanup(server.Close)
	return server
}

// fetchAndDrain issues the wrapped fetch and reads the body to completion.
func fetchAndDrain(t *testing.T, ctx context.Context, url string) (*http.Response, string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := OpenRouterAimdFetch(req)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(response.Body)
	return response, string(body), readErr
}

// neutraliseOtherTimers is the Go twin of the TS suite's beforeEach: park the
// timers a case is not exercising so they can never fire first.
func neutraliseOtherTimers(t *testing.T) {
	t.Helper()
	t.Setenv("CODEAF_OPENROUTER_IDLE_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_CONTENT_IDLE_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_TOTAL_REQ_MS", "600000")
}

func recordLimiter(t *testing.T) *recordingLimiter {
	t.Helper()
	limiter := &recordingLimiter{}
	t.Cleanup(SetLimiterProvider(func() Limiter { return limiter }))
	return limiter
}

func virtualWatchdogTimers(t *testing.T) *opRecorder {
	t.Helper()
	recorder := &opRecorder{}
	t.Cleanup(SetTimerFactoryForTesting(recorder.factory))
	return recorder
}

// waitForTimerSets waits for asynchronous socket reads to reach the timer
// seam. Elapsed time is only a failure guard; success is gated by the exact
// number of virtual timer arms, not by sleeping for an assumed duration.
func waitForTimerSets(t *testing.T, recorder *opRecorder, ms float64, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := 0
		for _, timer := range recorder.timersSnapshot() {
			if timer.ms == ms {
				got++
			}
		}
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timer %vms armed %d times, want at least %d", ms, got, want)
		}
		runtime.Gosched()
	}
}

func openWrappedResponse(t *testing.T, ctx context.Context, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := OpenRouterAimdFetch(req)
	if err != nil {
		t.Fatalf("open wrapped response: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

// ── translated: openRouterAimdFetch: first-content deadline ──────────────

func TestOpenRouterAimdFetchFirstContentDeadline(t *testing.T) {
	t.Run("fires when only keepalives arrive (never a data: chunk)", func(t *testing.T) {
		neutraliseOtherTimers(t)
		t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "60")
		recordLimiter(t)
		recorder := virtualWatchdogTimers(t)
		server := newSSEServer(t, 200, nil, func(ctx context.Context, w io.Writer, flush func()) {
			_, _ = io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
			flush()
			<-ctx.Done()
		})

		response := openWrappedResponse(t, context.Background(), server.URL)
		// The second byte-idle arm proves the keepalive reached the eager pump;
		// it must not clear the first-content deadline.
		waitForTimerSets(t, recorder, 600000, 4)
		recorder.fireByMs(t, 60)
		_, err := io.ReadAll(response.Body)

		if err == nil {
			t.Fatalf("expected a first-content abort, got none")
		}
		if !strings.Contains(err.Error(), "first-content timeout") {
			t.Errorf("error %q does not contain %q", err.Error(), "first-content timeout")
		}
		if !isLikelyTimeoutMirror(err.Error()) {
			t.Errorf("router would NOT classify %q as a timeout", err.Error())
		}
	})

	t.Run("does NOT fire when a content chunk arrives promptly", func(t *testing.T) {
		neutraliseOtherTimers(t)
		t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "1000")
		recordLimiter(t)
		server := newSSEServer(t, 200, nil, func(ctx context.Context, w io.Writer, flush func()) {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
			flush()
		})

		_, text, err := fetchAndDrain(t, context.Background(), server.URL)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !strings.Contains(text, "hi") {
			t.Errorf("body %q does not contain %q", text, "hi")
		}
	})

	t.Run("CODEAF_OPENROUTER_FIRST_CONTENT_MS=0 disables the deadline", func(t *testing.T) {
		neutraliseOtherTimers(t)
		t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "0")
		recordLimiter(t)
		recorder := virtualWatchdogTimers(t)
		server := newSSEServer(t, 200, nil, func(ctx context.Context, w io.Writer, flush func()) {
			_, _ = io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
			flush()
			_, _ = io.WriteString(w, "data: late\n\n")
			flush()
		})

		_, text, err := fetchAndDrain(t, context.Background(), server.URL)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !strings.Contains(text, "data: late") {
			t.Errorf("body %q does not contain %q", text, "data: late")
		}
		for _, timer := range recorder.timersSnapshot() {
			if timer.ms == 0 {
				t.Fatal("disabled first-content deadline must not arm a timer")
			}
		}
	})
}

// ── translated: openRouterAimdFetch: content-idle ────────────────────────

func TestOpenRouterAimdFetchContentIdle(t *testing.T) {
	t.Run("default lowered to 90s", func(t *testing.T) {
		if CONTENT_IDLE_DEFAULT_MS != 90_000 {
			t.Errorf("CONTENT_IDLE_DEFAULT_MS = %v, want 90000", CONTENT_IDLE_DEFAULT_MS)
		}
	})

	t.Run("first-content default is 45s", func(t *testing.T) {
		if FIRST_CONTENT_DEFAULT_MS != 45_000 {
			t.Errorf("FIRST_CONTENT_DEFAULT_MS = %v, want 45000", FIRST_CONTENT_DEFAULT_MS)
		}
	})

	t.Run("env override is respected (fires after overridden window)", func(t *testing.T) {
		neutraliseOtherTimers(t)
		// Disable first-content so this case isolates the content-idle timer.
		t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "0")
		t.Setenv("CODEAF_OPENROUTER_CONTENT_IDLE_MS", "60")
		recordLimiter(t)
		recorder := virtualWatchdogTimers(t)
		// One real token up front (arms content-idle), then keepalives only.
		server := newSSEServer(t, 200, nil, func(ctx context.Context, w io.Writer, flush func()) {
			_, _ = io.WriteString(w, "data: first\n\n")
			flush()
			<-ctx.Done()
		})

		response := openWrappedResponse(t, context.Background(), server.URL)
		waitForTimerSets(t, recorder, 60, 2)
		recorder.fireByMs(t, 60)
		body, err := io.ReadAll(response.Body)
		text := string(body)

		if !strings.Contains(text, "first") {
			t.Errorf("body %q does not contain %q", text, "first")
		}
		if err == nil {
			t.Fatalf("expected a content-idle abort, got none")
		}
		if !strings.Contains(err.Error(), "content-idle timeout") {
			t.Errorf("error %q does not contain %q", err.Error(), "content-idle timeout")
		}
	})
}

// ── wire semantics ───────────────────────────────────────────────────────

func TestWireByteIdleAbortsAHungStream(t *testing.T) {
	t.Setenv("CODEAF_OPENROUTER_IDLE_MS", "60")
	t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_CONTENT_IDLE_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_TOTAL_REQ_MS", "600000")
	recordLimiter(t)
	recorder := virtualWatchdogTimers(t)
	// Headers, then silence: not a single byte, not even a keepalive.
	server := newSSEServer(t, 200, nil, func(ctx context.Context, w io.Writer, flush func()) {
		<-ctx.Done()
	})

	response := openWrappedResponse(t, context.Background(), server.URL)
	recorder.fireByMs(t, 60)
	_, err := io.ReadAll(response.Body)

	if err == nil {
		t.Fatalf("expected a byte-idle abort, got none")
	}
	want := "openrouter byte-idle timeout: no bytes for 60ms"
	if err.Error() != want {
		t.Errorf("error = %q, want exactly %q", err.Error(), want)
	}
}

func TestWireTotalRequestCapsAHealthyStream(t *testing.T) {
	t.Setenv("CODEAF_OPENROUTER_IDLE_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_CONTENT_IDLE_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_TOTAL_REQ_MS", "120")
	recordLimiter(t)
	recorder := virtualWatchdogTimers(t)
	// Content reaches the pump before the total cap is fired, proving the
	// other watchdogs remain healthy without relying on a ticker cadence.
	server := newSSEServer(t, 200, nil, func(ctx context.Context, w io.Writer, flush func()) {
		_, _ = io.WriteString(w, "data: tok\n\n")
		flush()
		<-ctx.Done()
	})

	response := openWrappedResponse(t, context.Background(), server.URL)
	waitForTimerSets(t, recorder, 600000, 5)
	recorder.fireByMs(t, 120)
	body, err := io.ReadAll(response.Body)
	text := string(body)

	if err == nil {
		t.Fatalf("expected a total-request abort, got none")
	}
	want := "openrouter total-request timeout: request exceeded 120ms wall-clock"
	if err.Error() != want {
		t.Errorf("error = %q, want exactly %q", err.Error(), want)
	}
	if !strings.Contains(text, "data: tok") {
		t.Errorf("tokens that arrived before the cap should still reach the caller, got %q", text)
	}
}

func TestWire429AdjustsTheCapAndPassesTheResponseThrough(t *testing.T) {
	limiter := recordLimiter(t)
	server := newSSEServer(t, http.StatusTooManyRequests, map[string]string{
		"X-RateLimit-Limit":     "100",
		"X-RateLimit-Remaining": "0",
		"Retry-After":           "30",
	}, func(ctx context.Context, w io.Writer, flush func()) {
		_, _ = io.WriteString(w, "{\"error\":\"rate limited\"}")
	})

	response, text, err := fetchAndDrain(t, context.Background(), server.URL)

	if err != nil {
		t.Fatalf("429 must surface as a normal response, got error %v", err)
	}
	if response.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", response.StatusCode)
	}
	if got := response.Header.Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After header = %q, want %q", got, "30")
	}
	if text != "{\"error\":\"rate limited\"}" {
		t.Errorf("body = %q, want the upstream bytes verbatim", text)
	}
	assertLimiterCalls(t, limiter, []string{"acquire", "onThrottle", "release"})
}

func TestWire5xxLeavesTheCapAlone(t *testing.T) {
	limiter := recordLimiter(t)
	server := newSSEServer(t, http.StatusInternalServerError, nil, func(ctx context.Context, w io.Writer, flush func()) {
		_, _ = io.WriteString(w, "boom")
	})

	response, _, err := fetchAndDrain(t, context.Background(), server.URL)

	if err != nil {
		t.Fatalf("5xx must surface as a normal response, got error %v", err)
	}
	if response.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", response.StatusCode)
	}
	assertLimiterCalls(t, limiter, []string{"acquire", "release"})
}

func TestWireLowRemainingDecelerates(t *testing.T) {
	limiter := recordLimiter(t)
	server := newSSEServer(t, 200, map[string]string{
		"X-RateLimit-Limit":     "100",
		"X-RateLimit-Remaining": "7",
	}, func(ctx context.Context, w io.Writer, flush func()) {
		_, _ = io.WriteString(w, "data: ok\n\n")
	})

	if _, _, err := fetchAndDrain(t, context.Background(), server.URL); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLimiterCalls(t, limiter, []string{"acquire", "onLowRemaining", "release"})
}

func TestWireHealthyResponseBumpsTheCapAndPreservesBytes(t *testing.T) {
	limiter := recordLimiter(t)
	const payload = "data: {\"delta\":\"héllo — 世界\"}\n\n: OPENROUTER PROCESSING\n\ndata: [DONE]\n\n"
	server := newSSEServer(t, 200, map[string]string{"X-Request-Id": "req_abc"},
		func(ctx context.Context, w io.Writer, flush func()) {
			_, _ = io.WriteString(w, payload)
		})

	response, text, err := fetchAndDrain(t, context.Background(), server.URL)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != payload {
		t.Errorf("body = %q, want %q", text, payload)
	}
	if got := response.Header.Get("X-Request-Id"); got != "req_abc" {
		t.Errorf("headers must pass through unchanged, X-Request-Id = %q", got)
	}
	if response.StatusCode != 200 {
		t.Errorf("status = %d, want 200", response.StatusCode)
	}
	assertLimiterCalls(t, limiter, []string{"acquire", "onSuccess", "release"})
}

func TestWireCallerCancellationPropagates(t *testing.T) {
	t.Setenv("CODEAF_OPENROUTER_IDLE_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_CONTENT_IDLE_MS", "600000")
	t.Setenv("CODEAF_OPENROUTER_TOTAL_REQ_MS", "600000")
	recordLimiter(t)
	server := newSSEServer(t, 200, nil, func(ctx context.Context, w io.Writer, flush func()) {
		<-ctx.Done()
	})

	ctx, cancel := context.WithCancelCause(context.Background())
	callerReason := errors.New("user cancelled the session")
	defer cancel(callerReason)

	response := openWrappedResponse(t, ctx, server.URL)
	cancel(callerReason)
	_, err := io.ReadAll(response.Body)

	if err == nil {
		t.Fatalf("expected the caller's cancellation to surface, got none")
	}
	// The caller's own reason, not a manufactured timeout and not Go's
	// context.Canceled wrapper — same as TS, where fetch rejects with exactly
	// what was handed to controller.abort().
	if err.Error() != callerReason.Error() {
		t.Errorf("error = %q, want %q", err.Error(), callerReason.Error())
	}
	if isLikelyTimeoutMirror(err.Error()) {
		t.Errorf("a user cancellation must NOT be classified as a timeout")
	}
}

func TestWirePreAbortedCallerContextIsThrottleEquivalent(t *testing.T) {
	limiter := recordLimiter(t)
	server := newSSEServer(t, 200, nil, nil)

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("user cancelled the session"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := OpenRouterAimdFetch(req)
	if err == nil {
		response.Body.Close()
		t.Fatalf("expected the pre-aborted signal to fail the fetch")
	}
	if err.Error() != "user cancelled the session" {
		t.Errorf("error = %q, want the caller's own reason", err.Error())
	}
	// TS treats every throw as throttle-equivalent so it doesn't pile more
	// requests into a failing pipe — including a caller cancellation.
	assertLimiterCalls(t, limiter, []string{"acquire", "onThrottle", "release"})
}

func TestWireDialFailureIsThrottleEquivalent(t *testing.T) {
	limiter := recordLimiter(t)
	// Bind then immediately close so the port is dead.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets unavailable: %v", err)
	}
	url := "http://" + listener.Addr().String()
	_ = listener.Close()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := OpenRouterAimdFetch(req)
	if err == nil {
		response.Body.Close()
		t.Fatalf("expected a dial failure")
	}
	assertLimiterCalls(t, limiter, []string{"acquire", "onThrottle", "release"})
}

func TestWireNoBodyResponseReturnsUpstreamUnchanged(t *testing.T) {
	limiter := recordLimiter(t)
	server := newSSEServer(t, http.StatusNoContent, nil, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := OpenRouterAimdFetch(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", response.StatusCode)
	}
	if _, isWrapped := response.Body.(*wrappedBody); isWrapped {
		t.Errorf("a body-less response must be returned unwrapped, like the TS early return")
	}
	assertLimiterCalls(t, limiter, []string{"acquire", "onSuccess", "release"})
}

func assertLimiterCalls(t *testing.T, limiter *recordingLimiter, want []string) {
	t.Helper()
	got := limiter.snapshot()
	if len(got) != len(want) {
		t.Fatalf("limiter calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("limiter calls = %v, want %v", got, want)
		}
	}
}

// ── kept quirks, asserted in Go terms ────────────────────────────────────

func TestKeptTSQuirks(t *testing.T) {
	t.Run("consumer cancel never reaches upstream, so a later chunk re-arms the timers", func(t *testing.T) {
		// openrouter-fetch.ts:271 calls upstream.cancel(reason) on a stream
		// the pump's reader still locks, which always throws — so the pump
		// survives the cancel. Fixtures
		// cancel/chunk-after-cancel-rearms-then-pump-dies and
		// cancel/keepalive-after-cancel-rearms-byte-only pin the transcript;
		// this asserts the Go twin has the same shape.
		recorder := &opRecorder{}
		t.Cleanup(SetTimerFactoryForTesting(recorder.factory))
		t.Cleanup(SetLimiterProvider(func() Limiter { return &recordingLimiter{} }))
		t.Setenv("CODEAF_OPENROUTER_IDLE_MS", "101")
		t.Setenv("CODEAF_OPENROUTER_FIRST_CONTENT_MS", "102")
		t.Setenv("CODEAF_OPENROUTER_CONTENT_IDLE_MS", "103")
		t.Setenv("CODEAF_OPENROUTER_TOTAL_REQ_MS", "104")

		doer := &fakeDoer{plan: planSpec{Status: 200, StatusText: "OK"}}
		t.Cleanup(SetDoerForTesting(doer))

		req, err := http.NewRequest(http.MethodGet, "https://openrouter.ai/x", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		response, err := OpenRouterAimdFetch(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		before := len(recorder.snapshot())

		if err := response.Body.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		afterCancel := recorder.snapshot()
		if len(afterCancel) <= before {
			t.Fatalf("cancel should have cleared the timers")
		}

		// A chunk that arrives after the cancel still re-arms byte-idle.
		doer.body.push("data: zombie\n\n")
		awaitSignal(t, doer.body.closed, "post-cancel pump shutdown")
		var rearmed bool
		for _, op := range recorder.snapshot()[len(afterCancel):] {
			if op.Op == "set" {
				rearmed = true
			}
		}
		if !rearmed {
			t.Errorf("post-cancel chunk did not re-arm a timer — the TS bug is not reproduced")
		}
	})

	t.Run("a second Close is a no-op because the stream already left readable", func(t *testing.T) {
		recorder := &opRecorder{}
		t.Cleanup(SetTimerFactoryForTesting(recorder.factory))
		t.Cleanup(SetLimiterProvider(func() Limiter { return &recordingLimiter{} }))
		doer := &fakeDoer{plan: planSpec{Status: 200, StatusText: "OK"}}
		t.Cleanup(SetDoerForTesting(doer))

		req, err := http.NewRequest(http.MethodGet, "https://openrouter.ai/x", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		response, err := OpenRouterAimdFetch(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_ = response.Body.Close()
		afterFirst := len(recorder.snapshot())
		_ = response.Body.Close()
		if got := len(recorder.snapshot()); got != afterFirst {
			t.Errorf("second Close emitted %d extra timer ops, want 0", got-afterFirst)
		}
	})
}

func TestFixOrfetchCancelPumpExitsAfterClose(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_ORFETCH_CANCEL", "1")

	recorder := &opRecorder{}
	t.Cleanup(SetTimerFactoryForTesting(recorder.factory))
	t.Cleanup(SetLimiterProvider(func() Limiter { return &recordingLimiter{} }))

	doer := &fakeDoer{plan: planSpec{Status: 200, StatusText: "OK"}}
	t.Cleanup(SetDoerForTesting(doer))

	req, err := http.NewRequest(http.MethodGet, "https://openrouter.ai/x", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := OpenRouterAimdFetch(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The pump is now parked on upstream.Read, waiting for a chunk or EOF
	// that will never arrive. Close the body — with the fix the derived
	// context is cancelled, which unblocks the pump's Read and the pump exits.
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// When the pump exits it calls upstream.Close(), which signals
	// fakeBody.closed. If the fix is working this must happen promptly.
	awaitSignal(t, doer.body.closed, "pump shutdown after body close")

	// No timer re-arm may happen after the close.
	opsAfter := recorder.snapshot()
	var setsAfterClose int
	for _, op := range opsAfter {
		if op.Op == "set" {
			setsAfterClose++
		}
	}
	// The four initial arms (byte, first-content, content, total) plus zero
	// re-arms after close.
	if setsAfterClose != 4 {
		t.Errorf("expected 4 timer sets (initial arms only), got %d", setsAfterClose)
	}
}
