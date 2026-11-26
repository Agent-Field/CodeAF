package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestAppAIRunsMatchTheirExplicitScreenOrRecord(t *testing.T) {
	run := runs.Run{Subject: &runs.Subject{Type: "mention", ID: "one"}}
	if appAIMatches(appwire.AppAgentQuery{Screen: "mentions"}, run) {
		t.Fatal("a run without a screen appeared on a screen")
	}
	if !appAIMatches(appwire.AppAgentQuery{Record: &appwire.DataRecordRef{Type: "mention", ID: "one"}}, run) {
		t.Fatal("the run did not appear on its record")
	}
	run.Screen = "mentions"
	if !appAIMatches(appwire.AppAgentQuery{Screen: "mentions"}, run) || appAIMatches(appwire.AppAgentQuery{Screen: "posts"}, run) {
		t.Fatal("the run appeared on the wrong screen")
	}
}

func TestAppMCPMarkValidatesTheKeptScreenAddress(t *testing.T) {
	raw := json.RawMessage(`{"body":{"children":[{"id":"list"}]}}`)
	target := builder.Target{Screen: "mentions", Node: "list", Path: []any{"body", "children", float64(0)}}
	if !appMCPMarkTarget(raw, target) {
		t.Fatal("the declared node could not be marked")
	}
	target.Path[2] = float64(1)
	if appMCPMarkTarget(raw, target) {
		t.Fatal("an out-of-range node could be marked")
	}
	target.Path[2] = float64(0)
	target.Node = "other"
	if appMCPMarkTarget(raw, target) {
		t.Fatal("a different node could be marked through the path")
	}
}

func TestAppAgentWireSteersTheSelectedRunAndClearsPresenceOnStop(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	meta := installAppAssistantCRM(t)
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	defer b.Close()
	files := runs.Files(home.Join("apps"))
	var prompt string
	engine, err := runs.Open(runs.Config{
		Root: filepath.Join(root, "test-runs"),
		Load: func(app, id string) (runs.Definition, error) { return b.loadAppAgentRun(files, app, id) },
		Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
			prompt = call.Prompt
			return runs.Reply{Output: "Answer"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	b.appRuns.Store(engine)
	wire := wireOver(t, b)
	addr := &appwire.DataRecordRef{Type: "deal", ID: "record-1"}
	ask := appwire.AppAgentAsk{App: meta.ID, Screen: "deals", Record: addr, Text: "How is it?"}
	var first, second appwire.RunStarted
	wire.must("agents.ask", ask, &first)
	wire.must("agents.ask", ask, &second)
	query := appwire.AppAgentQuery{App: meta.ID, Kind: "presence", Type: "deal"}
	var presence []appAIPresence
	wire.must("agents.query", query, &presence)
	if len(presence) != 2 || presence[0].Record.ID != addr.ID {
		t.Fatalf("active presence = %+v", presence)
	}
	wire.must("agents.steer", appwire.AppAgentRunControl{App: meta.ID, Run: first.ID, Text: "Focus on the next step."}, nil)
	wire.must("agents.stop", appwire.AppAgentRunControl{App: meta.ID, Run: second.ID}, nil)
	wire.must("agents.query", query, &presence)
	if len(presence) != 1 {
		t.Fatalf("presence after stopping = %+v", presence)
	}
	if err := engine.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Focus on the next step.") {
		t.Fatalf("steered prompt = %q", prompt)
	}
	wire.must("agents.query", query, &presence)
	if len(presence) != 0 {
		t.Fatalf("presence after completion = %+v", presence)
	}
}
