package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// agentServer is a stub organization that speaks the server-agent part of the
// contract: the operator's keys and one workstream's agent, busy answering
// abir's message unless free.
type agentServer struct {
	mu     sync.Mutex
	asks   []string
	bodies []string
	busy   bool
}

func (s *agentServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/me" || r.URL.Path == "/v1/about" {
		orgJSON(w, 200, api.Me{Contract: api.Contract, Factory: api.Factory{Name: "Acme"}, Mode: api.FactoryTeam, Profile: api.Profile{Handle: "santosh"}})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asks = append(s.asks, r.Method+" "+r.URL.RequestURI())
	var body strings.Builder
	if r.Body != nil {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])
	}
	s.bodies = append(s.bodies, body.String())
	agent := map[string]any{"id": "ws_1", "state": "idle", "entries": []any{map[string]any{"seq": 1, "kind": "person", "by": "santosh", "text": "hi"}}, "seq": 1, "can_talk": true}
	switch {
	case r.URL.Path == "/v1/org/keys" && r.Method == http.MethodGet:
		orgJSON(w, 200, map[string]any{"keys": []any{map[string]any{"provider": "openrouter", "last4": "WXYZ", "set_at": "2026-11-13T00:00:00Z", "set_by": "owner"}}, "providers": []string{"openrouter"}})
	case r.URL.Path == "/v1/org/keys/openrouter" && r.Method == http.MethodPut:
		orgJSON(w, 200, map[string]any{"provider": "openrouter", "last4": "WXYZ", "set_at": "2026-11-13T00:00:00Z", "set_by": "owner"})
	case r.URL.Path == "/v1/org/keys/openrouter" && r.Method == http.MethodDelete:
		w.WriteHeader(204)
	case r.URL.Path == "/v1/workstreams/ws_1/agent" && r.Method == http.MethodGet:
		orgJSON(w, 200, agent)
	case r.URL.Path == "/v1/workstreams/ws_1/agent/messages" && r.Method == http.MethodPost:
		if s.busy {
			orgJSON(w, 409, map[string]any{"error": "the agent is still answering abir; wait for it, or stop it", "reason": "busy", "by": "abir"})
			return
		}
		agent["state"], agent["for"] = "working", "santosh"
		orgJSON(w, 202, agent)
	case r.URL.Path == "/v1/workstreams/ws_1/agent/stop" && r.Method == http.MethodPost:
		agent["state"], agent["for"] = "stopping", "santosh"
		orgJSON(w, 200, agent)
	default:
		orgRefuse(w, 404, "factory endpoint not found")
	}
}

// THE SERVER AGENT'S METHODS: the engine says it serves them, carries the
// key in and never out, reads and talks to the agent, and a message while the
// agent answers someone else names them in the refusal's detail.
func TestOrgWorkstreamAgentForwardsToTheServer(t *testing.T) {
	s := &agentServer{}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	id := conn.ID
	if !strings.Contains(wire.hello, `"org_agent_server"`) {
		t.Fatalf("hello lacks org_agent_server: %s", wire.hello)
	}
	var keys appwire.OrgKeys
	wire.must("factory.org_keys", map[string]any{"connection": id}, &keys)
	if len(keys.Keys) != 1 || keys.Keys[0].Last4 != "WXYZ" {
		t.Fatalf("keys %+v", keys)
	}
	var key appwire.OrgKey
	wire.must("factory.org_key_set", map[string]any{"connection": id, "provider": "openrouter", "key": "sk-or-v1-0123456789WXYZ"}, &key)
	wire.mustFail("factory.org_key_set", map[string]any{"connection": id, "provider": "openrouter", "key": "two words"}, appwire.CodeBadParams)
	wire.must("factory.org_key_delete", map[string]any{"connection": id, "provider": "openrouter"}, nil)

	ws := map[string]any{"connection": id, "id": "ws_1"}
	var agent appwire.OrgWorkstreamAgent
	wire.must("factory.org_workstream_agent", map[string]any{"connection": id, "id": "ws_1", "since": 4}, &agent)
	if agent.State != "idle" || len(agent.Entries) != 1 || !agent.CanTalk {
		t.Fatalf("read %+v", agent)
	}
	wire.must("factory.org_workstream_agent_send", map[string]any{"connection": id, "id": "ws_1", "text": "write the runbook"}, &agent)
	if agent.State != "working" || agent.For != "santosh" {
		t.Fatalf("sent %+v", agent)
	}
	wire.mustFail("factory.org_workstream_agent_send", map[string]any{"connection": id, "id": "ws_1", "text": "  "}, appwire.CodeBadParams)
	s.mu.Lock()
	s.busy = true
	s.mu.Unlock()
	raw := rawAnswer(wire, "factory.org_workstream_agent_send", map[string]any{"connection": id, "id": "ws_1", "text": "me too"})
	if !strings.Contains(raw, `"reason":"busy"`) || !strings.Contains(raw, `"by":"@abir"`) || !strings.Contains(raw, "still answering abir") {
		t.Fatalf("a busy agent answered %s", raw)
	}
	wire.must("factory.org_workstream_agent_stop", ws, &agent)
	if agent.State != "stopping" {
		t.Fatalf("stopped %+v", agent)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	want := []string{"GET /v1/org/keys", "PUT /v1/org/keys/openrouter", "DELETE /v1/org/keys/openrouter", "GET /v1/workstreams/ws_1/agent?since=4",
		"POST /v1/workstreams/ws_1/agent/messages", "POST /v1/workstreams/ws_1/agent/messages", "POST /v1/workstreams/ws_1/agent/stop"}
	if strings.Join(s.asks, "|") != strings.Join(want, "|") {
		t.Fatalf("asks %v", s.asks)
	}
	if s.bodies[1] != `{"key":"sk-or-v1-0123456789WXYZ"}` || s.bodies[4] != `{"text":"write the runbook"}` {
		t.Fatalf("bodies %q", s.bodies)
	}
}

// THE AGENT'S LINES AND STATE REACH THE DESK, with the connection.
func TestOrgWorkstreamAgentEventsAreRelayed(t *testing.T) {
	o, wire, id, _ := orgRelay(t)
	heard := make(chan appwire.FactoryOrgWorkstreamAgent, 4)
	h := wire.backend.factories
	h.mu.Lock()
	h.orgAgent = func(e appwire.FactoryOrgWorkstreamAgent) { heard <- e }
	h.mu.Unlock()
	var teams appwire.OrgTeams
	wire.must("factory.teams", map[string]any{"connection": id}, &teams)
	nextOpening(t, o)
	o.frames <- "id: 3\nevent: agent.entry\ndata: {\"id\":\"ws_2\",\"entry\":{\"seq\":7,\"at\":\"2026-11-13T00:00:00Z\",\"kind\":\"agent\",\"by\":\"mia\",\"text\":\"Done.\",\"turn\":\"turn_1\"}}\n\n"
	o.frames <- "id: 4\nevent: agent.state\ndata: {\"id\":\"ws_2\",\"state\":\"idle\"}\n\n"
	for i, want := range []func(appwire.FactoryOrgWorkstreamAgent) bool{
		func(e appwire.FactoryOrgWorkstreamAgent) bool {
			return e.Entry != nil && e.Entry.Seq == 7 && e.Entry.Text == "Done." && e.Entry.By == "mia" && e.State == nil
		},
		func(e appwire.FactoryOrgWorkstreamAgent) bool { return e.State != nil && e.State.State == "idle" && e.Entry == nil },
	} {
		select {
		case e := <-heard:
			if e.Connection != id || e.ID != "ws_2" || !want(e) {
				t.Fatalf("relayed %d: %+v", i, e)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("event %d never arrived", i)
		}
	}
}
