// Package boundary note: the reconciler consumes the planner, never the
// reverse, so the conversion from a planned graph to the store's admission
// shape lives here where every resident surface can share it.

package resident

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// SubtreeFromPlan converts a planned graph into the store's admission shape:
// the deliverable owner becomes the subtree root and every other node its
// child, so the goal lands last and the whole subtree reads as one task.
func SubtreeFromPlan(graph *plan.Graph, prefix string) (store.Subtree, error) {
	// Container nodes that were expanded into children are structure, not
	// work; only the nodes an executor would actually run are admitted.
	expanded := make(map[int]bool)
	for _, node := range graph.Nodes {
		if node.Parent != 0 {
			expanded[node.Parent] = true
		}
	}
	admitted := make([]plan.Node, 0, len(graph.Nodes))
	included := make(map[int]bool)
	for _, node := range graph.Nodes {
		if expanded[node.ID] {
			continue
		}
		admitted = append(admitted, node)
		included[node.ID] = true
	}
	if len(admitted) == 0 {
		return store.Subtree{}, fmt.Errorf("planned graph has no executable nodes")
	}

	// The root is the sink nothing else consumes — preferring a synthesis
	// node, which is the planner's own name for the deliverable owner.
	consumed := make(map[int]bool)
	for _, node := range admitted {
		for _, need := range node.Needs {
			consumed[need] = true
		}
	}
	rootID, rootStage, rootSynthesis := 0, 0, false
	for _, node := range admitted {
		if consumed[node.ID] {
			continue
		}
		synthesis := node.Kind == plan.KindSynthesis
		better := rootID == 0 ||
			(synthesis && !rootSynthesis) ||
			(synthesis == rootSynthesis && node.Stage > rootStage)
		if better {
			rootID, rootStage, rootSynthesis = node.ID, node.Stage, synthesis
		}
	}
	if rootID == 0 {
		rootID = admitted[len(admitted)-1].ID
	}

	// The dropped containers still carry the plan's shape: each admitted
	// leaf remembers the container chain it expanded out of, and every node
	// keeps the planner's own short title. Execution ignores both; the rail
	// renders them as the nesting the flat store no longer encodes.
	byID := make(map[int]plan.Node, len(graph.Nodes))
	for _, node := range graph.Nodes {
		byID[node.ID] = node
	}
	groupOf := func(node plan.Node) string {
		var chain []string
		for parent := node.Parent; parent != 0; {
			container, ok := byID[parent]
			if !ok {
				break
			}
			if title := strings.TrimSpace(container.Title); title != "" {
				chain = append([]string{title}, chain...)
			}
			parent = container.Parent
		}
		return strings.Join(chain, " › ")
	}

	id := func(planID int) string { return fmt.Sprintf("%s-n%d", prefix, planID) }
	specs := make([]store.NodeSpec, 0, len(admitted))
	for _, node := range admitted {
		spec := store.NodeSpec{
			ID:    id(node.ID),
			Brief: nodeBrief(node),
			Title: strings.TrimSpace(node.Title),
			Group: groupOf(node),
			Stage: node.Stage,
		}
		if node.ID != rootID {
			spec.Parent = id(rootID)
		}
		for _, need := range node.Needs {
			if !included[need] {
				continue
			}
			spec.Needs = append(spec.Needs, store.Need{NodeID: id(need), Kind: store.FeedsInto})
		}
		specs = append(specs, spec)
	}
	return store.Subtree{Nodes: specs}, nil
}

func nodeBrief(node plan.Node) string {
	brief := strings.TrimSpace(node.Brief)
	if brief == "" {
		brief = strings.TrimSpace(node.Title + "\n" + node.Summary)
	}
	if contract := strings.TrimSpace(node.Contract); contract != "" {
		brief += "\n\nWorking method:\n" + contract
	}
	return brief
}
