package orclient

// httptest-driven wire tests — ENGINE-DESIGN §7.2, the tier that has no TS
// oracle because it lives in the Go plumbing.
//
// Covered here: SSE framing against a real socket (CRLF, a frame split across
// two TCP writes, keepalive comments, `[DONE]`, a trailing frame with no
// terminating blank line), the collapsed layer-2/3 reader watchdog and its
// EXACT abort message, layer 2's total-request timeout and its EXACT abort
// message, the R2 early-teardown rule (cancel the context, do not merely close
// the body), goroutine cleanliness, and router registration exactly-once on
// success / failure / abandonment.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/retrysched"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
)

// ── helpers ───────────────────────────────────────────────────────────────

// sseServer serves a handler over httptest and returns a Client pointed at it.
// The fetch seam is swapped to a plain http.Client so the test exercises this
// package's own plumbing rather than orfetch's (which has its own suite).
func sseServer(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		// Some hermetic runners prohibit loopback sockets entirely. Keep the
		// tests as real httptest servers wherever sockets exist, and report a
		// capability skip rather than letting httptest panic in that sandbox.
		t.Skipf("loopback sockets unavailable: %v", err)
	}
	srv := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: handler},
	}
	srv.Start()
	httpClient := srv.Client()
	restore := SetFetcherForTesting(func(req *http.Request) (*http.Response, error) {
		return httpClient.Do(req)
	})
	c := &Client{BaseURL: srv.URL, Compatibility: CompatibilityCompatible}
	return c, func() {
		restore()
		srv.Close()
	}
}

func writeSSE(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for _, c := range chunks {
		_, _ = io.WriteString(w, c)
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func minimalParams() RequestParams {
	return RequestParams{ModelID: "deepseek/deepseek-v4-pro"}
}

func partTypes(parts []StreamPart) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.PartType())
	}
	return out
}

// ── framing ───────────────────────────────────────────────────────────────

func TestWireFramingOverHTTP(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{
			name:   "lf frames",
			chunks: []string{"data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n", "data: [DONE]\n\n"},
			want:   []string{PartTypeTextStart, PartTypeTextDelta, PartTypeTextEnd, PartTypeFinish},
		},
		{
			name:   "crlf frames",
			chunks: []string{"data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\r\n\r\n", "data: [DONE]\r\n\r\n"},
			want:   []string{PartTypeTextStart, PartTypeTextDelta, PartTypeTextEnd, PartTypeFinish},
		},
		{
			name: "frame split across two writes",
			chunks: []string{
				"data: {\"choices\":[{\"delta\":",
				"{\"content\":\"a\"}}]}\n\n",
				"data: [DONE]\n\n",
			},
			want: []string{PartTypeTextStart, PartTypeTextDelta, PartTypeTextEnd, PartTypeFinish},
		},
		{
			name: "keepalive comments are ignored",
			chunks: []string{
				": OPENROUTER PROCESSING\n\n",
				": OPENROUTER PROCESSING\n\n",
				"data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n",
				"data: [DONE]\n\n",
			},
			want: []string{PartTypeTextStart, PartTypeTextDelta, PartTypeTextEnd, PartTypeFinish},
		},
		{
			name:   "trailing frame with no blank line is still dispatched",
			chunks: []string{"data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}"},
			want:   []string{PartTypeTextStart, PartTypeTextDelta, PartTypeTextEnd, PartTypeFinish},
		},
		{
			name:   "multiple data lines in one frame join with a newline",
			chunks: []string{"data: {\"choices\":[{\"delta\":\ndata: {\"content\":\"a\"}}]}\n\n", "data: [DONE]\n\n"},
			want:   []string{PartTypeTextStart, PartTypeTextDelta, PartTypeTextEnd, PartTypeFinish},
		},
		{
			name:   "empty body yields only finish",
			chunks: []string{"data: [DONE]\n\n"},
			want:   []string{PartTypeFinish},
		},
		{
			name:   "malformed frame becomes an error part, not a failure",
			chunks: []string{"data: {not json\n\n", "data: [DONE]\n\n"},
			want:   []string{PartTypeError, PartTypeFinish},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeSSE(w, tc.chunks...)
			})
			defer cleanup()

			stream, err := c.DoStream(context.Background(), minimalParams())
			if err != nil {
				t.Fatalf("DoStream: %v", err)
			}
			defer stream.Close()

			parts, err := stream.Parts()
			if err != nil {
				t.Fatalf("drain: %v", err)
			}
			got := partTypes(parts)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("part types:\n want %v\n  got %v", tc.want, got)
			}
		})
	}
}

func TestWireRequestBodyAndHeadersReachTheServer(t *testing.T) {
	var gotBody []byte
	var gotHeader http.Header
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHeader = r.Header.Clone()
		writeSSE(w, "data: [DONE]\n\n")
	})
	defer cleanup()

	c.Headers = []HeaderPair{
		{Name: "authorization", Value: "Bearer KEY"},
		{Name: "content-type", Value: "application/json"},
		{Name: "x-session-affinity", Value: "ses_1"},
	}
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()
	if _, err := stream.Parts(); err != nil {
		t.Fatalf("drain: %v", err)
	}

	want := `{"model":"deepseek/deepseek-v4-pro","messages":[],"stream":true}`
	if string(gotBody) != want {
		t.Errorf("body:\n want %s\n  got %s", want, gotBody)
	}
	if got := gotHeader.Get("X-Session-Affinity"); got != "ses_1" {
		t.Errorf("x-session-affinity: got %q", got)
	}
	if got := gotHeader.Get("Authorization"); got != "Bearer KEY" {
		t.Errorf("authorization: got %q", got)
	}
}

func TestWireClientFetcherOverridesPackageSeam(t *testing.T) {
	packageCalls := 0
	restore := SetFetcherForTesting(func(*http.Request) (*http.Response, error) {
		packageCalls++
		return nil, errors.New("package fetcher should not run")
	})
	defer restore()

	clientCalls := 0
	client := &Client{
		BaseURL: "http://provider.invalid/api/v1",
		Fetcher: func(request *http.Request) (*http.Response, error) {
			clientCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
				Request:    request,
			}, nil
		},
	}
	stream, err := client.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Parts(); err != nil {
		t.Fatal(err)
	}
	if clientCalls != 1 || packageCalls != 0 {
		t.Fatalf("client fetches=%d package fetches=%d", clientCalls, packageCalls)
	}
}

// ── abort layers ──────────────────────────────────────────────────────────

func TestWireChunkWatchdogProducesTheLayer3Message(t *testing.T) {
	fire := make(chan func(), 1)
	restoreTimer := SetTimerFactoryForTesting(func(_ float64, fn func()) Timer {
		select {
		case fire <- fn:
		default:
		}
		return fakeTimer{}
	})
	defer restoreTimer()

	release := make(chan struct{})
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	defer cleanup()
	defer close(release)

	c.ChunkTimeoutMS = 30
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()

	(<-fire)()
	parts, err := stream.Parts()
	if err != nil {
		t.Fatalf("an abort must close the stream cleanly: %v", err)
	}
	abort := requireAbortPart(t, parts)
	if abort.Reason != "SSE read timed out" {
		t.Errorf("abort message: want %q, got %q", "SSE read timed out", abort.Reason)
	}
	// The text is the contract: it is what the router's cooldown class and
	// retry.ts's isTimeoutError match on.
	abortErr := errors.New(abort.Reason)
	if !adaptive.IsLikelyTimeout(retrysched.ToRouterValue(abortErr)) {
		t.Error("adaptive.IsLikelyTimeout must classify the collapsed layer-2/3 abort as a timeout")
	}
	message := abort.Reason
	if !retrysched.IsTimeoutError(retrysched.Err{Name: "APIError", Data: retrysched.ErrData{Message: &message}}) {
		t.Error("retrysched.IsTimeoutError must classify the collapsed layer-2/3 abort as a timeout")
	}
}

func TestWireTotalTimeoutProducesTheAbortSignalMessage(t *testing.T) {
	fire := make(chan context.CancelCauseFunc, 1)
	restoreTimeout := SetTimeoutContextFactoryForTesting(func(parent context.Context, _ time.Duration, cause error) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancelCause(parent)
		fire <- func(error) { cancel(cause) }
		return ctx, func() { cancel(context.Canceled) }
	})
	defer restoreTimeout()

	release := make(chan struct{})
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	defer cleanup()
	defer close(release)

	c.TotalTimeoutMS = 30
	c.ChunkTimeoutMS = -1 // disable the reader watchdog so layer 2 wins
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()

	(<-fire)(nil)
	parts, err := stream.Parts()
	if err != nil {
		t.Fatalf("an abort must close the stream cleanly: %v", err)
	}
	abort := requireAbortPart(t, parts)
	if abort.Reason != "The operation timed out." {
		t.Errorf("abort message: want %q, got %q", "The operation timed out.", abort.Reason)
	}
	// ENGINE-DESIGN R4: this string contains "timed out", not "timeout" — the
	// classifier still fires, which is the whole point of reproducing it.
	if !adaptive.IsLikelyTimeout(retrysched.ToRouterValue(errors.New(abort.Reason))) {
		t.Error("adaptive.IsLikelyTimeout must classify the layer-2 abort as a timeout")
	}
}

func TestWireCallerCancellationSurfacesItsOwnCause(t *testing.T) {
	release := make(chan struct{})
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	defer cleanup()
	defer close(release)

	c.ChunkTimeoutMS = -1
	ctx, cancel := context.WithCancelCause(context.Background())
	stream, err := c.DoStream(ctx, minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()

	callerCause := errors.New("caller gave up")
	cancel(callerCause)

	parts, err := stream.Parts()
	if err != nil {
		t.Fatalf("an abort must close the stream cleanly: %v", err)
	}
	abort := requireAbortPart(t, parts)
	if abort.Reason != callerCause.Error() {
		t.Fatalf("want the caller's own cause, got %q", abort.Reason)
	}
}

func requireAbortPart(t *testing.T, parts []StreamPart) AbortPart {
	t.Helper()
	for _, part := range parts {
		if abort, ok := part.(AbortPart); ok {
			return abort
		}
	}
	t.Fatalf("expected an abort part, got %v", partTypes(parts))
	return AbortPart{}
}

func TestWireCommentKeepalivesResetTheReadWatchdog(t *testing.T) {
	var arms int
	var stops int
	var stopsMu sync.Mutex
	restoreTimer := SetTimerFactoryForTesting(func(_ float64, _ func()) Timer {
		stopsMu.Lock()
		arms++
		stopsMu.Unlock()
		return fakeTimerFunc(func() {
			stopsMu.Lock()
			stops++
			stopsMu.Unlock()
		})
	})
	defer restoreTimer()

	restoreFetcher := SetFetcherForTesting(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: &chunkedBody{chunks: [][]byte{
				[]byte(": OPENROUTER PROCESSING\n\n"),
				[]byte(": OPENROUTER PROCESSING\n\n"),
				[]byte(": OPENROUTER PROCESSING\n\n"),
				[]byte(": OPENROUTER PROCESSING\n\n"),
				[]byte("data: [DONE]\n\n"),
			}},
		}, nil
	})
	defer restoreFetcher()

	c := &Client{Compatibility: CompatibilityCompatible}
	c.ChunkTimeoutMS = 30
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()

	parts, err := stream.Parts()
	if err != nil {
		t.Fatalf("keepalives must keep the read watchdog alive: %v", err)
	}
	for _, part := range parts {
		if part.PartType() == PartTypeAbort {
			t.Fatalf("keepalives must reset the underlying read timer, got %v", partTypes(parts))
		}
	}
	stopsMu.Lock()
	defer stopsMu.Unlock()
	if arms < 5 {
		t.Fatalf("watchdog armed %d times, want initial arm plus one per underlying read", arms)
	}
	if stops < 4 {
		t.Fatalf("watchdog stopped %d times, want at least one reset per keepalive", stops)
	}
}

// R2: early teardown must cancel the request context, not merely Close the
// body. The assertion is server-side — the handler must observe its request
// context finish — because orfetch's Body.Close() would leave the connection
// open (BUGS-KEPT #7).
func TestWireEarlyTeardownCancelsTheRequestContext(t *testing.T) {
	serverSawCancel := make(chan struct{})
	release := make(chan struct{})
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-r.Context().Done():
			close(serverSawCancel)
		case <-release:
		case <-time.After(5 * time.Second):
		}
	})
	defer cleanup()
	defer close(release)

	c.ChunkTimeoutMS = -1
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	// Read one part, then abandon — the `needsCompaction` shape.
	if _, err := stream.Next(); err != nil {
		t.Fatalf("first part: %v", err)
	}
	if err := stream.Close(); err != nil && err != io.EOF {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-serverSawCancel:
	case <-time.After(2 * time.Second):
		t.Fatal("Close must cancel the request context — the server never saw the request finish")
	}
}

func TestWireCloseIsIdempotentAndLeaksNoGoroutines(t *testing.T) {
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w,
			"data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n",
			"data: [DONE]\n\n",
		)
	})
	defer cleanup()

	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		stream, err := c.DoStream(context.Background(), minimalParams())
		if err != nil {
			t.Fatalf("DoStream: %v", err)
		}
		if _, err := stream.Parts(); err != nil {
			t.Fatalf("drain: %v", err)
		}
		_ = stream.Close()
		_ = stream.Close()
	}
	// Transport goroutine retirement is inherently scheduler-driven. Poll for
	// up to 2s (20x the old 100ms grace) so CPU starvation cannot turn a slow
	// cleanup into a false leak report.
	deadline := time.Now().Add(2 * time.Second)
	for {
		runtime.GC()
		after := runtime.NumGoroutine()
		if after <= before+10 {
			break
		}
		if time.Now().After(deadline) {
			t.Errorf("goroutine leak: before %d, after %d", before, after)
			break
		}
		runtime.Gosched()
	}
}

// ── HTTP error responses ──────────────────────────────────────────────────

func TestWireNon2xxBecomesAClassifiableStatusError(t *testing.T) {
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit exceeded","code":429}}`)
	})
	defer cleanup()

	_, err := c.DoStream(context.Background(), minimalParams())
	if err == nil {
		t.Fatal("expected a 429 to fail the request")
	}
	if err.Error() != "rate limit exceeded" {
		t.Errorf("message: want %q, got %q", "rate limit exceeded", err.Error())
	}
	value := retrysched.ToRouterValue(err)
	if !adaptive.IsLikelyRateLimit(value) {
		t.Error("a 429 must classify as a rate limit for the router")
	}
}

func TestErrorClassificationThroughRetryAdapter(t *testing.T) {
	type want struct {
		rateLimit, incompatible, timeout, structured, transient, retryable bool
	}
	cases := []struct {
		name string
		err  error
		want want
	}{
		{"layer 2 total timeout", ErrOperationTimedOut, want{timeout: true, retryable: true}},
		{"layer 3 read timeout", ErrSSEReadTimedOut, want{timeout: true, retryable: true}},
		{"http 429 status", retrysched.NewStatusError("request rejected", 429), want{rateLimit: true, retryable: true}},
		{"provider incompatibility", errors.New("unsupported parameter top_k"), want{incompatible: true, retryable: true}},
		{"structured failure", errors.New("invalid json parse"), want{structured: true, retryable: true}},
		{"http 503 status", retrysched.NewStatusError("request rejected", 503), want{transient: true, retryable: true}},
		{"ordinary application error", errors.New("tool execution failed"), want{}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			value := retrysched.ToRouterValue(tc.err)
			got := want{
				rateLimit:    adaptive.IsLikelyRateLimit(value),
				incompatible: adaptive.IsLikelyProviderIncompatible(value),
				timeout:      adaptive.IsLikelyTimeout(value),
				structured:   adaptive.IsLikelyStructuredFailure(value),
				transient:    adaptive.IsLikelyTransientProviderError(value),
				retryable:    adaptive.IsRetryableRouteError(value),
			}
			if got != tc.want {
				t.Errorf("classification:\n want %+v\n  got %+v", tc.want, got)
			}
		})
	}
}

// ── router registration exactly once ──────────────────────────────────────

type spyRouter struct {
	mu       sync.Mutex
	inflight int
	calls    []struct {
		completion float64
		err        *adaptive.JSValue
	}
}

func (s *spyRouter) Register(choice adaptive.RouteChoice, elapsedSeconds, completionTokens float64, err *adaptive.JSValue) adaptive.AdaptiveRouteEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, struct {
		completion float64
		err        *adaptive.JSValue
	}{completionTokens, err})
	if s.inflight > 0 {
		s.inflight--
	}
	return adaptive.AdaptiveRouteEvent{}
}

func (s *spyRouter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *spyRouter) inFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight
}

func TestWireRouterRegistersExactlyOnceOnSuccess(t *testing.T) {
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w,
			"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":7,\"total_tokens\":17}}\n\n",
			"data: [DONE]\n\n",
		)
	})
	defer cleanup()

	spy := &spyRouter{}
	c.Router = spy
	c.RouteChoice = &adaptive.RouteChoice{Slot: "planner"}

	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	if _, err := stream.Parts(); err != nil {
		t.Fatalf("drain: %v", err)
	}
	_ = stream.Close()

	if spy.count() != 1 {
		t.Fatalf("register must fire exactly once, got %d", spy.count())
	}
	if spy.calls[0].err != nil {
		t.Errorf("a successful stream must register a nil error")
	}
	// `registerRoute(usage?.outputTokens ?? 0, null)` — the SDK's flattened
	// outputTokens, i.e. the provider's completion_tokens.
	if spy.calls[0].completion != 7 {
		t.Errorf("completion tokens: want 7, got %v", spy.calls[0].completion)
	}
}

func TestWireRouterRegistersExactlyOnceOnHTTPFailure(t *testing.T) {
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	})
	defer cleanup()

	spy := &spyRouter{}
	c.Router = spy
	c.RouteChoice = &adaptive.RouteChoice{Slot: "planner"}

	if _, err := c.DoStream(context.Background(), minimalParams()); err == nil {
		t.Fatal("expected a 500 to fail")
	}
	if spy.count() != 1 {
		t.Fatalf("register must fire exactly once, got %d", spy.count())
	}
	if spy.calls[0].err == nil {
		t.Error("a failed request must register the error")
	}
}

func TestWireErrorPartRegistersFailureAndReleasesRouteInBothLeakModes(t *testing.T) {
	// Round-2 ErrorPart contract: streamText's onError registers a failure
	// exactly once before teardown. The abandoned-stream opt-in must not
	// overwrite that failure with its Close-time success registration.
	for _, flag := range []string{"", "1"} {
		t.Run("fix="+flag, func(t *testing.T) {
			t.Setenv("CODEAF_GO_FIX_ROUTER_INFLIGHT_LEAK", flag)
			c, cleanup := sseServer(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(w,
					"data: {\"error\":{\"message\":\"provider exploded\"},\"choices\":[]}\n\n",
					"data: [DONE]\n\n",
				)
			})
			defer cleanup()
			spy := &spyRouter{inflight: 1}
			c.Router = spy
			c.RouteChoice = &adaptive.RouteChoice{Slot: "coder"}
			stream, err := c.DoStream(context.Background(), minimalParams())
			if err != nil {
				t.Fatal(err)
			}
			parts, err := stream.Parts()
			if err != nil {
				t.Fatal(err)
			}
			_ = stream.Close()
			if len(parts) == 0 || parts[0].PartType() != PartTypeError {
				t.Fatalf("parts = %v", partTypes(parts))
			}
			if spy.count() != 1 || spy.inFlight() != 0 || spy.calls[0].err == nil ||
				spy.calls[0].completion != 0 {
				t.Fatalf("router calls=%#v inflight=%d", spy.calls, spy.inFlight())
			}
		})
	}
}

func TestWireErrorPartPreservesStatusForRouterCooldown(t *testing.T) {
	// Finding 4 contract: structured stream error fields reach adaptive.register,
	// so a numeric 503 is classified as transient and cools the route.
	router := adaptive.NewAdaptiveModelRouter(adaptive.AdaptiveRouterConfig{
		HighModels: []adaptive.ModelCandidate{
			{ID: "provider/first", Tier: adaptive.ModelTierHigh},
			{ID: "provider/second", Tier: adaptive.ModelTierHigh},
		},
	})
	choice, err := router.Pick("coder", adaptive.ModelTierHigh)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Fetcher: func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"error\":{\"message\":\"upstream unavailable\",\"status\":503},\"choices\":[]}\n\n" +
					"data: [DONE]\n\n",
			)),
			Request: request,
		}, nil
	}}
	c.Router = router
	c.RouteChoice = &choice
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Parts(); err != nil {
		t.Fatal(err)
	}
	next, err := router.Pick("coder", adaptive.ModelTierHigh)
	if err != nil {
		t.Fatal(err)
	}
	if next.Candidate.ID == choice.Candidate.ID {
		t.Fatalf("503 route %q was not cooled; next choice = %#v", choice.Candidate.ID, next)
	}
}

// BUGS-KEPT #10: TS leaks the in-flight slot when an Effect interruption stops
// both onFinish and onError firing. This remains the default.
func TestWireRouterLeaksOnAbandonedStreamByDefault(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_ROUTER_INFLIGHT_LEAK", "")

	release := make(chan struct{})
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	defer cleanup()
	defer close(release)

	spy := &spyRouter{inflight: 1}
	c.Router = spy
	c.RouteChoice = &adaptive.RouteChoice{Slot: "planner"}
	c.ChunkTimeoutMS = -1

	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	if _, err := stream.Next(); err != nil {
		t.Fatalf("first part: %v", err)
	}
	_ = stream.Close()

	if spy.count() != 0 {
		t.Fatalf("abandoned stream registered %d times, want 0", spy.count())
	}
	if spy.inFlight() != 1 {
		t.Fatalf("router in-flight counter = %d, want leaked value 1", spy.inFlight())
	}
}

func TestWireRouterLeakFixRegistersOnAbandonedStream(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_ROUTER_INFLIGHT_LEAK", "0")

	release := make(chan struct{})
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	defer cleanup()
	defer close(release)

	spy := &spyRouter{inflight: 1}
	c.Router = spy
	c.RouteChoice = &adaptive.RouteChoice{Slot: "planner"}
	c.ChunkTimeoutMS = -1

	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	if _, err := stream.Next(); err != nil {
		t.Fatalf("first part: %v", err)
	}
	// The flag is deliberately read live at cleanup, not cached when the
	// client or stream is created.
	t.Setenv("CODEAF_GO_FIX_ROUTER_INFLIGHT_LEAK", "1")
	_ = stream.Close()

	if spy.count() != 1 {
		t.Fatalf("leak fix must register once on an abandoned stream, got %d", spy.count())
	}
	if spy.inFlight() != 0 {
		t.Fatalf("router in-flight counter = %d, want released value 0", spy.inFlight())
	}
	if spy.calls[0].err != nil {
		t.Error("an abandoned stream registers a SUCCESS with zero tokens, not a failure")
	}
}

func TestWireNoRouteChoiceSkipsRegistration(t *testing.T) {
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, "data: [DONE]\n\n")
	})
	defer cleanup()

	spy := &spyRouter{}
	c.Router = spy // RouteChoice deliberately nil

	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	if _, err := stream.Parts(); err != nil {
		t.Fatalf("drain: %v", err)
	}
	_ = stream.Close()

	if spy.count() != 0 {
		t.Fatalf("`if (!routeChoice) return` — no registration without a pick, got %d", spy.count())
	}
}

// ── mid-stream read error ─────────────────────────────────────────────────

// withStreamErrorHandling catches a mid-stream reader error and the stream
// CLOSES cleanly; the error surfaces as an `error` PART at flush, never as a
// thrown exception. The httptest server aborts the connection mid-frame.
func TestWireMidStreamReadErrorBecomesAnErrorPart(t *testing.T) {
	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "512")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Returning early with an unsatisfied Content-Length makes the client
		// see an unexpected EOF.
	})
	defer cleanup()

	c.ChunkTimeoutMS = -1
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()

	parts, err := stream.Parts()
	if err != nil {
		t.Fatalf("a mid-stream read error must NOT be returned as an error: %v", err)
	}
	var sawError bool
	var finish *FinishPart
	for i := range parts {
		if parts[i].PartType() == PartTypeError {
			sawError = true
		}
		if f, ok := parts[i].(FinishPart); ok {
			finish = &f
		}
	}
	if !sawError {
		t.Errorf("expected an error part, got %v", partTypes(parts))
	}
	if finish == nil {
		t.Fatal("expected a finish part")
	}
	if finish.FinishReason.Unified != FinishError {
		t.Errorf("a captured stream error forces finishReason error, got %q", finish.FinishReason.Unified)
	}
}

// ── timer seam ────────────────────────────────────────────────────────────

func TestWireWatchdogUsesTheInjectableTimer(t *testing.T) {
	var mu sync.Mutex
	var armed []float64
	fire := make(chan func(), 8)
	restore := SetTimerFactoryForTesting(func(ms float64, fn func()) Timer {
		mu.Lock()
		armed = append(armed, ms)
		mu.Unlock()
		select {
		case fire <- fn:
		default:
		}
		return fakeTimer{}
	})
	defer restore()

	c, cleanup := sseServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, "data: [DONE]\n\n")
	})
	defer cleanup()

	c.ChunkTimeoutMS = 4321
	stream, err := c.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()
	if _, err := stream.Parts(); err != nil {
		t.Fatalf("drain: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(armed) == 0 {
		t.Fatal("the reader watchdog must go through the injectable timer")
	}
	for _, ms := range armed {
		if ms != 4321 {
			t.Errorf("watchdog armed with %v ms, want 4321", ms)
		}
	}
}

type fakeTimer struct{}

func (fakeTimer) Stop() {}

type fakeTimerFunc func()

func (f fakeTimerFunc) Stop() { f() }

// ── socket-free abort plumbing ────────────────────────────────────────────

type contextBody struct {
	ctx       context.Context
	closed    chan struct{}
	closeOnce sync.Once
}

type chunkedBody struct {
	chunks [][]byte
	next   int
}

func (b *chunkedBody) Read(p []byte) (int, error) {
	if b.next >= len(b.chunks) {
		return 0, io.EOF
	}
	chunk := b.chunks[b.next]
	b.next++
	return copy(p, chunk), nil
}

func (*chunkedBody) Close() error { return nil }

func newContextBody(ctx context.Context) *contextBody {
	return &contextBody{ctx: ctx, closed: make(chan struct{})}
}

func (b *contextBody) Read([]byte) (int, error) {
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-b.closed:
		return 0, io.EOF
	}
}

func (b *contextBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func TestClientTotalTimeoutEmitsAbortWithoutSocket(t *testing.T) {
	fire := make(chan context.CancelCauseFunc, 1)
	restoreTimeout := SetTimeoutContextFactoryForTesting(func(parent context.Context, _ time.Duration, cause error) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancelCause(parent)
		fire <- func(error) { cancel(cause) }
		return ctx, func() { cancel(context.Canceled) }
	})
	defer restoreTimeout()

	restore := SetFetcherForTesting(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       newContextBody(req.Context()),
			Header:     make(http.Header),
		}, nil
	})
	defer restore()

	client := &Client{TotalTimeoutMS: 5, ChunkTimeoutMS: -1}
	stream, err := client.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	defer stream.Close()
	(<-fire)(nil)
	parts, err := stream.Parts()
	if err != nil {
		t.Fatalf("abort must close cleanly: %v", err)
	}
	if got := requireAbortPart(t, parts).Reason; got != ErrOperationTimedOut.Error() {
		t.Errorf("want %q, got %q", ErrOperationTimedOut, got)
	}
}

func TestClientCloseCancelsRequestContextWithoutSocket(t *testing.T) {
	var requestContext context.Context
	restore := SetFetcherForTesting(func(req *http.Request) (*http.Response, error) {
		requestContext = req.Context()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       newContextBody(req.Context()),
			Header:     make(http.Header),
		}, nil
	})
	defer restore()

	client := &Client{TotalTimeoutMS: -1, ChunkTimeoutMS: -1}
	stream, err := client.DoStream(context.Background(), minimalParams())
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-requestContext.Done():
	default:
		t.Fatal("Close must cancel the HTTP request context")
	}
}

// ── the error-part payload of a mid-stream failure ────────────────────────

func TestErrorPartPayloadShape(t *testing.T) {
	got, err := ErrorPart{Error: readErrorValue(errors.New("connection reset"))}.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"error","error":{"name":"Error","message":"connection reset"}}`
	if string(got) != want {
		t.Errorf("want %s, got %s", want, got)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(got, &probe); err != nil {
		t.Fatalf("error part must be valid JSON: %v", err)
	}
}
