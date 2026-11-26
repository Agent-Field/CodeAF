package main

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
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
