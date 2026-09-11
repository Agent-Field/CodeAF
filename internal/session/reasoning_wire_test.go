package session

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	cfgstore "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// ── the reasoning level, end to end ─────────────────────────────────────────
//
// The same full-stack shape as [TestSetModelChangesTheWireModel] one file over,
// and for the same reason: a knob that moves the status line while the wire
// carries nothing is the failure mode this whole feature has, and only a real
// HTTP body can tell the two apart.

// reasoningServer stands up a provider that records every request body and
// answers with whatever the script says. Each entry is the SSE payload list for
// one request; the last entry answers every request after it, so a test only
// scripts the steps it cares about.
type reasoningServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []map[string]json.RawMessage
	// before runs after the body is recorded and before the answer is written,
	// with the number of requests seen so far. It is how a test changes the
	// agent's mind IN THE MIDDLE of a turn.
	before func(seen int)
}

func newReasoningServer(t *testing.T, script ...[]string) *reasoningServer {
	t.Helper()
	server := &reasoningServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body: %v", err)
		}
		server.mu.Lock()
		server.bodies = append(server.bodies, body)
		seen := len(server.bodies)
		before := server.before
		server.mu.Unlock()
		if before != nil {
			before(seen)
		}
		payloads := script[len(script)-1]
		if seen <= len(script) {
			payloads = script[seen-1]
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, payload := range payloads {
			_, _ = io.WriteString(w, "data: "+payload+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

// reasoningOf reads the reasoning knob off one recorded body: the level, or ""
// when the field was never sent at all.
func (s *reasoningServer) reasoningOf(t *testing.T, index int) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.bodies) {
		t.Fatalf("request %d was never made; the server saw %d", index, len(s.bodies))
	}
	raw, ok := s.bodies[index]["reasoning"]
	if !ok {
		return ""
	}
	var knob struct {
		Effort  string `json:"effort"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &knob); err != nil {
		t.Fatalf("request %d reasoning: %v (%s)", index, err, raw)
	}
	if knob.Enabled != nil && !*knob.Enabled {
		t.Fatalf("request %d disabled reasoning outright; off must send NOTHING", index)
	}
	return knob.Effort
}

func (s *reasoningServer) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

// answerOK is one step that just answers.
var answerOK = []string{`{"choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`}

// askTool is one step that calls a tool nobody has, which is answered "Unknown
// tool" and sends the turn round for a SECOND request — the cheapest way to get
// two provider calls inside one turn.
var askTool = []string{
	`{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"nope","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
}

func reasoningAgent(t *testing.T, server *reasoningServer, model string) *Agent {
	t.Helper()
	agent, err := New(Config{
		Workspace: t.TempDir(),
		Model:     model,
		APIKey:    "test",
		BaseURL:   server.URL,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

func drainTurn(t *testing.T, agent *Agent, text string) {
	t.Helper()
	ctx, cancel := deadline(5 * time.Second)
	defer cancel()
	events, err := agent.Submit(ctx, text)
	if err != nil {
		t.Fatalf("Submit(%q): %v", text, err)
	}
	for event := range events {
		if event.Kind == EventError {
			t.Fatalf("turn errored: %v", event.Err)
		}
	}
}

// The knob reaches the wire with the level on it, and vanishes when the level
// goes back to off — ABSENT, not {"enabled":false}. Off here is "let the model
// think however it thinks", which is the request with no reasoning field at
// all; provider.EffortOff is a different instruction and not this one.
func TestTheReasoningLevelReachesTheWireAndOffSendsNothing(t *testing.T) {
	server := newReasoningServer(t, answerOK)
	agent := reasoningAgent(t, server, "vendor/model-a")

	drainTurn(t, agent, "first")
	if got := server.reasoningOf(t, 0); got != "" {
		t.Fatalf("an undialled session sent reasoning %q, want no field at all", got)
	}

	agent.SetReasoning("high")
	drainTurn(t, agent, "second")
	if got := server.reasoningOf(t, 1); got != "high" {
		t.Fatalf("wire reasoning = %q, want high — the level moved and the request did not", got)
	}

	agent.SetReasoning("off")
	drainTurn(t, agent, "third")
	if got := server.reasoningOf(t, 2); got != "" {
		t.Fatalf("off sent reasoning %q, want no field at all", got)
	}
}

// The level is latched for the whole turn, exactly as the model is: a change
// made while the agent is working lands at the next Submit and never half-way
// through the one in flight.
func TestTheReasoningLevelIsLatchedForTheTurn(t *testing.T) {
	server := newReasoningServer(t, askTool, answerOK)
	agent := reasoningAgent(t, server, "vendor/model-a")
	agent.SetReasoning("low")

	// The change lands between the turn's two requests — the moment the latch
	// exists to survive.
	server.mu.Lock()
	server.before = func(seen int) {
		if seen == 1 {
			agent.SetReasoning("high")
		}
	}
	server.mu.Unlock()

	drainTurn(t, agent, "work")
	if server.requests() != 2 {
		t.Fatalf("the turn made %d requests, want 2 (the tool call and the answer)", server.requests())
	}
	if got := server.reasoningOf(t, 1); got != "low" {
		t.Fatalf("the turn's second request carried %q, want low — a mid-turn change must wait", got)
	}

	drainTurn(t, agent, "again")
	if got := server.reasoningOf(t, 2); got != "high" {
		t.Fatalf("the next turn carried %q, want high", got)
	}
}

// The level belongs to a MODEL and not to the session: it survives a switch
// away and a switch back, and it never travels onto the model switched to.
func TestTheReasoningLevelIsKeptPerModel(t *testing.T) {
	server := newReasoningServer(t, answerOK)
	agent := reasoningAgent(t, server, "vendor/model-a")

	agent.SetReasoning("medium")
	if got := agent.Reasoning(); got != "medium" {
		t.Fatalf("Reasoning() = %q, want medium", got)
	}

	agent.SetModel("vendor/model-b")
	if got := agent.Reasoning(); got != "" {
		t.Fatalf("the model switched to reports %q, want a level of its own — which is none", got)
	}
	drainTurn(t, agent, "on b")
	if got := server.reasoningOf(t, 0); got != "" {
		t.Fatalf("model-b sent reasoning %q, want none — that level was model-a's", got)
	}

	agent.SetModel("vendor/model-a")
	if got := agent.Reasoning(); got != "medium" {
		t.Fatalf("back on model-a Reasoning() = %q, want the medium it was left at", got)
	}
	drainTurn(t, agent, "on a")
	if got := server.reasoningOf(t, 1); got != "medium" {
		t.Fatalf("model-a sent reasoning %q, want medium", got)
	}

	// A level can be set for a model nobody has switched to — which is what a
	// picker does to the row under the cursor — and it is waiting when the
	// switch happens.
	agent.SetReasoningFor("vendor/model-c", "high")
	// And it is found again however the id was cased, the way every other model
	// lookup on these surfaces folds it.
	if got := agent.ReasoningFor("VENDOR/MODEL-C"); got != "high" {
		t.Fatalf("ReasoningFor(%q) = %q, want high", "VENDOR/MODEL-C", got)
	}
	agent.SetModel("vendor/model-c")
	drainTurn(t, agent, "on c")
	if got := server.reasoningOf(t, 2); got != "high" {
		t.Fatalf("model-c sent reasoning %q, want the high it was dialled to before the switch", got)
	}
}

func TestParseReasoningTakesTheFourLevelsAndNothingElse(t *testing.T) {
	for _, ok := range []struct{ in, want string }{
		{"", ""}, {"off", ""}, {"OFF", ""}, {" high ", "high"},
		{"low", "low"}, {"medium", "medium"},
	} {
		got, valid := ParseReasoning(ok.in)
		if !valid || got != ok.want {
			t.Fatalf("ParseReasoning(%q) = %q, %v; want %q, true", ok.in, got, valid, ok.want)
		}
	}
	for _, bad := range []string{"highest", "none", "1", "minimal"} {
		if _, valid := ParseReasoning(bad); valid {
			t.Fatalf("ParseReasoning(%q) was accepted; a typo must be a usage error", bad)
		}
	}
	// An unrecognized level leaves the one that was set alone rather than
	// clearing it: the setter's two callers cannot produce one, so a value this
	// does not know is a bug upstream and dropping a level would hide it.
	server := newReasoningServer(t, answerOK)
	agent := reasoningAgent(t, server, "vendor/model-a")
	agent.SetReasoning("high")
	agent.SetReasoning("higher")
	if got := agent.Reasoning(); got != "high" {
		t.Fatalf("after a bad level the model reads %q, want the high it was set to", got)
	}
}

// An ordinary profile must reach the real request without a reasoning override,
// while a saved explicit level must still win after the shipped default changes.
func TestProfileDefaultReasoningReachesTheWire(t *testing.T) {
	for _, choice := range []string{"unset", "auto", "high"} {
		t.Run(choice, func(t *testing.T) {
			profile := t.TempDir()
			if choice != "unset" {
				rung, _ := effort.Parse(choice)
				if err := cfgstore.WriteDefaultEffort(profile, rung); err != nil {
					t.Fatal(err)
				}
			}
			server := newReasoningServer(t, askTool, answerOK)
			agent, err := New(Config{Workspace: t.TempDir(), Model: "vendor/default-reasoning", APIKey: "test", BaseURL: server.URL, DefaultEffort: cfgstore.DefaultEffortAt(profile)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })
			drainTurn(t, agent, "finish the task")
			if server.requests() != 2 {
				t.Fatalf("want both tool and answer requests, got %d", server.requests())
			}
			for i := 0; i < server.requests(); i++ {
				if choice == "high" {
					if got := server.reasoningOf(t, i); got != "high" {
						t.Fatalf("explicit high sent %q", got)
					}
					continue
				}
				server.mu.Lock()
				for _, field := range []string{"reasoning", "reasoning_effort", "include_reasoning"} {
					if _, present := server.bodies[i][field]; present {
						t.Errorf("%s request %d forced %s", choice, i, field)
					}
				}
				server.mu.Unlock()
			}
		})
	}
}
