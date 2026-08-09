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

// The merge of declared-independent parts is assembly, not judgment: with a
// silent board and clean parts, the sink's delivery is the parts joined in
// the asked order and no model is consulted. Measured before this existed: a
// sink re-typing a 1,863-word part spent 121 of a 212-second job saying what
// the parts had already said. A board note or a dirty part hands the merge
// back to the ordinary sink leaf.
func TestABundleSinkWithNothingToReconcileIsAssembledByCode(t *testing.T) {
	graph := openCacheStore(t)
	provenance := store.Provenance{SessionID: "bundle", Origin: store.OriginUser, Intent: "three things"}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "deliver together", Group: resident.BundleGroup},
		{ID: "job-n1", Parent: "job", Brief: "part one", Stage: 1},
		{ID: "job-n2", Parent: "job", Brief: "part two", Stage: 1},
	}}, provenance); err != nil {
		t.Fatal(err)
	}
	land := func(id, summary string) {
		claim, won, err := graph.Claim(id, "w")
		if err != nil || !won {
			t.Fatalf("claim %s: %v", id, err)
		}
		if err := graph.Complete(claim, summary); err != nil {
			t.Fatal(err)
		}
	}
	land("job-n1", "The guide, in full.")

	sink, _, err := graph.Node("job")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := assembledBundle(graph, sink); ok {
		t.Fatal("a bundle with an unfinished part was assembled anyway")
	}
	land("job-n2", "The totals: £50,818.17.")
	joined, ok := assembledBundle(graph, sink)
	if !ok {
		t.Fatal("clean parts and a silent board were not assembled")
	}
	if joined != "The guide, in full.\n\nThe totals: £50,818.17." {
		t.Fatalf("assembly lost the asked order or the content:\n%s", joined)
	}

	// One board note means a worker learned something the parts may not all
	// reflect — the merge needs the model after all.
	if _, err := graph.PostMessage(store.Message{
		SessionID: "bundle", Role: store.RoleAgent, NodeID: "job",
		Body: jobNoteBody("part two", "amounts were in cents"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := assembledBundle(graph, sink); ok {
		t.Fatal("a board note did not hand the merge back to the model")
	}
}

// A timeout mid-split used to deliver the split receipt as the FINAL ANSWER —
// "[splitting the remaining work — 6 pieces queued]" scored against GAIA
// ground truth. A receipt about scheduling is never an answer.
func TestATimeoutMidSplitNeverDeliversTheReceipt(t *testing.T) {
	graph := openCacheStore(t)
	provenance := store.Provenance{SessionID: "gaia", Origin: store.OriginUser, Intent: "the question"}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "q", Brief: "answer the question"},
	}}, provenance); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("q", "w")
	if err != nil || !won {
		t.Fatal(err)
	}
	receipt := "partial work so far [" + resident.OverrunContinuationMessage(6) + "]"
	if err := graph.Complete(claim, receipt); err != nil {
		t.Fatal(err)
	}
	watch := &settlementWatch{graph: graph}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	outcome := watch.compose(nodes)
	if strings.Contains(outcome.Deliverable, "splitting the remaining work") {
		t.Fatalf("the split receipt shipped as the answer:\n%s", outcome.Deliverable)
	}
	if !strings.Contains(outcome.Deliverable, "never finished") {
		t.Fatalf("the timeout partial does not say what happened:\n%s", outcome.Deliverable)
	}
}
