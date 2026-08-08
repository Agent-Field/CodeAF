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

// The single highest-leverage gap the product had: a task-scale ask is spliced
// as one leaf and never went near the planner, so the per-leaf working method —
// what done means in the person's terms, how it is verified, where to stop —
// simply never ran for the shape of work the product handles most. It is one
// call, on the plan slot, under the job's own affinity key.
func TestTaskScaleLeafCarriesAWorkingMethod(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	capture := &planScriptClient{model: "worker/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	const goal = "write the note that announces the change"
	subtree, err := planSubtree(settings, client, client, plans, graph)(context.Background(), resident.Compiled{
		Goal: goal, Scale: head.ScaleTask,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(subtree.Nodes) != 1 {
		t.Fatalf("a task-scale ask produced %d nodes, want the one leaf", len(subtree.Nodes))
	}

	// Exactly one call, and it is the contract pass: proportionality applies to
	// the fix as much as to the plan.
	contracts := capture.keysFor(provider.ClassPlanContract)
	if len(contracts) != 1 {
		t.Fatalf("contract calls = %d, want exactly 1", len(contracts))
	}
	if want := provider.RunCacheKey(goal, settings.Model); contracts[0] != want {
		t.Errorf("contract cache key = %q, want the job's run key %q", contracts[0], want)
	}
	for _, class := range []provider.CallClass{
		provider.ClassPlanSpine, provider.ClassPlanFanOut, provider.ClassPlanBrief,
	} {
		if calls := capture.keysFor(class); len(calls) != 0 {
			t.Errorf("a task-scale ask bought %d %s calls, want none", len(calls), class)
		}
	}

	// The leaf that runs is the one the method was written for, and it is
	// handed over exactly once.
	leaf := store.Node{ID: subtree.Nodes[0].ID}
	contract := leafContract(plans, nil, leaf)
	if !strings.Contains(contract, "Read the diff") {
		t.Fatalf("the task-scale leaf carries no working method: %q", contract)
	}
	if again := leafContract(plans, nil, leaf); again != "" {
		t.Errorf("the method was handed out twice: %q", again)
	}
}

// A lookup is a question with an answer. Buying a call to say that the method
// for answering a question is to answer it would break the same proportionality
// this path exists to hold.
func TestLookupScaleBuysNoWorkingMethod(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	capture := &planScriptClient{model: "worker/model"}
	client := &liveClient{settings: settings, model: capture.model, client: capture}
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	subtree, err := planSubtree(settings, client, client, plans, graph)(context.Background(), resident.Compiled{
		Goal: "say what the current total is", Scale: head.ScaleLookup,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(subtree.Nodes) != 1 {
		t.Fatalf("a lookup produced %d nodes, want the one leaf", len(subtree.Nodes))
	}
	if calls := capture.keysFor(provider.ClassPlanContract); len(calls) != 0 {
		t.Fatalf("a lookup bought %d contract calls, want none", len(calls))
	}
	if contract := leafContract(plans, nil, store.Node{ID: subtree.Nodes[0].ID}); contract != "" {
		t.Errorf("a lookup leaf carries a working method: %q", contract)
	}
}
