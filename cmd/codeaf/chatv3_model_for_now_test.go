package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// C1, C2, C4: refreshing linked-local provider sources must not confirm the
// temporary model as a saved choice before catalog reconciliation can restore it.
func TestLinkedLocalSourceRefreshKeepsRunOnlyModelTemporary(t *testing.T) {
	dir, profile := t.TempDir(), t.TempDir()
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Place: session.Place{Dir: dir}, Model: "vendor/configured", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: agent, RefreshModelSources: func() { agent.SetSources(modelsource.Set{}) }}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().UseModelForNow("available/model"); err != nil {
		t.Fatal(err)
	}
	options := tui3.Options{Agent: loop.Client.Agent(), ProfileDir: profile}
	localDoors(&options, remote.Welcome{ProfileDir: profile}, config.Config{ProfileDir: profile})
	options.ApplyModelSources(modelsource.Set{})
	agent.SettleWrites()
	meta, err := session.LoadMeta(dir)
	if err != nil || meta.Model != "vendor/configured" || agent.Model() != "available/model" || loop.Client.Agent().ConfiguredModel() != "vendor/configured" {
		t.Fatalf("source refresh persisted the run-only model: %+v, %v", meta, err)
	}
	if config.ChatModelAt(profile) != "" {
		t.Fatal("source refresh wrote a talk preference")
	}
}
