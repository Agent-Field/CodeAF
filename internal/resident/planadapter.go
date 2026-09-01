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
//
// A node's Group is display provenance and only that. It used to carry one
// structural marker as well — the synthesis of a declared bundle, which the
// executor read to skip the model and join the parts by hand. That marker was
// a claim only the bundle route could make: its leaves were the person's own
// requests, verbatim and self-contained, so concatenating them was assembly.
// A planned subtree makes no such promise about its leaves — they are as often
// fragments of one deliverable — so the claim went out with the route that
// could support it, and the gathering node writes its own delivery like every
// other gathering node in the system.

// planShape is the admission reading of a planned graph: which nodes an
// executor would actually run, and which of them is the subtree's root.
//
// It exists as one function because two files used to answer the root question
// differently and neither could see the other. See PlanStoreIDs.
//
// grown names plan nodes minted after the subtree was admitted — a revision's
// own adds. They are read out of the shape entirely, because the root is a fact
// about the plan the store already holds: an added node that consumes the sink
// would otherwise become the new sink and silently rename a store node that was
// minted minutes ago and cannot be renamed.
func planShape(graph *plan.Graph, grown map[int]bool) (admitted []plan.Node, included map[int]bool, rootID int) {
	included = make(map[int]bool)
	if graph == nil {
		return nil, included, 0
	}
	// Container nodes that were expanded into children are structure, not
	// work; only the nodes an executor would actually run are admitted.
	expanded := make(map[int]bool)
	for _, node := range graph.Nodes {
		if node.Parent != 0 {
			expanded[node.Parent] = true
		}
	}
	admitted = make([]plan.Node, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if expanded[node.ID] || grown[node.ID] {
			continue
		}
		admitted = append(admitted, node)
		included[node.ID] = true
	}
	if len(admitted) == 0 {
		return admitted, included, 0
	}

	// The root is the sink nothing else consumes — preferring a synthesis
	// node, which is the planner's own name for the deliverable owner.
	consumed := make(map[int]bool)
	for _, node := range admitted {
		for _, need := range node.Needs {
			consumed[need] = true
		}
	}
	rootStage, rootSynthesis := 0, false
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
	return admitted, included, rootID
}

// storeID is the naming law itself, in one expression: the subtree's root is the
// bare prefix and every other plan node is "<prefix>-n<id>".
func storeID(prefix string, rootID int) func(planID int) string {
	return func(planID int) string {
		if planID == rootID && rootID != 0 {
			return prefix
		}
		return fmt.Sprintf("%s-n%d", prefix, planID)
	}
}

// PlanStoreIDs is that law, offered to everything that has to name a store node
// it did not mint.
//
// It exists because the law had two authors. The splice named the subtree's root
// with the bare prefix; every later edit of the same subtree spelled every node
// "<prefix>-n<id>", root included. So a revision that rewired the deliverable's
// inputs addressed "<prefix>-n<root>" — an id that has never existed in any
// store — and the store answered "unknown node", which the batch recorded as a
// note nobody reads while the plan document recorded the edit as applied. The
// measured shape of that is a job that delivers nothing while the report it was
// supposed to deliver sits finished on disk.
//
// grown is the set of plan nodes minted after admission; see planShape. Nil is
// the answer for every caller that is naming the plan as it was admitted.
func PlanStoreIDs(graph *plan.Graph, prefix string, grown map[int]bool) func(planID int) string {
	_, _, rootID := planShape(graph, grown)
	return storeID(prefix, rootID)
}

// SoleWorkNode is the one-node plan document: a job that is one leaf and nothing
// else, with no synthesis over it. It answers the plan node's id.
//
// It is the reading that tells a claim-time division apart from the one thing it
// must never divide. A store node carrying the bare prefix is normally the job's
// deliverable sink — the gathering node, not work — and dividing it would be
// dividing the answer. When the document holds exactly one node, that same bare
// prefix is instead the whole of the work, and it is as divisible as any other
// leaf. Both readings are the same question asked of the document rather than of
// the id, which carries neither fact.
func SoleWorkNode(graph *plan.Graph) (int, bool) {
	if graph == nil || len(graph.Nodes) != 1 {
		return 0, false
	}
	node := graph.Nodes[0]
	if node.Kind == plan.KindSynthesis {
		return 0, false
	}
	return node.ID, true
}

func SubtreeFromPlan(graph *plan.Graph, prefix string) (store.Subtree, error) {
	admitted, included, rootID := planShape(graph, nil)
	if len(admitted) == 0 {
		return store.Subtree{}, fmt.Errorf("planned graph has no executable nodes")
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

	id := storeID(prefix, rootID)

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
			Group: groupOf(node),
			Stage: node.Stage,
			// The worker column travels with the node rather than being
			// re-derived at the splice. A graph written by an older build names
			// what it named, and a reader downstream can still tell "nobody
			// wrote this" from "this was written and said linear".
			Subharness: strings.TrimSpace(node.Subharness),
			// The task object travels with the node it was authored for. Brief
			// above is still the instruction a person would recognise and still
			// what the executor reads; this is the same work stated once as an
			// object, so the criterion survives everything that happens to the
			// node afterwards.
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
