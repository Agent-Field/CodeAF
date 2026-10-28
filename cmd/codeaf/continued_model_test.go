package main

import (
	"strings"
	"testing"

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
	return session.Config{Workspace: work, Place: session.Place{Dir: c.Root, Workspace: work}, SessionFile: session.Place{Dir: c.Root}.Transcript(), Model: "ollama/llama3.2:latest"}
}

func TestChatC1NoUsableModelExplainsSetup(t *testing.T) {
	cfg := incomingModelConfig(t)
	work := cfg.Workspace
	agent, _, _, err := openV3Agent(cfg, work, session.New)
	if agent != nil {
		closeV3Agent(agent)
	}
	if err == nil || !strings.Contains(err.Error(), "isn't set up here") || !strings.Contains(err.Error(), "Set up a model in Settings") {
		t.Fatalf("C1 no usable model did not explain setup: %v", err)
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
