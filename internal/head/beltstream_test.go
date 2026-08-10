package head

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// The belt's calls go through the real adapter here, streamed, because that is
// the only place the defect lived: the accumulator dropped tool calls, so every
// belt turn came back with none, the loop spent a full board-and-notebook round
// trip that could not succeed, and the message fell through to the router
// anyway. The assertion is the one that matters — the tools actually ran — and
// the count is what says the round trip was not wasted.

// beltStreamServer answers the belt with tool calls and the router with text,
// all over the event stream, and records what it was asked.
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
	// one tooled call, learns nothing from it, and pays the router as well.
	if tooled != 2 {
		t.Fatalf("belt made %d tooled calls, want the read and the answer", tooled)
	}
	if plain != 0 {
		t.Fatalf("the belt answered and the router was still paid %d times", plain)
	}
	if thinking == 0 {
		t.Fatal("the reasoning phase reached the surface as dead air")
	}
}

// A message the belt cannot settle still costs exactly one tooled call before
// the router answers — the loop declines on the first turn rather than looping.
func TestStreamedBeltDeclineCostsOneCall(t *testing.T) {
	graph := openHeadStore(t)
	seedExceptBoard(t, graph)
	declining := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(payload), `"tools"`) {
			_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"` +
				controlNotWorkSentinel + `"},"finish_reason":"stop"}]}` + "\n\n"))
		} else {
			_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"{\"reply\":\"Nothing to stop.\",\"command\":null}"},"finish_reason":"stop"}]}` + "\n\n"))
		}
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
		declining.ServeHTTP(writer, request)
	})

	session := "belt-decline"
	user := postUser(t, graph, session, "kill everything except the finance one")
	client := streamedProviderClient(t, counting)
	ctx := provider.WithStreamObserver(context.Background(), func(provider.StreamEvent) {})
	if err := New(client, graph).answer(ctx, user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if strings.Contains(reply.Body, controlNotWorkSentinel) {
		t.Fatalf("the sentinel reached the thread: %q", reply.Body)
	}
	if tooled, _ := server.counts(); tooled != 1 {
		t.Fatalf("a declining belt made %d tooled calls, want one", tooled)
	}
}
