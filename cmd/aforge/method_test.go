package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/revision"
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
	client := adoptLiveClient(settings, capture.model, capture)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	const goal = "write the note that announces the change"
	subtree, err := planSubtree(settings, client, client, plans, graph, "")(context.Background(), resident.Compiled{
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

	// The leaf that runs is the one the method was written for, and it reads
	// the same method every time it is asked. It used to be handed over exactly
	// once, out of a map a restart emptied — so a process that died between the
	// splice and the leaf ran the generic loop with no method at all. The method
	// now rides the leaf's own spec, which is durable, and reading it twice is
	// the proof rather than the defect.
	leaf := store.Node{ID: subtree.Nodes[0].ID, Spec: subtree.Nodes[0].Spec}
	contract := leafContract(plans, nil, leaf)
	if !strings.Contains(contract, "Read the diff") {
		t.Fatalf("the task-scale leaf carries no working method: %q", contract)
	}
	if again := leafContract(plans, nil, leaf); again != contract {
		t.Errorf("the method did not survive a second read: %q, want %q", again, contract)
	}
}

// A lookup is a question with an answer. Buying a call to say that the method
// for answering a question is to answer it would break the same proportionality
// this path exists to hold.
func TestLookupScaleBuysNoWorkingMethod(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	capture := &planScriptClient{model: "worker/model"}
	client := adoptLiveClient(settings, capture.model, capture)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	subtree, err := planSubtree(settings, client, client, plans, graph, "")(context.Background(), resident.Compiled{
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

// The deliverable owner, end to end. On a planned job the gated node is the
// plan's gathering node, and it used to reach the executor and the gate holding
// a two-line harness stub — so the one node whose output IS the answer was the
// one node with no statement of what done means. It now carries a method like
// any other leaf, and the gate is handed the same one the worker was held to.
func TestThePlannedDeliverableOwnerCarriesTheMethodTheGateReads(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model", MaxDepth: 0, NodeBudget: 6}
	capture := &planScriptClient{
		model:    "worker/model",
		parts:    `{"parts":[{"title":"Read","summary":"Read the diff."},{"title":"Weigh","summary":"Weigh the risks."}]}`,
		contract: `{"contract":"State the verdict in the first line, then the evidence under it."}`,
	}
	client := adoptLiveClient(settings, capture.model, capture)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	subtree, err := planSubtree(settings, client, client, plans, graph, "")(context.Background(), resident.Compiled{
		Goal:  "review the pull request and deliver REVIEW.md",
		Scale: head.ScaleProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := subtreeSink(subtree)
	if root == "" || len(subtree.Nodes) < 3 {
		t.Fatalf("planned subtree = %d nodes with root %q, want the two leaves and their gathering node", len(subtree.Nodes), root)
	}
	// The gathering node is the subtree's own root — the node the delivery gate
	// fires on — and it is written for like the leaves under it.
	if calls := capture.keysFor(provider.ClassPlanContract); len(calls) != 3 {
		t.Fatalf("contract calls = %d, want one per leaf and one for the deliverable owner", len(calls))
	}
	_, _, planNode, _, _ := plans.lookup(root)
	if planNode == nil || strings.TrimSpace(planNode.Contract) == "" {
		t.Fatal("the deliverable owner still runs on the generic loop")
	}
	var rootSpec store.NodeSpec
	for _, spec := range subtree.Nodes {
		if spec.ID == root {
			rootSpec = spec
		}
	}
	if strings.Contains(rootSpec.Brief, "Assemble the finished answer to the goal from every result") {
		t.Fatalf("the deliverable owner reached the store with the harness stub: %q", rootSpec.Brief)
	}

	// And the gate is handed that method, above the deliverable so a repair pass
	// still moves the deliverable first.
	node := store.Node{ID: root, Brief: rootSpec.Brief, Provenance: store.Provenance{Intent: "review the pull request", SessionID: "s1"}}
	gate := &gateCaptureClient{model: "worker/model"}
	revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, gate.model, gate), graph, node,
		"the review", leafContract(plans, planNode, node), revision.Evidence{}, "worker/model")
	body := gate.messages[len(gate.messages)-1].Content[0].Text
	method := strings.Index(body, "The working method this deliverable was held to:\n")
	deliverable := strings.Index(body, "Deliverable as produced:\n")
	if method < 0 || !strings.Contains(body, "State the verdict in the first line") {
		t.Fatalf("the gate was not handed the working method:\n%s", body)
	}
	if deliverable < method {
		t.Fatalf("the method churns below the deliverable: method=%d deliverable=%d", method, deliverable)
	}
	if !strings.Contains(revision.DeliverablePrompt, "it is the only standard beside the request itself that you hold the deliverable to") {
		t.Fatal("the gate is handed the method and never told what to do with it")
	}
	// Calibration comes from the method, never from a rubric this file could
	// grow: where the method asks for nothing, nothing is missing.
	if !strings.Contains(revision.DeliverablePrompt, "Where it asks for nothing, nothing is missing") {
		t.Fatal("the gate lost the clause that keeps a small ask small")
	}
}

// A gate with no method in force says nothing about one: an empty block in
// every prompt would be paid for on every job that has none.
func TestTheGateGrowsNoEmptyMethodBlock(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	node := store.Node{ID: "job", Brief: "produce it", Provenance: store.Provenance{Intent: "produce it"}}
	capture := &gateCaptureClient{model: "worker/model"}
	revision.JudgeDeliverable(context.Background(), settings,
		adoptLiveClient(settings, capture.model, capture), graph, node,
		"done", "  \n ", revision.Evidence{}, "worker/model")
	if got := capture.messages[len(capture.messages)-1].Content[0].Text; strings.Contains(got, "The working method") {
		t.Errorf("an empty method block reached the gate:\n%s", got)
	}
}
