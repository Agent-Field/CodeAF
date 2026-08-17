package subharness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// THE RUN'S MODEL, from the two sides it can be wrong from: a run that asked
// for a model and did not get it, and a node that pinned one and lost it.

// modelNameServer is a provider endpoint that answers everything and remembers
// WHICH MODEL each request named. The bridge's other tests read the prompts;
// this one reads the field beside them, because that is the whole of what this
// file changes.
type modelNameServer struct {
	mu    sync.Mutex
	asked []string
	http  *httptest.Server
}

func newModelNameServer(t *testing.T) *modelNameServer {
	t.Helper()
	server := &modelNameServer{}
	server.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		server.mu.Lock()
		server.asked = append(server.asked, body.Model)
		server.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		reply, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{
				"index":   0,
				"message": map[string]any{"role": "assistant", "content": "PASS\nthe step said something"},
			}},
		})
		_, _ = w.Write(reply)
	}))
	t.Cleanup(server.http.Close)
	return server
}

func (s *modelNameServer) client(t *testing.T) *provider.Client {
	t.Helper()
	client, err := provider.NewClient(provider.Config{
		APIKey: "test-key", BaseURL: s.http.URL, Model: "client/default",
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	return client
}

func (s *modelNameServer) models() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// twoLoops is a program with the two cases side by side: a step that named no
// model and one that pinned its own.
func twoLoops() Harness {
	return Harness{
		Id: Id{Name: "two", Version: 1},
		Program: Program{
			Nodes: []Node{
				{Id: "open", Kind: KindAgentLoop, Fields: Fields{"brief": "read the tree"}},
				{Id: "pinned", Kind: KindAgentLoop, Fields: Fields{"brief": "draft it", "model": "vendor/pinned"}},
			},
			Edges: []Edge{{"open", "pinned"}},
		},
		Verify: Verify{Ladder: VerifyAccept},
	}
}

// THE RUN'S MODEL IS WHAT AN UNPINNED STEP RIDES, and a pinned step still rides
// its own: node beats run, run beats the client.
func TestTheRunsModelIsTheDefaultAndANodesOwnModelStillWins(t *testing.T) {
	server := newModelNameServer(t)
	h := twoLoops()
	if _, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{
		Harness: h,
		Model:   "anthropic/claude-opus-5",
	})); err != nil {
		t.Fatalf("run: %v", err)
	}
	asked := server.models()
	if len(asked) != 2 {
		t.Fatalf("the run made %d calls, want two: %v", len(asked), asked)
	}
	if asked[0] != "anthropic/claude-opus-5" {
		t.Fatalf("the unpinned step ran on %q, want the run's model", asked[0])
	}
	if asked[1] != "vendor/pinned" {
		t.Fatalf("the pinned step ran on %q, want its own model", asked[1])
	}
}

// AND WITHOUT ONE, NOTHING MOVES. A run that named no model is the run every
// caller had before the field existed: the client's model, and the node's when
// it has one.
func TestWithoutARunModelTheClientsOwnModelAnswers(t *testing.T) {
	server := newModelNameServer(t)
	h := twoLoops()
	if _, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{Harness: h})); err != nil {
		t.Fatalf("run: %v", err)
	}
	asked := server.models()
	if len(asked) != 2 || asked[0] != "client/default" || asked[1] != "vendor/pinned" {
		t.Fatalf("the run asked %v, want the client's model then the pinned one", asked)
	}
}

// THE PAGE IS NOT REWRITTEN. The override lives as long as the run does: the
// harness in memory is the harness that was loaded, so the next run of it is
// the ordinary one.
func TestTheRunsModelDoesNotWriteItselfOntoTheHarness(t *testing.T) {
	server := newModelNameServer(t)
	h := twoLoops()
	if _, err := Run(context.Background(), h, ModelExec(server.client(t), ModelExecOpts{
		Harness: h,
		Model:   "anthropic/claude-opus-5",
	})); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := h.Program.Nodes[0].Fields.Get("model"); got != "" {
		t.Fatalf("the run left %q on the page", got)
	}
}

// withModel itself, on the four cases it decides: the kinds it reaches, the
// kinds it does not, and the two ways it must answer the node it was given.
func TestWithModelReachesOnlyAnUnpinnedLoop(t *testing.T) {
	cases := []struct {
		name  string
		node  Node
		model string
		want  string
	}{
		{
			name:  "an unpinned loop takes the run's model",
			node:  Node{Id: "open", Kind: KindAgentLoop, Fields: Fields{"brief": "read"}},
			model: "vendor/run",
			want:  "vendor/run",
		},
		{
			name:  "a pinned loop keeps its own",
			node:  Node{Id: "open", Kind: KindAgentLoop, Fields: Fields{"brief": "read", "model": "vendor/node"}},
			model: "vendor/run",
			want:  "vendor/node",
		},
		{
			name:  "no run model changes nothing",
			node:  Node{Id: "open", Kind: KindAgentLoop, Fields: Fields{"brief": "read"}},
			model: "  ",
			want:  "",
		},
		{
			name:  "a judgement is not a step",
			node:  Node{Id: "check", Kind: KindVerify, Fields: Fields{"check": "it works"}},
			model: "vendor/run",
			want:  "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := c.node.Fields.Get("model")
			if got := withModel(c.node, c.model).Fields.Get("model"); got != c.want {
				t.Fatalf("the node ran on %q, want %q", got, c.want)
			}
			// And the node it was HANDED still says what it said: the fields are
			// the saved program's own map, and writing through them would leave
			// the override on the page.
			if after := c.node.Fields.Get("model"); after != before {
				t.Fatalf("the node it was given now says %q, was %q", after, before)
			}
		})
	}
}
