package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
)

// latePlane is a control plane that takes a registration only with its API
// key, and offers no link, so the lease runs over HTTP.
type latePlane struct {
	*httptest.Server
	mu       sync.Mutex
	refused  int
	accepted []agentfield.Registration
}

func newLatePlane(t *testing.T, key string) *latePlane {
	p := &latePlane{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-API-Key") != key {
			if r.Method == http.MethodPost && r.URL.Path == "/api/v1/nodes" {
				p.mu.Lock()
				p.refused++
				p.mu.Unlock()
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/nodes":
			var reg agentfield.Registration
			_ = json.NewDecoder(r.Body).Decode(&reg)
			p.mu.Lock()
			p.accepted = append(p.accepted, reg)
			p.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"success":true}`))
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{"lease_seconds":300}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *latePlane) counts() (refused, accepted int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refused, len(p.accepted)
}

// A KEY THE APP HANDS OVER LATE STILL CONNECTS THE FACTORY. The desk waits
// only a few seconds for the keychain at launch, so the factory can start
// connected to AgentField (its settings are on disk) with no key held, and
// its first registration is refused. The keys.set that arrives afterwards
// restarts the node with the key, once: the next registration is taken, and
// handing the same key over again changes nothing.
func TestALateAgentFieldKeyRegistersTheRunningFactory(t *testing.T) {
	s := newLocalSetup(t)
	plane := newLatePlane(t, "af-late-key-1")
	s.start()
	s.backend.local.mu.Lock()
	host := s.backend.local.host
	s.backend.local.mu.Unlock()
	if host == nil {
		t.Fatal("the factory on this machine did not start")
	}
	node := host.agentFieldNode()
	settings, err := agentfield.Clean(agentfield.Settings{URL: plane.URL})
	if err != nil {
		t.Fatal(err)
	}
	// What a launch leaves behind: the settings kept, the keys not yet back.
	if err := node.Conn.Keep(settings, "", ""); err != nil {
		t.Fatal(err)
	}
	host.startAgentField()
	t.Cleanup(node.Stop)
	waitLate(t, "the keyless registration to be refused", func() bool { r, _ := plane.counts(); return r > 0 })

	s.must("keys.set", map[string]any{"provider": config.AppAgentFieldProvider, "key": "af-late-key-1"}, nil)
	waitLate(t, "the registration with the late key", func() bool { _, a := plane.counts(); return a > 0 })
	if !node.Running() {
		t.Fatal("the node is not running after the late key")
	}
	if h := node.Health(); h.Registered.IsZero() {
		t.Fatalf("the node does not say it registered: %+v", h)
	}

	// THE SAME KEY AGAIN IS NO NEW START: no second instance registers.
	s.must("keys.set", map[string]any{"provider": config.AppAgentFieldProvider, "key": "af-late-key-1"}, nil)
	time.Sleep(300 * time.Millisecond)
	if _, a := plane.counts(); a != 1 {
		t.Fatalf("the same key handed over again registered %d times", a)
	}
}

func waitLate(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
