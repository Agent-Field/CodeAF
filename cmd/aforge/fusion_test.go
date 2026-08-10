package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The compile call writes the task-scale working method in the same reply that
// names the goal, so the separate contract pass — one paid structuring call,
// serialized on the critical path of every single-worker job — is a fallback
// and not the path. Measured against the benchmark's atomic tier, this is the
// difference between a 7-call floor and a 6-call one.
func TestACompiledMethodCostsNoSecondCall(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	capture := &planScriptClient{model: "worker/model"}
	client := adoptLiveClient(settings, capture.model, capture)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	const method = "Read the diff first. Done means the note names every migration step."
	subtree, err := planSubtree(settings, client, client, plans, graph)(context.Background(), resident.Compiled{
		Goal: "write the note that announces the change", Scale: head.ScaleTask, Contract: method,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(subtree.Nodes) != 1 {
		t.Fatalf("a task-scale ask produced %d nodes, want the one leaf", len(subtree.Nodes))
	}
	if calls := capture.keysFor(provider.ClassPlanContract); len(calls) != 0 {
		t.Fatalf("a compiled method still bought %d contract calls, want none", len(calls))
	}
	leaf := store.Node{ID: subtree.Nodes[0].ID}
	if got := leafContract(plans, nil, leaf); !strings.Contains(got, "every migration step") {
		t.Fatalf("the leaf does not carry the compiled method: %q", got)
	}
}
