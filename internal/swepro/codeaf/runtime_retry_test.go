package codeaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type retryCapture struct {
	mu    sync.Mutex
	logs  []string
	waits []time.Duration
}

func (c *retryCapture) logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, fmt.Sprintf(format, args...))
}

func (c *retryCapture) sleep(_ context.Context, duration time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, duration)
	return nil
}

func (c *retryCapture) snapshot() ([]string, []time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.logs...), append([]time.Duration(nil), c.waits...)
}

func retryTurn() turn {
	return turn{ModelID: "test/model", Prompt: "hello"}
}

func successResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprint(w, chatReply("ok", 1))
}

func errorResponse(w http.ResponseWriter, message string, status int) {
	encoded, _ := json.Marshal(map[string]any{"error": map[string]any{"message": message}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func newRuntimeRetryServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets unavailable: %v", err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	server.Start()
	return server
}

func TestOpenRouterBackendRetries429ThenSucceeds(t *testing.T) {
	// Contract 1: 429 followed by 200 retries once and honors retry-after-ms.
	requests := 0
	server := newRuntimeRetryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After-Ms", "17")
			errorResponse(w, "slow down", http.StatusTooManyRequests)
			return
		}
		successResponse(w)
	}))
	defer server.Close()

	capture := &retryCapture{}
	backend := &openRouterBackend{
		apiKey: "test", client: server.Client(), endpoint: server.URL,
		logf: capture.logf, sleep: capture.sleep,
	}
	result, err := backend.Run(context.Background(), retryTurn())
	if err != nil || result.Text != "ok" {
		t.Fatalf("Run = (%+v, %v), want success", result, err)
	}
	logs, waits := capture.snapshot()
	if requests != 2 || len(waits) != 1 || waits[0] != 17*time.Millisecond {
		t.Fatalf("requests=%d waits=%v, want 2 and [17ms]", requests, waits)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "attempt=1") ||
		!strings.Contains(logs[0], "slow down") || !strings.Contains(logs[0], "delay_ms=17") {
		t.Fatalf("retry logs = %q", logs)
	}
}

func TestOpenRouterBackendFloorsZeroProviderDelay(t *testing.T) {
	// Finding 5: an explicit Retry-After-Ms: 0 yields between unbounded
	// flag-off attempts instead of spinning hot.
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		response := &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
			Body: io.NopCloser(strings.NewReader(chatReply("ok", 1))),
		}
		if requests == 1 {
			response.StatusCode = http.StatusTooManyRequests
			response.Header.Set("Retry-After-Ms", "0")
			response.Body = io.NopCloser(strings.NewReader(`{"error":{"message":"slow down"}}`))
		}
		return response, nil
	})}
	capture := &retryCapture{}
	backend := &openRouterBackend{
		apiKey: "test", client: client, endpoint: "http://provider.invalid",
		logf: capture.logf, sleep: capture.sleep,
	}
	if _, err := backend.Run(context.Background(), retryTurn()); err != nil {
		t.Fatal(err)
	}
	_, waits := capture.snapshot()
	if len(waits) != 1 || waits[0] < 250*time.Millisecond {
		t.Fatalf("waits = %v, want provider delay floor >=250ms", waits)
	}
}

func TestOpenRouterBackendRetriesTwo500sAndLogsReasons(t *testing.T) {
	// Contract 2: two 500s succeed on the third request and log both retries.
	requests := 0
	server := newRuntimeRetryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests < 3 {
			errorResponse(w, fmt.Sprintf("failure-%d", requests), http.StatusInternalServerError)
			return
		}
		successResponse(w)
	}))
	defer server.Close()

	capture := &retryCapture{}
	backend := &openRouterBackend{
		apiKey: "test", client: server.Client(), endpoint: server.URL,
		logf: capture.logf, sleep: capture.sleep,
	}
	if _, err := backend.Run(context.Background(), retryTurn()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	logs, waits := capture.snapshot()
	if requests != 3 || len(logs) != 2 || len(waits) != 2 {
		t.Fatalf("requests=%d logs=%q waits=%v", requests, logs, waits)
	}
	for i, want := range []string{"failure-1", "failure-2"} {
		if !strings.Contains(logs[i], fmt.Sprintf("attempt=%d", i+1)) || !strings.Contains(logs[i], want) {
			t.Errorf("log[%d] = %q", i, logs[i])
		}
	}
}

func TestOpenRouterBackendDoesNotRetryClientErrors(t *testing.T) {
	// Contract 3: OpenRouter 400/401/404 responses are not retryable.
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := newRuntimeRetryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				errorResponse(w, "client failure", status)
			}))
			defer server.Close()
			capture := &retryCapture{}
			backend := &openRouterBackend{
				apiKey: "test", client: server.Client(), endpoint: server.URL,
				logf: capture.logf, sleep: capture.sleep,
			}
			_, err := backend.Run(context.Background(), retryTurn())
			logs, waits := capture.snapshot()
			if err == nil || requests != 1 || len(logs) != 0 || len(waits) != 0 {
				t.Fatalf("err=%v requests=%d logs=%q waits=%v", err, requests, logs, waits)
			}
		})
	}
}

func TestOpenRouterBackendAttemptCapExhaustion(t *testing.T) {
	// Contract 4: the opt-in attempt-cap fix surfaces the eleventh failure.
	t.Setenv("CODEAF_GO_FIX_RETRY_ATTEMPT_CAP", "1")
	requests := 0
	server := newRuntimeRetryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		errorResponse(w, fmt.Sprintf("still-failing-%d", requests), http.StatusBadGateway)
	}))
	defer server.Close()
	capture := &retryCapture{}
	backend := &openRouterBackend{
		apiKey: "test", client: server.Client(), endpoint: server.URL,
		logf: capture.logf, sleep: capture.sleep,
	}
	_, err := backend.Run(context.Background(), retryTurn())
	logs, waits := capture.snapshot()
	if err == nil || !strings.Contains(err.Error(), "still-failing-11") {
		t.Fatalf("exhausted error = %v", err)
	}
	if requests != 11 || len(logs) != 10 || len(waits) != 10 {
		t.Fatalf("requests=%d logs=%d waits=%d", requests, len(logs), len(waits))
	}
}

func TestOpenRouterBackendCancellationInterruptsBackoff(t *testing.T) {
	// Contract 5: cancellation during a Retry-After sleep returns promptly.
	backingOff := make(chan struct{})
	server := newRuntimeRetryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		errorResponse(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()
	var once sync.Once
	backend := &openRouterBackend{
		apiKey: "test", client: server.Client(), endpoint: server.URL,
		logf: func(string, ...any) { once.Do(func() { close(backingOff) }) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := backend.Run(ctx, retryTurn())
		done <- err
	}()
	<-backingOff
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || time.Since(start) > 250*time.Millisecond {
			t.Fatalf("cancel result=%v elapsed=%s", err, time.Since(start))
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestOpenRouterBackendFlagOffHasNoAttemptCap(t *testing.T) {
	// Contract 7: TS-parity flag-off behavior continues beyond ten failures.
	t.Setenv("CODEAF_GO_FIX_RETRY_ATTEMPT_CAP", "0")
	want := errors.New("test sleeper stop")
	requests := 0
	waits := 0
	server := newRuntimeRetryServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		errorResponse(w, "persistent", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	backend := &openRouterBackend{
		apiKey: "test", client: server.Client(), endpoint: server.URL,
		sleep: func(context.Context, time.Duration) error {
			waits++
			if waits == 12 {
				return want
			}
			return nil
		},
	}
	_, err := backend.Run(context.Background(), retryTurn())
	// The engine boundary serializes errors into the assistant record the way
	// TS does, so identity does not survive — the contract is the message and
	// the unbounded attempt count.
	if err == nil || !strings.Contains(err.Error(), want.Error()) || requests != 12 {
		t.Fatalf("err=%v requests=%d waits=%d", err, requests, waits)
	}
}
