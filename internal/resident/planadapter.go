// Package boundary note: the reconciler consumes the planner, never the
// reverse, so the conversion from a planned graph to the store's admission
// shape lives here where every resident surface can share it.

package resident

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// SubtreeFromPlan converts a planned graph into the store's admission shape:
// the deliverable owner becomes the subtree root and every other node its
// child, so the goal lands last and the whole subtree reads as one task.
// nodeGroup is the display provenance, except for the one structural marker
// that rides the same field: a bundle's synthesis carries BundleGroup so the
// executor can tell assembly from judgment.
func nodeGroup(node plan.Node, groupOf func(plan.Node) string) string {
	if node.Bundle {
		return BundleGroup
	}
	return groupOf(node)
}

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

	id := func(planID int) string {
		if planID == rootID {
			return prefix
		}
		return fmt.Sprintf("%s-n%d", prefix, planID)
	}

	// A need may point at a container that was expanded away. Dropping it
	// dropped the dependency itself: a "connect the scans" node ran first,
	// against an empty workspace, because its needs named the three scan
	// containers and none of their fourteen leaves. A container's meaning is
	// "done when its descendants are done", so a need on one resolves to every
	// admitted node that expanded out of it.
	childrenOf := make(map[int][]int, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.Parent != 0 {
			childrenOf[node.Parent] = append(childrenOf[node.Parent], node.ID)
		}
	}
	var resolveNeed func(planID int, seen map[int]bool) []int
	resolveNeed = func(planID int, seen map[int]bool) []int {
		if seen[planID] {
			return nil
		}
		seen[planID] = true
		if included[planID] {
			return []int{planID}
		}
		var resolved []int
		for _, child := range childrenOf[planID] {
			resolved = append(resolved, resolveNeed(child, seen)...)
		}
		return resolved
	}

	specs := make([]store.NodeSpec, 0, len(admitted))
	for _, node := range admitted {
		spec := store.NodeSpec{
			ID:    id(node.ID),
			Brief: nodeBrief(node),
			Title: strings.TrimSpace(node.Title),
			Group: nodeGroup(node, groupOf),
			Stage: node.Stage,
			// The sizing pass's other verdict. A node the planner judged atomic
			// for a specialist arrives here as one admitted leaf rather than as
			// the eight the baseline ruler would have cut it into, and the name
			// of who it is for has to arrive with it or the cut was for nothing.
			Subharness: strings.TrimSpace(node.Subharness),
			// The task object travels with the node it was authored for. Brief
			// above is still the instruction a person would recognise and still
			// what the executor reads; this is the same work stated once as an
			// object, so the criterion survives everything that happens to the
			// node afterwards — including being re-aimed at a different worker.
			Spec: EncodeSpec(node.Spec),
		}
		if node.ID != rootID {
			spec.Parent = id(rootID)
		}
		needed := make(map[int]bool)
		for _, need := range node.Needs {
			for _, target := range resolveNeed(need, map[int]bool{node.ID: true}) {
				if target == node.ID || needed[target] {
					continue
				}
				needed[target] = true
				spec.Needs = append(spec.Needs, store.Need{NodeID: id(target), Kind: store.FeedsInto})
			}
		}
		specs = append(specs, spec)
	}
	return store.Subtree{Nodes: specs}, nil
}

// EncodeSpec renders a task object for the store, which holds it as opaque
// bytes. An empty spec encodes to nothing at all rather than to "{}": a node
// with no spec must be indistinguishable from a node admitted before specs
// existed, or the fallback path is not byte-identical and the rollback is not a
// rollback.
func EncodeSpec(spec plan.Spec) json.RawMessage {
	if spec.Empty() {
		return nil
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		// A spec is an edge, not a load-bearing wall: losing it costs the
		// criterion and the retry inheritance, and costs the node nothing else,
		// because Brief is still what it runs on.
		return nil
	}
	return encoded
}

// DecodeSpec reads a task object back off a node. Anything unreadable is the
// same answer as anything absent — the empty spec — which every reader handles
// because an absent spec is what the whole system had until this wave.
func DecodeSpec(raw json.RawMessage) plan.Spec {
	if len(raw) == 0 {
		return plan.Spec{}
	}
	var spec plan.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return plan.Spec{}
	}
	return spec
}

// nodeBrief is what the job is, and only that. The working method used to be
// folded on here as a trailing paragraph, which delivered it — but into the
// user message, below everything that changes between leaves. It belongs in the
// system message beside the harness's own invariants, so the executor now reads
// it off the plan node directly (exec.Task.Contract) and the store's brief is
// left as the instruction a person would recognise.
func nodeBrief(node plan.Node) string {
	brief := strings.TrimSpace(node.Brief)
	if brief == "" {
		brief = strings.TrimSpace(node.Title + "\n" + node.Summary)
	}
	return brief
}
