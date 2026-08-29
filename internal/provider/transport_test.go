package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// blockingBody is a stream that has gone quiet: it never returns from Read
// until the request context is cancelled, which is exactly what a stalled
// connection looks like from inside the decode loop.
type blockingBody struct {
	ctx    context.Context
	closed atomic.Bool
}

func (b *blockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *blockingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestIdleWatchdogCancelsASilentStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &blockingBody{ctx: ctx}
	watched := newIdleWatchdog(body, 30*time.Millisecond, cancel)

	started := time.Now()
	if _, err := watched.Read(make([]byte, 8)); err == nil {
		t.Fatal("a silent stream should end in an error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the watchdog took %s to notice silence", elapsed)
	}
	if err := watched.Close(); err != nil {
		t.Fatal(err)
	}
	if !body.closed.Load() {
		t.Fatal("Close must reach the underlying body")
	}
}

// dribble delivers one byte at a time with a gap, which is the shape of a
// healthy but slow answer — the case the total client deadline used to kill.
type dribble struct {
	remaining int
	gap       time.Duration
}

func (d *dribble) Read(p []byte) (int, error) {
	if d.remaining == 0 {
		return 0, io.EOF
	}
	time.Sleep(d.gap)
	d.remaining--
	p[0] = 'x'
	return 1, nil
}

func (d *dribble) Close() error { return nil }

func TestIdleWatchdogLetsASlowStreamFinish(t *testing.T) {
	cancelled := false
	watched := newIdleWatchdog(&dribble{remaining: 20, gap: 5 * time.Millisecond},
		100*time.Millisecond, func() { cancelled = true })
	defer watched.Close()

	buffer := make([]byte, 1)
	for {
		_, err := watched.Read(buffer)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("a delivering stream was cut short: %v", err)
		}
	}
	// Twenty gaps is twice the idle window in total, and none of it is silence.
	if cancelled {
		t.Fatal("the watchdog fired on a stream that never stopped delivering")
	}
}

// The regression itself: a total deadline that covers body reads kills a
// healthy stream at the deadline, however much of the answer is still coming.
// A non-streamed call keeps that deadline, because there the whole answer is
// the response and a call still open past the budget is a wedged call.
func TestStreamOutlivesTheTotalTimeout(t *testing.T) {
	const pause = 300 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") == "text/event-stream" {
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.WriteHeader(http.StatusOK)
			writer.(http.Flusher).Flush()
			// Longer than the client's total deadline, and entirely normal for
			// a model that thinks before it answers.
			time.Sleep(pause)
			fmt.Fprint(writer, "data: {\"id\":\"one\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"late\"}}]}\n\n")
			writer.(http.Flusher).Flush()
			fmt.Fprint(writer, "data: [DONE]\n\n")
			return
		}
		time.Sleep(pause)
		fmt.Fprint(writer, `{"id":"one","choices":[{"index":0,"message":{"role":"assistant","content":"late"}}]}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		APIKey: "test", BaseURL: server.URL, Model: "vendor/model",
		HTTPClient: &http.Client{Timeout: pause / 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	var streamed strings.Builder
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamDelta {
			streamed.WriteString(event.Delta)
		}
	})
	response, err := client.CompleteWithMessages(ctx, []ai.Message{{Role: "user"}})
	if err != nil {
		t.Fatalf("a stream slower than the total timeout must still land: %v", err)
	}
	if got := strings.TrimSpace(response.Text()); got != "late" {
		t.Fatalf("streamed text = %q", got)
	}
	if streamed.String() != "late" {
		t.Fatalf("observer saw %q", streamed.String())
	}

}

// The other half of the same rule, asserted where it can be asserted quickly:
// a completion keeps the adaptive total deadline, a stream carries none.
func TestClientForBoundsCompletionsInTimeAndStreamsInSilence(t *testing.T) {
	client, err := NewClient(Config{APIKey: "test", BaseURL: "https://example.invalid/v1", Model: "vendor/model"})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	if got := client.clientFor("vendor/model", false, 32_768).Timeout; got != 512*time.Second {
		t.Fatalf("completion timeout = %s, want the adaptive budget", got)
	}
	if got := client.clientFor("vendor/model", true, 32_768).Timeout; got != 0 {
		t.Fatalf("a stream must carry no total deadline, got %s", got)
	}
	if client.clientFor("vendor/model", true, 0).Transport != streamTransport() {
		t.Fatal("a stream must go out over the transport that bounds the header wait")
	}
	if client.clientFor("vendor/model", false, 0).Transport != SharedTransport() {
		t.Fatal("a completion must go out over the pool without a header deadline")
	}
}

func TestSharedTransportPoolsPerHostConnections(t *testing.T) {
	transport := SharedTransport()
	if transport.MaxIdleConnsPerHost != limiterCeiling {
		t.Fatalf("MaxIdleConnsPerHost = %d, want the limiter's ceiling %d",
			transport.MaxIdleConnsPerHost, limiterCeiling)
	}
	if transport.ResponseHeaderTimeout != 0 {
		t.Fatal("a header deadline on the shared transport would cut off long non-streamed answers")
	}
	if streamTransport().ResponseHeaderTimeout != responseHeaderTimeout {
		t.Fatal("the streaming transport must bound how long a silent provider may hold the connection")
	}
	if streamTransport() == transport {
		t.Fatal("the streaming transport needs its own pool: ResponseHeaderTimeout is transport-wide")
	}
}

// A completion on a lane that has finished a reply for us is held under that
// lane's wall; a lane nothing is known about keeps the adaptive budget.
func TestACompletionOnAMeasuredLaneIsHeldUnderItsWall(t *testing.T) {
	client, err := NewClient(Config{APIKey: "test", BaseURL: "https://example.invalid/v1", Model: "vendor/model"})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	const room = 65_536
	if got := client.clientFor("vendor/model", false, room).Timeout; got != adaptiveCompletionTimeout(room, 0) {
		t.Fatalf("an unmeasured lane should keep the adaptive budget, got %s", got)
	}
	client.velocity.noteRun("vendor/model", "fast-endpoint", 24*time.Second)
	if got := client.clientFor("vendor/model", false, room).Timeout; got != wallFor(24*time.Second) {
		t.Fatalf("a measured lane should be held under its wall %s, got %s", wallFor(24*time.Second), got)
	}
	client.velocity.noteRun("vendor/model", "slow-endpoint", 4*time.Minute)
	if got := client.clientFor("vendor/model", false, room).Timeout; got != adaptiveCompletionTimeout(room, 0) {
		t.Fatalf("a wall above the adaptive budget must not lengthen it, got %s", got)
	}
	if got := client.clientFor("vendor/model", true, room).Timeout; got != 0 {
		t.Fatalf("a stream still carries no total deadline, got %s", got)
	}
}
