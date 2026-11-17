package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

func TestHarnessPlannerUsesSharedWriteScopes(t *testing.T) {
	d := &driver{}
	d.add([]orchestrate.Node{
		{ID: "bad", Goal: "write report", WriteScope: []string{"../*.md"}},
		{ID: "first", Goal: "write report", WriteScope: []string{"crew-0-*.md"}},
		{ID: "second", Goal: "write report", WriteScope: []string{"crew-0-a.md"}},
	})
	if len(d.refused) != 1 || !strings.Contains(d.refused[0], "write_scope") || !strings.Contains(d.refused[0], "../*.md") {
		t.Fatalf("planner did not receive a repairable scope refusal: %v", d.refused)
	}
	if len(d.nodes) != 2 || len(d.nodes[1].Needs) != 1 || d.nodes[1].Needs[0] != "first" {
		t.Fatalf("only valid, serialized scopes may land: %+v", d.nodes)
	}
}
