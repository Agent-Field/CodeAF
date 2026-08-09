package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
)

// A declared bundle never meets the planner: the measured failure was three
// independent requests laid end to end by a spine that had just restated the
// independence rule, at 230 wall seconds against pi's 23. The compile call
// judges independence; the graph is then geometry, and the only structuring
// these jobs buy is their working methods.
func TestADeclaredBundleNeverMeetsThePlanner(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	capture := &planScriptClient{model: "worker/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	subtree, err := planSubtree(settings, client, client, plans, graph)(context.Background(), resident.Compiled{
		Goal:  "Deliver three independent results.",
		Scale: head.ScaleProject,
		Parts: []string{
			"Fix the failing test in the fixtures repository.",
			"Compute March revenue from orders.csv.",
			"Draft the sponsorship decline email.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []provider.CallClass{
		provider.ClassPlanSpine, provider.ClassPlanFanOut, provider.ClassPlanBrief,
	} {
		if calls := capture.keysFor(class); len(calls) != 0 {
			t.Errorf("a declared bundle bought %d %s calls, want none", len(calls), class)
		}
	}
	if len(subtree.Nodes) != 4 {
		t.Fatalf("bundle spliced %d nodes, want three parts and their sink", len(subtree.Nodes))
	}
	roots := 0
	for _, spec := range subtree.Nodes {
		if spec.Parent == "" {
			roots++
		}
	}
	if roots != 1 {
		t.Fatalf("bundle has %d roots, want the one sink", roots)
	}
}
