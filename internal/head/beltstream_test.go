package head

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The belt's calls go through the real adapter here, streamed, because that is
// the only place the defect lived: the accumulator dropped tool calls, so every
// belt turn came back with none, the loop spent a full board-and-notebook round
// trip that could not succeed, and the message was paid for a second time.
// The assertion is the one that matters — the tools actually ran — and the count
// is what says the round trip was not wasted.

// beltStreamServer answers with tool calls and then with text, all over the
// event stream, and records what it was asked. Every call the loop makes carries
// tools now, so a call with none is itself a finding: nothing in the head speaks
// to a model without offering it hands.
type beltStreamServer struct {
	mutex  sync.Mutex
	tooled int
	plain  int
}

func (server *beltStreamServer) handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		var decoded struct {
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(payload, &decoded)
		server.mutex.Lock()
		tooled := len(decoded.Tools) > 0
		if tooled {
			server.tooled++
		} else {
			server.plain++
		}
		turn := server.tooled
		server.mutex.Unlock()

		writer.Header().Set("Content-Type", "text/event-stream")
		switch {
		case tooled && turn == 1:
			// One reasoning delta, then a tool call spelled out in fragments —
			// exactly the shape an OpenRouter reasoning model sends.
			for _, event := range []string{
				`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"which jobs are live"}}]}`,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"board","arguments":""}}]}}]}`,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			} {
				_, _ = writer.Write([]byte("data: " + event + "\n\n"))
			}
		case tooled:
			_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"Three things are running: the finance close, the market research and the line scans."},"finish_reason":"stop"}]}` + "\n\n"))
		default:
			_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"{\"reply\":\"routed\",\"command\":null}"},"finish_reason":"stop"}]}` + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
}

func (server *beltStreamServer) counts() (tooled, plain int) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.tooled, server.plain
}

func streamedProviderClient(t *testing.T, handler http.Handler) *provider.Client {
	t.Helper()
	httpClient := &http.Client{Transport: handlerRoundTrip{handler: handler}}
	client, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "sim/model", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// handlerRoundTrip serves the adapter's requests without opening a listener,
// exactly as the provider package's own wire tests do.
type handlerRoundTrip struct{ handler http.Handler }

func (h handlerRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

func TestStreamedControlBeltActsInsteadOfBurningTheCall(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	server := &beltStreamServer{}
	client := streamedProviderClient(t, server.handler())

	var thinking int
	ctx := provider.WithStreamObserver(context.Background(), func(event provider.StreamEvent) {
		if event.Kind == provider.StreamThinking {
			thinking++
		}
	})
	session := "belt-streamed"
	user := postUser(t, graph, session, "kill everything except the finance one")
	if err := New(client, graph).answer(ctx, user); err != nil {
		t.Fatal(err)
	}

	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(reply.Body, "market research") {
		t.Fatalf("belt answer did not come from the board it read: %q", reply.Body)
	}
	tooled, plain := server.counts()
	// Two tooled calls: the one that asked for the board and the one that spoke
	// with the board's answer in hand. A belt whose tool calls are dropped makes
	// one tooled call and learns nothing from it.
	if tooled != 2 {
		t.Fatalf("belt made %d tooled calls, want the read and the answer", tooled)
	}
	if plain != 0 {
		t.Fatalf("the turn made %d calls with no tools offered at all", plain)
	}
	if thinking == 0 {
		t.Fatal("the reasoning phase reached the surface as dead air")
	}
}

// A turn that touches nothing costs exactly one call, and that call speaks.
//
// This used to be the decline path: the loop said a sentinel, the sentinel was
// swallowed, and the message was paid for a second time at the router. There is
// no second engine to fall through to now, so the property worth pinning is the
// one the fall-through was hiding — a message with no verb in it is answered
// once, in words, by the same call that was offered the tools and did not need
// them, and nothing is journaled on the way past.
func TestATurnThatTouchesNothingSpeaksOnceAndPaysOnce(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	const said = "Nothing has stopped — that poem is not something on the board."
	answering := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"` +
			said + `"},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
	server := &beltStreamServer{}
	counting := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		server.mutex.Lock()
		if strings.Contains(string(payload), `"tools"`) {
			server.tooled++
		} else {
			server.plain++
		}
		server.mutex.Unlock()
		request.Body = io.NopCloser(strings.NewReader(string(payload)))
		answering.ServeHTTP(writer, request)
	})

	session := "belt-quiet"
	user := postUser(t, graph, session, "finish reading me that Auden poem")
	client := streamedProviderClient(t, counting)
	ctx := provider.WithStreamObserver(context.Background(), func(provider.StreamEvent) {})
	if err := New(client, graph).answer(ctx, user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != said {
		t.Fatalf("the turn's own words were not the reply: %q", reply.Body)
	}
	tooled, plain := server.counts()
	if tooled != 1 {
		t.Fatalf("a turn that touched nothing made %d tooled calls, want one", tooled)
	}
	if plain != 0 {
		t.Fatalf("a turn that touched nothing was paid for %d more times", plain)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a turn that touched nothing journaled: %+v", commands)
	}
	if reply.CommandSeq != 0 {
		t.Fatalf("a turn that touched nothing carried command seq %d", reply.CommandSeq)
	}
}

// TestServeStampsStreamEventsWithTheAnsweringSession exercises the producer
// side of the session key end to end: Serve, not a direct answer() call, so
// the stamp actually goes through answerTurn's context wrapping rather than
// being skipped by a test that builds its own context. Every event the
// provider call emits for one turn must carry that turn's session, because
// that is the value a multi-room consumer would key on.
func TestServeStampsStreamEventsWithTheAnsweringSession(t *testing.T) {
	graph := openHeadStore(t)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"hi there"},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
	client := streamedProviderClient(t, handler)

	var mutex sync.Mutex
	var sessions []string
	ctx, cancel := context.WithCancel(context.Background())
	ctx = provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		mutex.Lock()
		sessions = append(sessions, event.Session)
		mutex.Unlock()
	})

	done := make(chan error, 1)
	go func() { done <- New(client, graph).Serve(ctx) }()

	const session = "room-a"
	user, err := graph.PostMessage(store.Message{SessionID: session, Role: store.RoleUser, Body: "hello"})
	if err != nil {
		t.Fatalf("post user message: %v", err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if reply.Body != "hi there" {
		t.Fatalf("reply body = %q", reply.Body)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("serve returned %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}

	mutex.Lock()
	defer mutex.Unlock()
	if len(sessions) == 0 {
		t.Fatal("no stream events observed for the turn")
	}
	for index, got := range sessions {
		if got != session {
			t.Fatalf("event %d carried session %q, want the answering session %q", index, got, session)
		}
	}
}
