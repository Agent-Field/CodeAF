package resident

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestSubtreeFromPlanMakesTheSynthesisSinkTheRoot(t *testing.T) {
	graph := &plan.Graph{Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Gather A", Brief: "gather a"},
		{ID: 2, Stage: 1, Kind: plan.KindWork, Title: "Gather B", Brief: "gather b"},
		{ID: 3, Stage: 2, Kind: plan.KindSynthesis, Title: "Write report",
			Brief: "write the report", Needs: []int{1, 2}},
	}}

	subtree, err := SubtreeFromPlan(graph, "t0ff")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	byID := make(map[string]store.NodeSpec, len(subtree.Nodes))
	for _, spec := range subtree.Nodes {
		byID[spec.ID] = spec
	}

	root, ok := byID["t0ff"]
	if !ok || root.Parent != "" {
		t.Fatalf("synthesis sink should be the parentless root: %+v", subtree.Nodes)
	}
	if len(root.Needs) != 2 {
		t.Fatalf("root should need both gathers: %+v", root.Needs)
	}
	for _, id := range []string{"t0ff-n1", "t0ff-n2"} {
		child, ok := byID[id]
		if !ok || child.Parent != "t0ff" {
			t.Fatalf("%s should be a child of the root: %+v", id, child)
		}
	}

	// The shape must be admissible as-is: children land first, the goal last.
	s, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.Splice(store.RootID, subtree, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "verbatim words",
	}); err != nil {
		t.Fatalf("planned subtree was not admissible: %v", err)
	}
	ready, err := s.Ready(10)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	for _, node := range ready {
		if node.ID == "t0ff" {
			t.Fatalf("the goal node must not be ready before its inputs: %+v", ready)
		}
	}
}

func TestSubtreeFromPlanDropsExpandedContainers(t *testing.T) {
	graph := &plan.Graph{Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Big piece", Brief: "was expanded"},
		{ID: 2, Stage: 1, Depth: 1, Parent: 1, Kind: plan.KindWork, Title: "Part one", Brief: "part one"},
		{ID: 3, Stage: 1, Depth: 1, Parent: 1, Kind: plan.KindWork, Title: "Part two", Brief: "part two"},
		{ID: 4, Stage: 2, Kind: plan.KindSynthesis, Title: "Deliver", Brief: "deliver", Needs: []int{2, 3}},
	}}

	subtree, err := SubtreeFromPlan(graph, "tabc")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(subtree.Nodes) != 3 {
		t.Fatalf("expanded container should be structure, not work: %+v", subtree.Nodes)
	}
	for _, spec := range subtree.Nodes {
		if spec.ID == "tabc-n1" {
			t.Fatalf("container n1 admitted: %+v", subtree.Nodes)
		}
		for _, need := range spec.Needs {
			if need.NodeID == "tabc-n1" {
				t.Fatalf("need points at excluded container: %+v", spec)
			}
		}
	}
}

// The brief says what the job is; the contract says how that kind of job is
// done well, and it belongs in the executor's system message beside the
// harness's own invariants — where a headless run has always put it. Folded
// onto the brief instead, it arrived as user text below everything that changes
// between leaves, and the store's own record of the request carried a paragraph
// no person asked for.
func TestSubtreeFromPlanLeavesTheContractOutOfTheBrief(t *testing.T) {
	graph := &plan.Graph{Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Task",
			Brief: "do the thing", Contract: "verify by running it"},
	}}
	subtree, err := SubtreeFromPlan(graph, "tdd")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	brief := subtree.Nodes[0].Brief
	if !strings.Contains(brief, "do the thing") {
		t.Fatalf("brief lost the instruction: %q", brief)
	}
	if strings.Contains(brief, "verify by running it") {
		t.Fatalf("brief still folds in the working method: %q", brief)
	}
}

// The strategic-news-arbitrage bug: a downstream node's needs named three scan
// CONTAINERS; expansion replaced them with leaves and admission silently
// dropped the needs — so "connect the scans" ran first against an empty
// workspace. A need on an expanded container must resolve to every admitted
// node that expanded out of it.
func TestNeedsOnExpandedContainersRewireToTheirLeaves(t *testing.T) {
	graph := &plan.Graph{Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Source Catalog", Brief: "catalog"},
		// Two scan containers, both expanded into leaves.
		{ID: 5, Stage: 2, Kind: plan.KindWork, Title: "Scan Macro", Brief: "expanded"},
		{ID: 6, Stage: 2, Kind: plan.KindWork, Title: "Scan Companies", Brief: "expanded"},
		{ID: 10, Stage: 2, Depth: 1, Parent: 5, Kind: plan.KindWork, Title: "Macro Outlooks", Brief: "macro"},
		{ID: 11, Stage: 2, Depth: 1, Parent: 5, Kind: plan.KindWork, Title: "Tariffs", Brief: "tariffs"},
		{ID: 12, Stage: 2, Depth: 1, Parent: 6, Kind: plan.KindWork, Title: "Earnings Scan", Brief: "earnings"},
		// The consumer that was promised the scan outputs.
		{ID: 8, Stage: 3, Kind: plan.KindWork, Title: "Connect Dots", Brief: "connect", Needs: []int{1, 5, 6}},
		{ID: 9, Stage: 4, Kind: plan.KindSynthesis, Title: "Rank", Brief: "rank", Needs: []int{8}},
	}}

	subtree, err := SubtreeFromPlan(graph, "tarb")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var connect store.NodeSpec
	for _, spec := range subtree.Nodes {
		if spec.ID == "tarb-n8" {
			connect = spec
		}
	}
	if connect.ID == "" {
		t.Fatalf("connect node missing: %+v", subtree.Nodes)
	}
	got := make(map[string]bool, len(connect.Needs))
	for _, need := range connect.Needs {
		got[need.NodeID] = true
	}
	for _, want := range []string{"tarb-n1", "tarb-n10", "tarb-n11", "tarb-n12"} {
		if !got[want] {
			t.Fatalf("connect must need %s, has %v", want, connect.Needs)
		}
	}
	if len(connect.Needs) != 4 {
		t.Fatalf("unexpected needs (duplicates or containers?): %v", connect.Needs)
	}

	// End to end: admitted into a real store, the connect node must not be
	// ready while any scan leaf is unfinished.
	s, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.Splice(store.RootID, subtree, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "arbitrage playbook",
	}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	ready, err := s.Ready(20)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	for _, node := range ready {
		if node.ID == "tarb-n8" || node.ID == "tarb" {
			t.Fatalf("%s ready before its scan inputs settled: %+v", node.ID, ready)
		}
	}
}
