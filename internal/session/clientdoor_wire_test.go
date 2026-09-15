package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/store"
)

type clientDoorCall struct {
	authorization string
	body          map[string]json.RawMessage
}

type clientDoorRequest struct {
	method        string
	path          string
	authorization string
}

// clientDoorServer is a real provider boundary that answers both the streamed
// conversation road and the unstreamed errand roads while retaining the exact
// account and JSON shape each caller put on the wire.
type clientDoorServer struct {
	*httptest.Server
	mu       sync.Mutex
	calls    []clientDoorCall
	requests []clientDoorRequest
	content  string
}

// forbiddenClientDoorServer makes an accidental default-service request fail
// at the real HTTP boundary, while retaining a count for the final assertion.
// A mock completer could only prove that the test had bypassed clientFor too.
type forbiddenClientDoorServer struct {
	*httptest.Server
	calls atomic.Int64
}

func newForbiddenClientDoorServer(t *testing.T) *forbiddenClientDoorServer {
	t.Helper()
	server := &forbiddenClientDoorServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.calls.Add(1)
		t.Errorf("default service received %s %s", request.Method, request.URL.Path)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return server
}

func newClientDoorServer(t *testing.T, content string) *clientDoorServer {
	t.Helper()
	server := &clientDoorServer{content: content}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.mu.Lock()
		server.requests = append(server.requests, clientDoorRequest{
			method: request.Method, path: request.URL.Path, authorization: request.Header.Get("Authorization"),
		})
		server.mu.Unlock()
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

func (s *clientDoorServer) allCalls() []clientDoorCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]clientDoorCall(nil), s.calls...)
}

func (s *clientDoorServer) allRequests() []clientDoorRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]clientDoorRequest(nil), s.requests...)
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

func directClientDoorSources(server *clientDoorServer, key string) modelsource.Set {
	return modelsource.NewSet(
		modelsource.Connected{Source: modelsource.DefaultSource("http://127.0.0.1:1"), Key: "default-key", Address: "http://127.0.0.1:1"},
		modelsource.Connected{Source: modelsource.Source{ID: "direct", Written: "direct"}, Key: key, Address: server.URL},
	)
}

func TestTheSessionReachesTheSourceThatServesItsModel(t *testing.T) {
	server := newClientDoorServer(t, "done")
	agent, err := New(Config{Workspace: t.TempDir(), Model: "direct/stub/conversation", Sources: directClientDoorSources(server, "direct-conversation-key")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "hello")
	if agent.Model() != "direct/stub/conversation" {
		t.Fatalf("session model = %q, want its service-qualified identity", agent.Model())
	}
	assertOrdinaryClientDoorCall(t, server.call(t, 0), "direct-conversation-key", "stub/conversation")
}

// Every model chooses its own service before its slug is put on the request.
// This drives the real Agent through the three tier families that made the
// hand-run leak visible: reflex, worker, and mastermind.
func TestTheCrewReachesItsOwnServiceWhileTheChatIsElsewhere(t *testing.T) {
	defaultServer := newClientDoorServer(t, "done")
	directServer := newClientDoorServer(t, "done")
	defaultSource := modelsource.DefaultSource(defaultServer.URL)
	directSource := modelsource.Source{ID: "direct", Written: "localhost", Address: directServer.URL}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "localhost/fake-small",
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: defaultSource, Key: "default-crew-key", Address: defaultServer.URL},
			modelsource.Connected{Source: directSource, Key: "direct-chat-key", Address: directServer.URL},
		),
		RolesSource: tierSettings(map[string]string{
			roles.TierKey(roles.TierReflex):     "crew/reflex",
			roles.TierKey(roles.TierWorker):     "crew/worker",
			roles.TierKey(roles.TierMastermind): "crew/mastermind",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	for _, role := range []roles.Role{roles.RoleReflex, roles.RoleWorker, roles.RolePlanner} {
		if _, _, err := agent.callRole(t.Context(), role, agent.Model(), []ai.Message{textMessage("user", "answer")}); err != nil {
			t.Fatalf("%s call: %v", role, err)
		}
	}
	drainTurn(t, agent, "chat here")

	crew := defaultServer.allCalls()
	if len(crew) != 3 {
		t.Fatalf("default service received %d crew calls, want 3: %+v", len(crew), crew)
	}
	for index, want := range []string{"crew/reflex", "crew/worker", "crew/mastermind"} {
		if crew[index].authorization != "Bearer default-crew-key" || clientDoorModel(t, crew[index]) != want {
			t.Fatalf("crew call %d = bearer %q model %q, want default bearer and %q", index+1,
				crew[index].authorization, clientDoorModel(t, crew[index]), want)
		}
	}
	chat := directServer.allCalls()
	if len(chat) != 1 || chat[0].authorization != "Bearer direct-chat-key" || clientDoorModel(t, chat[0]) != "fake-small" {
		t.Fatalf("direct service calls = %+v, want only the chat model on its own bearer", chat)
	}
}

func TestACrewSeatFallsToTheSeatedModelWhenTheDefaultServiceHasNoKey(t *testing.T) {
	defaultServer := newForbiddenClientDoorServer(t)
	directServer := newClientDoorServer(t, "done")
	defaultSource := modelsource.DefaultSource(defaultServer.URL)
	directSource := modelsource.Source{ID: "direct", Written: "direct", Address: directServer.URL}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "direct/stub/old-seat",
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: defaultSource, Address: defaultServer.URL},
			modelsource.Connected{Source: directSource, Key: "direct-seat-key", Address: directServer.URL},
		),
		RolesSource: tierSettings(map[string]string{
			roles.TierKey(roles.TierReflex): "crew/reflex",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	// The construction-time seat is deliberately made stale. The model picked
	// live is the one the unavailable crew rung must fall to.
	agent.SetModel("direct/stub/conversation")
	_, answered, err := agent.callRole(t.Context(), roles.RoleReflex, agent.Model(), []ai.Message{textMessage("user", "answer")})
	if err != nil {
		t.Fatal(err)
	}
	if answered != "direct/stub/conversation" {
		t.Fatalf("answer was recorded on %q, want the seated model", answered)
	}
	if got := directServer.allCalls(); len(got) != 1 || got[0].authorization != "Bearer direct-seat-key" || clientDoorModel(t, got[0]) != "stub/conversation" {
		t.Fatalf("connected service calls = %+v, want the seated model's bare slug", got)
	}
	if got := defaultServer.calls.Load(); got != 0 {
		t.Fatalf("default service received %d requests", got)
	}
}

func TestACrewSeatFallsToAKeyOptionalSeatedService(t *testing.T) {
	defaultServer := newForbiddenClientDoorServer(t)
	localServer := newClientDoorServer(t, "done")
	defaultSource := modelsource.DefaultSource(defaultServer.URL)
	localSource := modelsource.Source{
		ID: "ollama", Written: "ollama", Address: localServer.URL, KeyOptional: true,
	}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "ollama/llama3.2",
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: defaultSource, Address: defaultServer.URL},
			modelsource.Connected{Source: localSource, Address: localServer.URL},
		),
		RolesSource: tierSettings(map[string]string{
			roles.TierKey(roles.TierReflex): "crew/reflex",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	_, answered, err := agent.callRole(t.Context(), roles.RoleReflex, agent.Model(), []ai.Message{textMessage("user", "answer")})
	if err != nil {
		t.Fatal(err)
	}
	if answered != "ollama/llama3.2" {
		t.Fatalf("answer was recorded on %q, want the key-optional seat", answered)
	}
	if got := localServer.allCalls(); len(got) != 1 || got[0].authorization != "" || clientDoorModel(t, got[0]) != "llama3.2" {
		t.Fatalf("key-optional service calls = %+v", got)
	}
	if got := defaultServer.calls.Load(); got != 0 {
		t.Fatalf("default service received %d requests", got)
	}
}

func TestAChildAgentFallsToTheSeatedModelWhenTheDefaultServiceHasNoKey(t *testing.T) {
	defaultServer := newForbiddenClientDoorServer(t)
	directServer := newClientDoorServer(t, "child done")
	defaultSource := modelsource.DefaultSource(defaultServer.URL)
	directSource := modelsource.Source{ID: "direct", Written: "direct", Address: directServer.URL}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "direct/stub/conversation",
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: defaultSource, Address: defaultServer.URL},
			modelsource.Connected{Source: directSource, Key: "direct-seat-key", Address: directServer.URL},
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	child, err := agent.newChildAgent(Config{Workspace: t.TempDir(), Model: "crew/worker"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Close() })

	drainTurn(t, child, "work")
	if got := directServer.allCalls(); len(got) != 1 || got[0].authorization != "Bearer direct-seat-key" || clientDoorModel(t, got[0]) != "stub/conversation" {
		t.Fatalf("connected service calls = %+v, want the parent's seated model", got)
	}
	if got := defaultServer.calls.Load(); got != 0 {
		t.Fatalf("default service received %d requests", got)
	}
}

func TestACrewSeatKeepsTheDefaultServiceWhenItsKeyIsPresent(t *testing.T) {
	defaultServer := newClientDoorServer(t, "crew done")
	directServer := newClientDoorServer(t, "direct done")
	defaultSource := modelsource.DefaultSource(defaultServer.URL)
	directSource := modelsource.Source{ID: "direct", Written: "direct", Address: directServer.URL}
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "direct/stub/conversation",
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: defaultSource, Key: "default-crew-key", Address: defaultServer.URL},
			modelsource.Connected{Source: directSource, Key: "direct-seat-key", Address: directServer.URL},
		),
		RolesSource: tierSettings(map[string]string{
			roles.TierKey(roles.TierReflex): "crew/reflex",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	_, answered, err := agent.callRole(t.Context(), roles.RoleReflex, agent.Model(), []ai.Message{textMessage("user", "answer")})
	if err != nil {
		t.Fatal(err)
	}
	if answered != "crew/reflex" {
		t.Fatalf("answer was recorded on %q, want the crew seat", answered)
	}
	if got := defaultServer.allCalls(); len(got) != 1 || got[0].authorization != "Bearer default-crew-key" || clientDoorModel(t, got[0]) != "crew/reflex" {
		t.Fatalf("default service calls = %+v", got)
	}
	if got := directServer.allCalls(); len(got) != 0 {
		t.Fatalf("connected service received the crew call: %+v", got)
	}
}

// A production child is a request-carrying Agent in its own right. Repeating
// the crossing catches the old split identity, where a role call happened to
// use the account pool but a task-shaped child inherited the conversation's
// direct client and sent the same unqualified id to another host.
func TestTheCrewNeverReachesAServiceThatDoesNotServeIt(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		t.Run(fmt.Sprintf("attempt-%d", attempt+1), func(t *testing.T) {
			defaultServer := newClientDoorServer(t, "crew done")
			directServer := newClientDoorServer(t, "chat done")
			defaultSource := modelsource.DefaultSource(defaultServer.URL)
			directSource := modelsource.Source{ID: "direct", Written: "localhost", Address: directServer.URL}
			agent, err := New(Config{
				Workspace: t.TempDir(), Model: "localhost/fake-small",
				Sources: modelsource.NewSet(
					modelsource.Connected{Source: defaultSource, Key: "default-crew-key", Address: defaultServer.URL},
					modelsource.Connected{Source: directSource, Key: "direct-chat-key", Address: directServer.URL},
				),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })

			graph := agent.graph()
			graph.run = func(*TaskNode) {}
			id := graph.reserve()
			graph.admit(id, taskSpec{title: "crew crossing", brief: "answer", acceptance: "done", model: "crew/worker"})
			child, err := agent.newTaskAgent(t.Context(), t.TempDir(), graph.node(id), "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = child.Close() })
			if _, err := child.completeWithModel(t.Context(), purposeTask, []ai.Message{textMessage("user", "work")}, "crew/worker"); err != nil {
				t.Fatal(err)
			}
			drainTurn(t, agent, "chat")

			crew := defaultServer.allCalls()
			if len(crew) != 1 || crew[0].authorization != "Bearer default-crew-key" || clientDoorModel(t, crew[0]) != "crew/worker" {
				t.Fatalf("default service calls = %+v", crew)
			}
			chat := directServer.allCalls()
			if len(chat) != 1 || chat[0].authorization != "Bearer direct-chat-key" || clientDoorModel(t, chat[0]) != "fake-small" {
				t.Fatalf("direct service calls = %+v", chat)
			}
			for _, request := range directServer.allRequests() {
				if request.method != http.MethodPost || request.path != modelsource.ChatCompletionsPath {
					t.Fatalf("direct service received a non-chat request: %+v", directServer.allRequests())
				}
			}
		})
	}
}

// The memory reflex chooses between its own tier and the conversation model
// after it has been bound. Each choice has to resolve its account at that
// moment: resolving only the primary at bind time sends the fallback to the
// wrong host, while binding the conversation client sends the primary there.
func TestTheMemoryReflexResolvesEachActiveModelsService(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		t.Run(fmt.Sprintf("attempt-%d", attempt+1), func(t *testing.T) {
			defaultServer := sourcestub.New("crew/reflex")
			directServer := sourcestub.New("fake-small")
			t.Cleanup(defaultServer.Close)
			t.Cleanup(directServer.Close)
			defaultServer.Refuse(http.StatusOK, `{"model":"crew/reflex","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":" "}}]}`)
			directServer.Refuse(http.StatusOK, `{"model":"fake-small","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"inject\":[],\"cmd\":null}"}}]}`)

			brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = brain.Close() })
			if _, err := brain.AddMemory(store.Memory{
				Type: store.MemoryFact, Scope: store.MemoryScopeUser,
				Title: "the useful fact", Text: "the useful remembered fact",
			}); err != nil {
				t.Fatal(err)
			}

			defaultSource := modelsource.DefaultSource(defaultServer.URL())
			directSource := modelsource.Source{ID: "direct", Written: "localhost", Address: directServer.URL()}
			agent, err := New(Config{
				Workspace: t.TempDir(), Model: "localhost/fake-small", Memory: brain,
				Sources: modelsource.NewSet(
					modelsource.Connected{Source: defaultSource, Key: "default-reflex-key", Address: defaultServer.URL()},
					modelsource.Connected{Source: directSource, Key: "direct-chat-key", Address: directServer.URL()},
				),
				RolesSource: tierSettings(map[string]string{
					roles.TierKey(roles.TierReflex): "crew/reflex",
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })

			// The first route gets an empty primary answer and moves to the
			// conversation-model fallback. The second starts on that fallback.
			agent.routedMemory(t.Context(), "use the useful fact", nil, false)
			agent.routedMemory(t.Context(), "use the useful fact again", nil, false)

			assertSourceRequests := func(server *sourcestub.Server, count int, bearer, model string) {
				t.Helper()
				requests := server.Requests()
				if len(requests) != count {
					t.Fatalf("%s host received %d requests, want %d: %+v", model, len(requests), count, requests)
				}
				for index, request := range requests {
					var body struct {
						Model string `json:"model"`
					}
					if err := json.Unmarshal(request.Body, &body); err != nil {
						t.Fatalf("decode %s request %d: %v", model, index+1, err)
					}
					if request.Method != http.MethodPost || request.Bearer != "Bearer "+bearer || body.Model != model {
						t.Fatalf("%s request %d = method %q bearer %q model %q", model, index+1, request.Method, request.Bearer, body.Model)
					}
				}
			}
			assertSourceRequests(defaultServer, 1, "default-reflex-key", "crew/reflex")
			assertSourceRequests(directServer, 2, "direct-chat-key", "fake-small")
		})
	}
}

func TestRemovingAServiceEvictsAndDisarmsItsClient(t *testing.T) {
	lanes.Default().Reset()
	t.Cleanup(func() { lanes.Default().Reset() })
	defaultServer := newClientDoorServer(t, "default done")
	directServer := newClientDoorServer(t, "direct done")
	defaultSource := modelsource.DefaultSource(defaultServer.URL)
	directSource := modelsource.Source{ID: "direct", Written: "localhost", Address: directServer.URL}
	defaults := modelsource.NewSet(modelsource.Connected{
		Source: defaultSource, Key: "default-key", Address: defaultServer.URL,
	})
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "localhost/fake-small",
		Sources: modelsource.NewSet(
			defaults.Default(),
			modelsource.Connected{Source: directSource, Key: "deleted-key", Address: directServer.URL},
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	stale := unwrapCompleter(agent.client)
	drainTurn(t, agent, "before disconnect")
	agent.SetSources(defaults)
	agent.SetModel("default/chat")
	if _, err := stale.CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "after disconnect")}); !errors.Is(err, provider.ErrNoAPIKey) {
		t.Fatalf("evicted client answered with %v, want no-key refusal", err)
	}
	// Exercise the process sheet after the account change, just as the live beat
	// does while picker refresh and turns continue. Its 404 is expected; which
	// host receives the request is the assertion below.
	_ = lanes.Default().Sheet().Refresh(t.Context(), "default/chat")
	drainTurn(t, agent, "after disconnect")
	if got := directServer.allRequests(); len(got) != 1 || got[0].authorization != "Bearer deleted-key" || got[0].method != http.MethodPost || got[0].path != modelsource.ChatCompletionsPath {
		t.Fatalf("removed service received another request: %+v", got)
	}
	if got := defaultServer.allCalls(); len(got) != 1 || got[0].authorization != "Bearer default-key" || clientDoorModel(t, got[0]) != "default/chat" {
		t.Fatalf("remaining service calls = %+v", got)
	}
}

func TestTheDocumentReadReachesTheSourceThatServesItsModel(t *testing.T) {
	server := newClientDoorServer(t, "extracted text")
	client, err := newDocClient(Config{Model: "direct/stub/document", Sources: directClientDoorSources(server, "direct-document-key")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ParseDocument(context.Background(), provider.DocumentRequest{Filename: "note.txt", MediaType: "text/plain", Data: []byte("note"), Engine: provider.DocumentParseNative}); err != nil {
		t.Fatal(err)
	}
	assertOrdinaryClientDoorCall(t, server.call(t, 0), "direct-document-key", "stub/document")
}

func TestTheStandingRunReachesTheSourceThatServesItsModel(t *testing.T) {
	server := newClientDoorServer(t, "no — nothing changed")
	sentinel := NewStandingSentinel(Config{Model: "direct/stub/standing", Sources: directClientDoorSources(server, "direct-standing-key")})
	if _, _, _, err := sentinel(context.Background(), standing.Judgment{Item: standing.Item{Words: "tell me when it changes"}, Evidence: "unchanged"}); err != nil {
		t.Fatal(err)
	}
	assertOrdinaryClientDoorCall(t, server.call(t, 0), "direct-standing-key", "stub/standing")
}

func TestTheMemoryConsolidationReachesTheSourceThatServesItsModel(t *testing.T) {
	server := newClientDoorServer(t, `{"ops":[]}`)
	root := t.TempDir()
	brainPath := filepath.Join(root, "brain.db")
	addClientDoorMemories(t, brainPath)
	tidy := NewMemoryTidy(Config{Model: "direct/stub/memory", Sources: directClientDoorSources(server, "direct-memory-key")}, brainPath, root, func(_ time.Duration) bool { return true })
	if _, err := tidy(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertOrdinaryClientDoorCall(t, server.call(t, 0), "direct-memory-key", "stub/memory")
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

func TestAFirstRunKeyReachesATaskSpawnedAfterItLands(t *testing.T) {
	server := newClientDoorServer(t, "child done")
	defaultSource := modelsource.DefaultSource(server.URL)
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "stub/conversation",
		Sources: modelsource.NewSet(modelsource.Connected{Source: defaultSource, Address: server.URL}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if err := agent.SetAPIKey("first-run-key"); err != nil {
		t.Fatal(err)
	}
	graph := agent.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "later child", brief: "answer", acceptance: "done"})
	child, err := agent.newTaskAgent(context.Background(), t.TempDir(), graph.node(id), "")
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	drainTurn(t, child, "answer now")
	if got := server.call(t, 0).authorization; got != "Bearer first-run-key" {
		t.Fatalf("child request authorization = %q", got)
	}
	if got := child.config.Sources.Default().Key; got != "first-run-key" {
		t.Fatalf("child inherited stale default-service key %q", got)
	}
}
