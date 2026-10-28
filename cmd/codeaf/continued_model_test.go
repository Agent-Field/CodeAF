package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

func incomingModelConfig(t *testing.T) session.Config {
	t.Helper()
	celltest.RequireEngine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv(syncsetup.URLVar, "off")
	for _, name := range config.ProviderKeyEnvNamesAt(home.Dir()) {
		t.Setenv(name, "")
	}
	if _, err := identity.Device(home.Dir()); err != nil {
		t.Fatal(err)
	}
	foreign, err := identity.Device(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := cell.Create(home.Dir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: work, Model: "ollama/llama3.2:latest"}); err != nil {
		t.Fatal(err)
	}
	engine := cellstore.EngineFor(work)
	engine.Identity = cellstore.Identity{Device: foreign.Cert.Device, Fence: 1}
	if _, err := engine.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cellstore.StopSpawned)
	return session.Config{Workspace: work, Place: session.Place{Dir: c.Root, Workspace: work}, SessionFile: session.Place{Dir: c.Root}.Transcript(), Model: "ollama/llama3.2:latest", BaseURL: "http://127.0.0.1:1"}
}

func TestChatT1UnavailableModelMakesNoNamingProviderCall(t *testing.T) {
	cfg := incomingModelConfig(t)
	var calls atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			calls.Add(1)
		}
		http.Error(w, "an unavailable model must not be called", http.StatusBadRequest)
	}))
	defer endpoint.Close()
	cfg.APIKey, cfg.BaseURL = "fixture", endpoint.URL
	if err := config.WriteChatModel(home.Dir(), cfg.Model); err != nil {
		t.Fatal(err)
	}
	agent, _, _, err := openV3Agent(cfg, cfg.Workspace, session.New)
	if err != nil {
		t.Fatal(err)
	}
	defer closeV3Agent(agent)
	events, err := agent.Submit(t.Context(), "an unnamed incoming chat")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	// Naming runs beside the turn. Let its scheduled goroutine reach the
	// fixture before checking that the refusal prevented every provider call.
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("T1 unavailable model made %d naming/provider calls", calls.Load())
	}
}

func TestChatC1T1NoUsableModelExplainsSetupOnFirstTurn(t *testing.T) {
	cfg := incomingModelConfig(t)
	work := cfg.Workspace
	agent, _, _, err := openV3Agent(cfg, work, session.New)
	if err != nil {
		t.Fatal(err)
	}
	defer closeV3Agent(agent)
	events, err := agent.Submit(t.Context(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	var reason error
	for event := range events {
		if event.Kind == session.EventError {
			reason = event.Err
		}
	}
	if reason == nil || !strings.Contains(reason.Error(), "isn't set up on this computer") || !strings.Contains(reason.Error(), "Set up a model in Settings") {
		t.Fatalf("C1/T1 no usable model did not explain setup: %v", reason)
	}
}

func TestChatC1ExplicitUsableModelKeepsLaunchPrecedence(t *testing.T) {
	cfg := incomingModelConfig(t)
	if err := config.WriteChatModel(home.Dir(), "profile/default"); err != nil {
		t.Fatal(err)
	}
	cfg.Model, cfg.APIKey, cfg.BaseURL = "chosen/model", "fixture", "http://127.0.0.1:1"
	agent, _, notice, err := openV3Agent(cfg, cfg.Workspace, session.New)
	if err != nil {
		t.Fatal(err)
	}
	defer closeV3Agent(agent)
	if agent.Model() != "chosen/model" || notice != "" {
		t.Fatalf("C1 explicit usable choice changed: %q, %q", agent.Model(), notice)
	}
}
