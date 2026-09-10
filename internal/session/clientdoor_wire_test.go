package session

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

type clientDoorCall struct {
	authorization string
	body          map[string]json.RawMessage
}

// clientDoorServer is a real provider boundary that answers both the streamed
// conversation road and the unstreamed errand roads while retaining the exact
// account and JSON shape each caller put on the wire.
type clientDoorServer struct {
	*httptest.Server
	mu      sync.Mutex
	calls   []clientDoorCall
	content string
}

func newClientDoorServer(t *testing.T, content string) *clientDoorServer {
	t.Helper()
	server := &clientDoorServer{content: content}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !answersChatOnly(writer, request) {
			return
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read provider request: %v", err)
			return
		}
		body := make(map[string]json.RawMessage)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode provider request: %v", err)
			return
		}
		server.mu.Lock()
		server.calls = append(server.calls, clientDoorCall{
			authorization: request.Header.Get("Authorization"),
			body:          body,
		})
		server.mu.Unlock()

		var streamed bool
		_ = json.Unmarshal(body["stream"], &streamed)
		if streamed {
			writer.Header().Set("Content-Type", "text/event-stream")
			payload, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": content}}},
			})
			_, _ = io.WriteString(writer, "data: "+string(payload)+"\n\ndata: [DONE]\n\n")
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": "stub/answer",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": content},
			}},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *clientDoorServer) call(t *testing.T, index int) clientDoorCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.calls) {
		t.Fatalf("provider received %d requests, want request %d", len(s.calls), index+1)
	}
	return s.calls[index]
}

func clientDoorModel(t *testing.T, call clientDoorCall) string {
	t.Helper()
	var model string
	if err := json.Unmarshal(call.body["model"], &model); err != nil {
		t.Fatalf("wire model: %v (%s)", err, call.body["model"])
	}
	return model
}

func clientDoorReasoning(t *testing.T, call clientDoorCall) string {
	t.Helper()
	raw, ok := call.body["reasoning"]
	if !ok {
		return ""
	}
	var reasoning struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal(raw, &reasoning); err != nil {
		t.Fatalf("wire reasoning: %v (%s)", err, raw)
	}
	return reasoning.Effort
}

func clientDoorHasCeiling(t *testing.T, call clientDoorCall) bool {
	t.Helper()
	var preferences map[string]json.RawMessage
	if raw, ok := call.body["provider"]; ok {
		if err := json.Unmarshal(raw, &preferences); err != nil {
			t.Fatalf("wire provider preferences: %v (%s)", err, raw)
		}
	}
	_, ok := preferences["max_price"]
	return ok
}

func addClientDoorMemories(t *testing.T, path string) {
	t.Helper()
	brain, err := store.Open(path)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	for _, memory := range []store.Memory{
		{Type: store.MemoryFact, Scope: store.MemoryScopeUser, Title: "first", Text: "the first remembered fact"},
		{Type: store.MemoryFact, Scope: store.MemoryScopeUser, Title: "second", Text: "the second remembered fact"},
	} {
		if _, err := brain.AddMemory(memory); err != nil {
			_ = brain.Close()
			t.Fatalf("add memory: %v", err)
		}
	}
	if err := brain.Close(); err != nil {
		t.Fatalf("close memory store: %v", err)
	}
}

func establishClientDoorRouter(t *testing.T, baseURL string) {
	t.Helper()
	lanes.WireSheet(baseURL, "", nil, true)
	t.Cleanup(func() { lanes.WireSheet("", "", nil, false) })
	if !lanes.PrefsProven(baseURL) {
		t.Fatal("the provider stub was not established as a router")
	}
}

// A conversation built by the public session door sends the account it was
// handed, strips the thinking level from the provider's slug, and carries that
// level beside the slug where the request grammar expects it.
func TestAConversationSendsTheBareSlugAndCarriesTheLevelBeside(t *testing.T) {
	server := newClientDoorServer(t, "done")
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "stub/conversation-client-door:low",
		APIKey: "conversation-key", BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("build session: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "hello")

	call := server.call(t, 0)
	if call.authorization != "Bearer conversation-key" {
		t.Fatalf("Authorization = %q, want the session's bearer", call.authorization)
	}
	if model := clientDoorModel(t, call); model != "stub/conversation-client-door" {
		t.Fatalf("model = %q, want the bare slug", model)
	}
	if reasoning := clientDoorReasoning(t, call); reasoning != "low" {
		t.Fatalf("reasoning effort = %q, want low beside the slug", reasoning)
	}
}

// A document read uses the session's account and bare default model even
// though its raw request deliberately has no reasoning object of its own.
func TestADocumentReadSendsTheBareSlugThroughTheSessionsAccount(t *testing.T) {
	server := newClientDoorServer(t, "extracted text")
	client, err := newDocClient(Config{
		Model: "stub/document-client-door:low", APIKey: "document-key", BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("build document client: %v", err)
	}
	if _, err := client.ParseDocument(context.Background(), provider.DocumentRequest{
		Filename: "note.txt", MediaType: "text/plain", Data: []byte("note"), Engine: provider.DocumentParseNative,
	}); err != nil {
		t.Fatalf("read document: %v", err)
	}
	call := server.call(t, 0)
	if call.authorization != "Bearer document-key" {
		t.Fatalf("Authorization = %q, want the session's bearer", call.authorization)
	}
	if model := clientDoorModel(t, call); model != "stub/document-client-door" {
		t.Fatalf("model = %q, want the bare slug", model)
	}
	if _, carried := call.body["reasoning"]; carried {
		t.Fatalf("document request carried reasoning: %s", call.body["reasoning"])
	}
	if _, err := newDocClient(Config{}); err == nil || err.Error() != "this session has no API key" {
		t.Fatalf("empty-key refusal = %v, want %q", err, "this session has no API key")
	}
}

// A standing judgment receives the session catalog's list price, so its
// latency request carries the ceiling that prevents the fastest reseller from
// silently charging several times the published price.
func TestAStandingPassCarriesTheSessionsPublishedPriceCeiling(t *testing.T) {
	server := newClientDoorServer(t, "yes — the evidence changed")
	establishClientDoorRouter(t, server.URL)
	sentinel := NewStandingSentinel(Config{
		Model: "stub/standing-client-door", APIKey: "standing-key", BaseURL: server.URL,
		Routing: provider.RoutingLatency,
		ModelPrice: func(string) (float64, float64, bool) {
			return 0.0000004, 0.0000016, true
		},
	})
	if _, _, _, err := sentinel(context.Background(), standing.Judgment{
		Item: standing.Item{Words: "tell me when it changes"}, Evidence: "it changed",
	}); err != nil {
		t.Fatalf("standing judgment: %v", err)
	}
	call := server.call(t, 0)
	if call.authorization != "Bearer standing-key" {
		t.Fatalf("Authorization = %q, want the session's bearer", call.authorization)
	}
	if !clientDoorHasCeiling(t, call) {
		t.Fatalf("standing request carried no provider.max_price; its provider preferences were %s", call.body["provider"])
	}
}

// A memory consolidation receives the same price seam as the conversation,
// so an unattended pass cannot lose the account's ceiling at its private
// client-construction site.
func TestAMemoryConsolidationCarriesTheSessionsPublishedPriceCeiling(t *testing.T) {
	server := newClientDoorServer(t, `{"ops":[]}`)
	establishClientDoorRouter(t, server.URL)
	root := t.TempDir()
	brainPath := filepath.Join(root, "brain.db")
	addClientDoorMemories(t, brainPath)
	tidy := NewMemoryTidy(Config{
		Model: "stub/memory-client-door", APIKey: "memory-key", BaseURL: server.URL,
		Routing: provider.RoutingLatency,
		ModelPrice: func(string) (float64, float64, bool) {
			return 0.0000004, 0.0000016, true
		},
	}, brainPath, root, func(_ time.Duration) bool { return true })
	if _, err := tidy(context.Background()); err != nil {
		t.Fatalf("memory consolidation: %v", err)
	}
	call := server.call(t, 0)
	if call.authorization != "Bearer memory-key" {
		t.Fatalf("Authorization = %q, want the session's bearer", call.authorization)
	}
	if !clientDoorHasCeiling(t, call) {
		t.Fatalf("memory request carried no provider.max_price; its provider preferences were %s", call.body["provider"])
	}
}

func assertOrdinaryClientDoorCall(t *testing.T, call clientDoorCall, key, model string) {
	t.Helper()
	if call.authorization != "Bearer "+key {
		t.Fatalf("Authorization = %q, want Bearer %s", call.authorization, key)
	}
	if got := clientDoorModel(t, call); got != model {
		t.Fatalf("model = %q, want unchanged plain model %q", got, model)
	}
	if _, carried := call.body["reasoning"]; carried {
		t.Fatalf("plain model invented reasoning: %s", call.body["reasoning"])
	}
	if clientDoorHasCeiling(t, call) {
		t.Fatalf("client with no price seam invented provider.max_price: %#v", call.body)
	}
}

// Plain model ids and absent catalog seams are the compatibility control for
// every private session road: centralising construction changes neither the
// account nor any request field when there is no level or catalog fact to add.
func TestTheSessionClientDoorsLeaveAnOrdinaryRequestAlone(t *testing.T) {
	t.Run("conversation", func(t *testing.T) {
		server := newClientDoorServer(t, "done")
		agent, err := New(Config{
			Workspace: t.TempDir(), Model: "stub/plain-conversation",
			APIKey: "plain-conversation-key", BaseURL: server.URL,
		})
		if err != nil {
			t.Fatalf("build session: %v", err)
		}
		t.Cleanup(func() { _ = agent.Close() })
		drainTurn(t, agent, "hello")
		assertOrdinaryClientDoorCall(t, server.call(t, 0), "plain-conversation-key", "stub/plain-conversation")
	})

	t.Run("document", func(t *testing.T) {
		server := newClientDoorServer(t, "extracted text")
		client, err := newDocClient(Config{
			Model: "stub/plain-document", APIKey: "plain-document-key", BaseURL: server.URL,
		})
		if err != nil {
			t.Fatalf("build document client: %v", err)
		}
		if _, err := client.ParseDocument(context.Background(), provider.DocumentRequest{
			Filename: "note.txt", MediaType: "text/plain", Data: []byte("note"), Engine: provider.DocumentParseNative,
		}); err != nil {
			t.Fatalf("read document: %v", err)
		}
		assertOrdinaryClientDoorCall(t, server.call(t, 0), "plain-document-key", "stub/plain-document")
	})

	t.Run("standing", func(t *testing.T) {
		server := newClientDoorServer(t, "no — nothing changed")
		sentinel := NewStandingSentinel(Config{
			Model: "stub/plain-standing", APIKey: "plain-standing-key", BaseURL: server.URL,
		})
		if _, _, _, err := sentinel(context.Background(), standing.Judgment{
			Item: standing.Item{Words: "tell me when it changes"}, Evidence: "unchanged",
		}); err != nil {
			t.Fatalf("standing judgment: %v", err)
		}
		assertOrdinaryClientDoorCall(t, server.call(t, 0), "plain-standing-key", "stub/plain-standing")
	})

	t.Run("memory", func(t *testing.T) {
		server := newClientDoorServer(t, `{"ops":[]}`)
		root := t.TempDir()
		brainPath := filepath.Join(root, "brain.db")
		addClientDoorMemories(t, brainPath)
		tidy := NewMemoryTidy(Config{
			Model: "stub/plain-memory", APIKey: "plain-memory-key", BaseURL: server.URL,
		}, brainPath, root, func(_ time.Duration) bool { return true })
		if _, err := tidy(context.Background()); err != nil {
			t.Fatalf("memory consolidation: %v", err)
		}
		assertOrdinaryClientDoorCall(t, server.call(t, 0), "plain-memory-key", "stub/plain-memory")
	})
}
