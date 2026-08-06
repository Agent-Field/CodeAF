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

	root, ok := byID["t0ff-n3"]
	if !ok || root.Parent != "" {
		t.Fatalf("synthesis sink should be the parentless root: %+v", subtree.Nodes)
	}
	if len(root.Needs) != 2 {
		t.Fatalf("root should need both gathers: %+v", root.Needs)
	}
	for _, id := range []string{"t0ff-n1", "t0ff-n2"} {
		child, ok := byID[id]
		if !ok || child.Parent != "t0ff-n3" {
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
		if node.ID == "t0ff-n3" {
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

func TestSubtreeFromPlanCarriesContractIntoBrief(t *testing.T) {
	graph := &plan.Graph{Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Task",
			Brief: "do the thing", Contract: "verify by running it"},
	}}
	subtree, err := SubtreeFromPlan(graph, "tdd")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	brief := subtree.Nodes[0].Brief
	if !strings.Contains(brief, "do the thing") || !strings.Contains(brief, "verify by running it") {
		t.Fatalf("brief lost content: %q", brief)
	}
}
