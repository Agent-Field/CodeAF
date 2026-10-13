package appapi

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
)

// A blocked tick makes the shutdown race deterministic without a real sleep.
func TestServerCloseCancelsAndJoinsAnInFlightStandingTick(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	s := New(testToken, Options{Agents: NewAgentRegistry(t.TempDir(), nil), StandingTick: func(ctx context.Context, _ *standing.Store) error {
		close(started)
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	}})
	t.Cleanup(s.Close)
	s.wakeOrders()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("standing tick did not start")
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel its tick")
	}
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before the tick ended")
	}
}

func TestServerCloseCancelsAndJoinsAnInFlightAIStream(t *testing.T) {
	streamStopped, streamRelease := make(chan struct{}), make(chan struct{})
	receiptStarted, receiptRelease := make(chan struct{}), make(chan struct{})
	var releaseOnce, streamOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(receiptRelease) }) }
	releaseStream := func() { streamOnce.Do(func() { close(streamRelease) }) }
	defer release()
	defer releaseStream()
	s, calls, home := aiTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generation" {
			close(receiptStarted)
			select {
			case <-receiptRelease:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"data":{"total_cost":0.0012,"tokens_prompt":3,"tokens_completion":1}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"shutdown\",\"choices\":[{\"delta\":{\"content\":\"live delta\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	runtime := s.options.Context.Value(aiRuntimeKey{}).(AICallRuntime)
	original := runtime.ClientConfig
	runtime.ClientConfig = func(model string) provider.Config {
		cfg := original(model)
		cfg.Direct = false
		cfg.HTTPClient = &http.Client{Transport: keyTransport(func(r *http.Request) (*http.Response, error) {
			response, err := http.DefaultTransport.RoundTrip(r)
			if err == nil && r.URL.Path == "/chat/completions" {
				response.Body = &shutdownStreamBody{ReadCloser: response.Body, ctx: r.Context(), stopped: streamStopped, release: streamRelease}
			}
			return response, err
		})}
		return cfg
	}
	s.options.Context = WithAICallRuntime(s.options.Context, runtime)
	server := httptest.NewServer(s)
	defer func() { releaseStream(); release(); s.Close(); server.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/app/v1/ai/call", strings.NewReader(strings.ReplaceAll(aiTestBody, `"stream":false`, `"stream":true`)))
	req.Header.Set("Authorization", "Bearer test-resource-token")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "live delta") {
			break
		}
	}
	finished := make(chan struct{})
	go func() { s.Close(); close(finished) }()
	select {
	case <-streamStopped:
	case <-ctx.Done():
		t.Fatal("shutdown did not cancel the stream")
	}
	select {
	case <-finished:
		t.Fatal("Close returned before the canceled arm registered its receipt")
	default:
	}
	releaseStream()
	select {
	case <-receiptStarted:
	case <-ctx.Done():
		t.Fatal("shutdown did not cancel the stream and request its receipt")
	}
	select {
	case <-finished:
		t.Fatal("Close returned while the spending receipt was still in flight")
	default:
	}
	if w := resourceRequest(s, "POST", "/app/v1/ai/call", aiTestBody, ""); w.Code != 503 || calls.Load() != 2 {
		t.Fatalf("shutdown admitted another AI call: %d %s; calls=%d", w.Code, w.Body.String(), calls.Load())
	}
	release()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("Close did not join the AI receipt")
	}
	lines, err := session.ReadUsage(filepath.Join(home, "v3", session.UsageLedgerName), time.Time{})
	if err != nil || len(lines) != 1 || !lines[0].Reconciled || lines[0].USD != .0012 {
		t.Fatalf("shutdown lost its final spending receipt: %+v %v", lines, err)
	}
}

// The canceled reader stays blocked before receipt registration, exposing the
// gap between a routing race's early return and its arm's final spending callback.
type shutdownStreamBody struct {
	io.ReadCloser
	ctx     context.Context
	stopped chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (b *shutdownStreamBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && b.ctx.Err() != nil {
		b.once.Do(func() { close(b.stopped) })
		<-b.release
	}
	return n, err
}
