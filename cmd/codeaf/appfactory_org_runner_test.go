package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// runnerServer is a stub organization that speaks the agent-runner part of
// the contract: the setting in /v1/settings, one workstream's driver hand
// (held by abir unless free), and E2's spend route.
type runnerServer struct {
	mu      sync.Mutex
	asks    []string
	keys    []string
	held    string
	choice  string
	spent   float64
	spendOK bool
}

func (s *runnerServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/me" || r.URL.Path == "/v1/about" {
		orgJSON(w, 200, api.Me{Contract: api.Contract, Factory: api.Factory{Name: "Acme"}, Mode: api.FactoryTeam, Profile: api.Profile{Handle: "santosh"}})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asks = append(s.asks, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == "/v1/settings" && r.Method == http.MethodGet:
		orgJSON(w, 200, map[string]any{"runner_timeout_s": 60, "agent_runner": s.choice, "agent_runners": []string{"driver"}})
	case r.URL.Path == "/v1/settings" && r.Method == http.MethodPatch:
		orgRefuse(w, 403, "only the operator can do this")
	case r.URL.Path == "/v1/workstreams/ws_1/driver" && r.Method == http.MethodPut:
		if s.held != "" && s.held != "santosh" {
			orgJSON(w, 409, map[string]any{"error": s.held + " is driving this workstream's agent", "driver": map[string]any{"handle": s.held, "ttl_s": 90}})
			return
		}
		s.held = "santosh"
		orgJSON(w, 200, map[string]any{"handle": "santosh", "since": "2026-11-13T00:00:00Z", "until": "2026-11-13T00:01:30Z", "ttl_s": 90})
	case r.URL.Path == "/v1/workstreams/ws_1/driver" && r.Method == http.MethodDelete:
		s.held = ""
		w.WriteHeader(204)
	case r.URL.Path == "/v1/workstreams/ws_1/spend" && r.Method == http.MethodPost && s.spendOK:
		s.keys = append(s.keys, r.Header.Get("Idempotency-Key"))
		s.spent += 0.25
		orgJSON(w, 200, map[string]any{"spend_usd": s.spent, "budget_usd": nil})
	default:
		orgRefuse(w, 404, "factory endpoint not found")
	}
}

func attachRunner(t *testing.T, s *runnerServer) (*factoryWire, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	return wire, conn.ID
}

// THE AGENT RUNNER: the engine says it serves the methods, reads the choice
// (an older server's silence reads as driver), passes the operator's refusal
// on, and a drive someone else holds names them in the refusal's detail.
func TestOrgAgentRunnerAndDriverForwardToTheServer(t *testing.T) {
	s := &runnerServer{held: "abir"}
	wire, id := attachRunner(t, s)
	if !strings.Contains(wire.hello, `"org_agent_runner"`) {
		t.Fatalf("hello lacks org_agent_runner: %s", wire.hello)
	}
	c := map[string]any{"connection": id}
	var got appwire.OrgAgentRunner
	wire.must("factory.org_agent_runner", c, &got)
	if got.AgentRunner != "driver" || len(got.AgentRunners) != 1 {
		t.Fatalf("read %+v", got)
	}
	wire.mustFail("factory.org_agent_runner_set", map[string]any{"connection": id, "agent_runner": "driver"}, appwire.CodeForbidden)
	wire.mustFail("factory.org_agent_runner_set", map[string]any{"connection": id, "agent_runner": "cloud"}, appwire.CodeBadParams)

	ws := map[string]any{"connection": id, "id": "ws_1"}
	raw := rawAnswer(wire, "factory.org_workstream_drive", ws)
	if !strings.Contains(raw, `"reason":"driving"`) || !strings.Contains(raw, `"by":"@abir"`) || !strings.Contains(raw, "abir is driving") {
		t.Fatalf("a held hand answered %s", raw)
	}
	s.mu.Lock()
	s.held = ""
	s.mu.Unlock()
	var hand appwire.OrgWorkstreamDriver
	wire.must("factory.org_workstream_drive", ws, &hand)
	if hand.Handle != "santosh" || hand.TTL != 90 {
		t.Fatalf("took %+v", hand)
	}
	wire.must("factory.org_workstream_let_go", ws, nil)
	wire.mustFail("factory.org_workstream_drive", map[string]any{"connection": id, "id": "../x"}, appwire.CodeBadParams)
}

// SPEND IS POSTED ONCE: the key travels as Idempotency-Key, a figure that is
// not spend is refused before it leaves, and a server without the route
// answers not_found.
func TestOrgWorkstreamSpendIsPostedWithItsKey(t *testing.T) {
	s := &runnerServer{}
	wire, id := attachRunner(t, s)
	spend := func(usd float64, key string) map[string]any {
		return map[string]any{"connection": id, "id": "ws_1", "usd": usd, "chat": "agent-1", "key": key}
	}
	wire.mustFail("factory.org_workstream_spend", spend(0.25, "k1"), appwire.CodeNotFound)
	s.mu.Lock()
	s.spendOK = true
	s.mu.Unlock()
	var out appwire.OrgWorkstreamSpend
	wire.must("factory.org_workstream_spend", spend(0.25, "k1"), &out)
	if out.SpendUSD != 0.25 || out.BudgetUSD != nil {
		t.Fatalf("spend answered %+v", out)
	}
	wire.mustFail("factory.org_workstream_spend", spend(0, "k2"), appwire.CodeBadParams)
	wire.mustFail("factory.org_workstream_spend", spend(0.1, ""), appwire.CodeBadParams)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.keys) != 1 || s.keys[0] != "k1" {
		t.Fatalf("keys sent = %v", s.keys)
	}
}

// rawAnswer is the engine's whole answer line, refusal detail included.
func rawAnswer(w *factoryWire, method string, params any) string {
	w.t.Helper()
	w.next++
	data, _ := json.Marshal(params)
	fmt.Fprintf(w.in, `{"id":%d,"method":%q,"params":%s}`+"\n", w.next, method, data)
	for line := range w.answers {
		var f struct {
			ID *int `json:"id"`
		}
		if json.Unmarshal(line, &f) == nil && f.ID != nil && *f.ID == w.next {
			return string(line)
		}
	}
	w.t.Fatalf("the wire ended during %s", method)
	return ""
}
