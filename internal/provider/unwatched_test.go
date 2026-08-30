package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE CALL NOBODY IS WATCHING IS THE ONE THAT HUNG.
//
// Every test beside this one attaches a stream observer, which is what an
// interactive surface does — and until 2026-08 attaching one was also what
// decided whether the request was streamed at all. A headless `aforge do` leaf
// attaches none, so its calls took the request/response path, whose only bound
// is adaptiveCompletionTimeout's fifteen-minute ceiling. Measured, on the
// happy-dom run of 2026-08-28: three provider calls that produced nothing, each
// held for 15m24s, each followed by a twenty-minute claim reaper taking the node
// away and starting it over. Ninety minutes, $0.63, no product.
//
// The detectors were all present and all dormant. These tests pin that they are
// armed by the CALL and not by an audience.

// completeUnwatched makes one completion with NO observer on the context — the
// headless shape — against a scripted endpoint.
func completeUnwatched(t *testing.T, base string) (*ai.Response, error) {
	t.Helper()
	client, err := NewClient(Config{APIKey: "k", BaseURL: base, Model: "sim/model"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	// A ledger of its own, for the reason streamAgainst gives: the wall is
	// derived from one, and the process-wide ledger carries whatever the test
	// before this earned.
	client.velocity = newVelocityLedger()
	return client.CompleteWithMessages(context.Background(), []ai.Message{{
		Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}},
	}})
}

// TestAnUnwatchedCallIsStillGuarded is defect 1. The endpoint accepts the
// request, sends keepalives forever and never writes a token — the shape a
// DeepSeek endpoint actually took — and nobody is watching. It must be cut in
// the bound, not held for the total deadline.
func TestAnUnwatchedCallIsStillGuarded(t *testing.T) {
	restore := shortenStallBounds(t, 60*time.Millisecond, 60*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for range 40 {
			fmt.Fprint(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	began := time.Now()
	_, err := completeUnwatched(t, server.URL)
	cut, ok := CutFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a stream cut on a call nobody was watching", err)
	}
	if cut.Reason != CutSilent {
		t.Fatalf("reason = %d, want CutSilent", cut.Reason)
	}
	// The bound and nothing near the deadline: the point of the fix is that this
	// ends in the guard's window rather than in the transport's.
	if waited := time.Since(began); waited > 10*time.Second {
		t.Fatalf("an unwatched silent call was held for %s", waited)
	}
}

// TestAnUnwatchedStallStrikesTheLaneItStalledOn is the other half of defect 1:
// an endpoint that stalls is treated exactly like one that refuses. The ledger
// takes its lane away, so the next request this process encodes routes around
// it — and the cut says so, which is how the caller knows another attempt is
// worth making.
func TestAnUnwatchedStallStrikesTheLaneItStalledOn(t *testing.T) {
	restore := shortenStallBounds(t, 60*time.Millisecond, 60*time.Millisecond)
	defer restore()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		// One chunk names who is serving, and then the endpoint goes quiet for
		// good. Naming the lane is what makes it strikeable — an anonymous
		// stall has nothing to route around.
		fmt.Fprint(w, `data: {"id":"one","provider":"slow-pool","choices":[{"index":0,"delta":{"content":"the answer begins"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		for range 40 {
			fmt.Fprint(w, ": keepalive\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "openrouter/sim-model"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ledger := newVelocityLedger()
	client.velocity = ledger

	_, err = client.CompleteWithMessages(context.Background(), []ai.Message{{
		Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}},
	}})
	cut, ok := CutFrom(err)
	if !ok {
		t.Fatalf("err = %v, want a stream cut", err)
	}
	if cut.Provider != "slow-pool" {
		t.Fatalf("cut named %q, want the endpoint the stream said was serving it", cut.Provider)
	}
	if !cut.Rerouted {
		t.Fatal("a named endpoint that stalled must lose its lane, exactly as one that refuses does")
	}
	_, ignored := ledger.preferences("openrouter/sim-model")
	if len(ignored) == 0 {
		t.Fatal("the ledger must refuse the lane that stalled, so the next encode routes around it")
	}
}

// TestAnEndpointThatAnswersWholeIsKeptAndRemembered is the one dialect that is
// not a stall. A gateway may take `stream: true` and serve one whole JSON
// completion; that answer is real and already billed, so it is parsed rather
// than re-asked, and the endpoint is remembered so the next call is bounded the
// way an answer with no inside has to be.
func TestAnEndpointThatAnswersWholeIsKeptAndRemembered(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"whole"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.velocity = newVelocityLedger()
	messages := []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}}}

	response, err := client.CompleteWithMessages(context.Background(), messages)
	if err != nil {
		t.Fatalf("a whole answer must be an answer: %v", err)
	}
	if len(response.Choices) != 1 || !strings.Contains(response.Choices[0].Message.Content[0].Text, "whole") {
		t.Fatalf("response = %#v, want the answer the endpoint actually sent", response)
	}
	if requests.Load() != 1 {
		t.Fatalf("made %d requests, want one — the answer was already paid for", requests.Load())
	}
	if !client.unstreamable.Load() {
		t.Fatal("an endpoint that answered whole must be remembered, so its next call is bounded in total")
	}
}
