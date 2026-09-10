package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

type v3ClientDoorCall struct {
	authorization string
	body          map[string]json.RawMessage
}

// v3ClientDoorServer retains the real HTTP request made by either harness
// runner and answers the streamed and unstreamed provider dialects, so these
// tests measure the launch wiring rather than a provider.Config value.
type v3ClientDoorServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []v3ClientDoorCall
}

func newV3ClientDoorServer(t *testing.T) *v3ClientDoorServer {
	t.Helper()
	server := &v3ClientDoorServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/chat/completions" {
			http.NotFound(writer, request)
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
		server.calls = append(server.calls, v3ClientDoorCall{
			authorization: request.Header.Get("Authorization"), body: body,
		})
		server.mu.Unlock()

		var streamed bool
		_ = json.Unmarshal(body["stream"], &streamed)
		if streamed {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(writer, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
			_, _ = io.WriteString(writer, "data: [DONE]\n\n")
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"model":"stub/answer","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *v3ClientDoorServer) call(t *testing.T, index int) v3ClientDoorCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.calls) {
		t.Fatalf("provider received %d requests, want request %d", len(s.calls), index+1)
	}
	return s.calls[index]
}

func assertV3ClientDoorCall(t *testing.T, call v3ClientDoorCall, key, model, effort string) {
	t.Helper()
	if call.authorization != "Bearer "+key {
		t.Fatalf("Authorization = %q, want Bearer %s", call.authorization, key)
	}
	var gotModel string
	if err := json.Unmarshal(call.body["model"], &gotModel); err != nil {
		t.Fatalf("wire model: %v (%s)", err, call.body["model"])
	}
	if gotModel != model {
		t.Fatalf("model = %q, want %q", gotModel, model)
	}
	var gotEffort string
	if raw, ok := call.body["reasoning"]; ok {
		var reasoning struct {
			Effort string `json:"effort"`
		}
		if err := json.Unmarshal(raw, &reasoning); err != nil {
			t.Fatalf("wire reasoning: %v (%s)", err, raw)
		}
		gotEffort = reasoning.Effort
	}
	if gotEffort != effort {
		t.Fatalf("reasoning effort = %q, want %q", gotEffort, effort)
	}
	var preferences map[string]json.RawMessage
	if raw, ok := call.body["provider"]; ok {
		if err := json.Unmarshal(raw, &preferences); err != nil {
			t.Fatalf("wire provider preferences: %v (%s)", err, raw)
		}
	}
	if _, carried := preferences["max_price"]; carried {
		t.Fatalf("client with no price seam invented provider.max_price: %#v", call.body)
	}
}

func saveV3ClientDoorHarness(t *testing.T, store *subharness.Store) {
	t.Helper()
	if _, err := store.Save(subharness.Harness{
		Id: subharness.Id{Name: "client-door", Desc: "checks the client door"},
		Program: subharness.Program{Nodes: []subharness.Node{
			{Id: "work", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "say done"}},
		}},
		Verify: subharness.Verify{Ladder: subharness.VerifyAccept},
	}); err != nil {
		t.Fatalf("save harness: %v", err)
	}
}

func runV3HarnessClientDoor(t *testing.T, server *v3ClientDoorServer, model, key string) {
	t.Helper()
	store := subharness.At(t.TempDir())
	saveV3ClientDoorHarness(t, store)
	run := v3RunHarness(store, config.Config{APIKey: key, BaseURL: server.URL}, model, t.TempDir(), session.HarnessBeltSeams{})
	if run == nil {
		t.Fatal("the harness run door was not built")
	}
	if _, _, err := run(context.Background(), "client-door", "say done", "", func(subharness.Trail) {}); err != nil {
		t.Fatalf("run harness: %v", err)
	}
}

func runV3SubharnessClientDoor(t *testing.T, server *v3ClientDoorServer, model, key string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AFORGE_HOME", t.TempDir())
	wiring := v3Subharnesses(config.Config{APIKey: key, BaseURL: server.URL}, nil, model, t.TempDir(), nil)
	if wiring.Registry == nil {
		t.Fatal("the subharness registry was not built")
	}
	runner, err := wiring.Registry.Subharness(exec.LinearSubharness)
	if err != nil {
		t.Fatalf("resolve the linear subharness: %v", err)
	}
	input, _ := json.Marshal(exec.TaskInput{Brief: "say done"})
	if _, err := runner.Run(context.Background(), input, nil); err != nil {
		t.Fatalf("run subharness: %v", err)
	}
}

// The offer-card harness runner inherits the profile account and splits a
// thinking level off the model before its first node reaches the provider.
func TestAHarnessRunSendsTheBareSlugThroughTheProfilesAccount(t *testing.T) {
	server := newV3ClientDoorServer(t)
	runV3HarnessClientDoor(t, server, "stub/harness-client-door:low", "harness-key")
	assertV3ClientDoorCall(t, server.call(t, 0), "harness-key", "stub/harness-client-door", "low")
}

// The subharness registry's plain adapter comes through the same profile door,
// including the bearer and the split between a slug and its thinking level.
func TestASubharnessRunSendsTheBareSlugThroughTheProfilesAccount(t *testing.T) {
	server := newV3ClientDoorServer(t)
	runV3SubharnessClientDoor(t, server, "stub/subharness-client-door:low", "subharness-key")
	assertV3ClientDoorCall(t, server.call(t, 0), "subharness-key", "stub/subharness-client-door", "low")
}

// Plain model ids with no catalog seams are the compatibility control for both
// harness roads: centralising construction leaves their bearer, slug and
// absent optional fields exactly where the old literals put them.
func TestTheHarnessClientDoorsLeaveAnOrdinaryRequestAlone(t *testing.T) {
	t.Run("harness", func(t *testing.T) {
		server := newV3ClientDoorServer(t)
		runV3HarnessClientDoor(t, server, "stub/plain-harness", "plain-harness-key")
		assertV3ClientDoorCall(t, server.call(t, 0), "plain-harness-key", "stub/plain-harness", "")
	})
	t.Run("subharness", func(t *testing.T) {
		server := newV3ClientDoorServer(t)
		runV3SubharnessClientDoor(t, server, "stub/plain-subharness", "plain-subharness-key")
		assertV3ClientDoorCall(t, server.call(t, 0), "plain-subharness-key", "stub/plain-subharness", "")
	})
}
