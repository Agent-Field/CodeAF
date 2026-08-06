// Just-in-time re-decomposition: a leaf that ran out of budget mid-work is
// the planner's clearest evidence that one node held more than one agent's
// worth. Instead of shipping the partial or failing the job, the remainder is
// re-planned — informed by what the partial actually produced — and spliced
// in as deeper structure that consumes the partial and feeds everyone who
// was waiting. Specialize by addition: the journal keeps the exhausted
// attempt, the graph grows the finish.
package resident

import (
	"context"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// OverrunPlanFunc plans the remaining work of an exhausted leaf into a
// subtree, with prefix as the id namespace for the new nodes.
type OverrunPlanFunc func(ctx context.Context, goal, prefix string) (store.Subtree, error)

// overrunMarker tags re-expansion namespaces. Its presence in a node id is
// the recursion guard: work that is already a re-expansion does not re-expand
// again, so one oversized estimate can never cascade into unbounded splitting.
const overrunMarker = "-x1"

// OverrunGoal phrases the replan brief. The partial result is in the goal on
// purpose — "based on the current result" is the whole point: the planner
// sees what was actually produced and plans only what remains.
func OverrunGoal(node store.Node, partial string, artifacts []string) string {
	var goal strings.Builder
	goal.WriteString("Finish work a previous agent started but could not complete before running out of budget. Plan only the REMAINING work — completed parts must not be redone.\n\nThe original assignment:\n")
	goal.WriteString(node.Brief)
	if strings.TrimSpace(partial) != "" {
		goal.WriteString("\n\nWhat the previous agent produced before stopping (its partial result arrives as a dependency input; build on it):\n")
		goal.WriteString(partial)
	}
	if len(artifacts) > 0 {
		goal.WriteString("\n\nFiles already produced, to reuse rather than recreate:\n")
		goal.WriteString(strings.Join(artifacts, "\n"))
	}
	return goal.String()
}

// ReplanOverrun splices a repair subtree for a leaf whose partial result is
// about to land. The subtree's entry nodes consume the exhausted node's
// digest, its sink feeds every consumer that was waiting on the exhausted
// node and has not started, and the whole thing lives under the same job so
// workspaces, folding, and narration all treat it as the job's own work.
// Returns the spliced node count and the repair sink's id (0, "" when the
// node is itself a re-expansion and the guard declines).
func ReplanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial string, artifacts []string, planRemainder OverrunPlanFunc) (int, string, error) {
	if strings.Contains(node.ID, overrunMarker) {
		return 0, "", nil
	}
	prefix := node.ID + overrunMarker
	anchor := PlanAnchor{NodeID: jobRootID(graph, node), SessionID: node.Provenance.SessionID}
	planCtx := withPlanAnchor(ctx, anchor)
	subtree, err := planRemainder(planCtx, OverrunGoal(node, partial, artifacts), prefix)
	if err != nil {
		return 0, "", fmt.Errorf("replan overrun %s: %w", node.ID, err)
	}
	if len(subtree.Nodes) == 0 {
		return 0, "", nil
	}
	subtree = attachNeeds(subtree, []string{node.ID})

	sink := ""
	for _, spec := range subtree.Nodes {
		if spec.Parent == "" {
			sink = spec.ID
			break
		}
	}
	if sink == "" {
		return 0, "", fmt.Errorf("replan overrun %s: subtree has no sink", node.ID)
	}

	// The repair joins the exhausted node's own job when there is one; an
	// exhausted top-level job continues as a new top-level job instead, so
	// its finished result is announced like any other deliverable.
	parent := node.Parent
	if parent == "" {
		parent = store.RootID
	}
	provenance := store.Provenance{
		Origin:    store.OriginSelf,
		SessionID: node.Provenance.SessionID,
		Intent:    "re-expand " + node.ID + ": ran out of budget; the remainder continues as its own subtree",
	}
	if err := graph.Splice(parent, subtree, provenance); err != nil {
		return 0, "", fmt.Errorf("replan overrun %s: %w", node.ID, err)
	}

	// Consumers that were waiting on the exhausted node now also wait for
	// the finished remainder. Only consumers that have not started are
	// rewired — one that already ran built its transcript from the partial,
	// and rewriting history is not on offer.
	edges, err := graph.ActiveEdges()
	if err == nil {
		for _, edge := range edges {
			if edge.From != node.ID || edge.Kind == store.Suggests {
				continue
			}
			if strings.HasPrefix(edge.To, prefix) {
				continue
			}
			_ = graph.AddEdge(sink, edge.To, store.FeedsInto)
		}
	}
	return len(subtree.Nodes), sink, nil
}

func jobRootID(graph *store.Store, node store.Node) string {
	root := node
	for root.Parent != "" && root.Parent != store.RootID {
		parent, ok, err := graph.Node(root.Parent)
		if err != nil || !ok {
			return node.ID
		}
		root = parent
	}
	return root.ID
}

// attachNeeds points every entry node of a subtree at prior work, the same
// wiring continuity uses: an entry is a node with no in-subtree dependency,
// and each named source arrives as an ordinary dependency digest.
func attachNeeds(subtree store.Subtree, sources []string) store.Subtree {
	inSubtree := make(map[string]bool, len(subtree.Nodes))
	for _, spec := range subtree.Nodes {
		inSubtree[spec.ID] = true
	}
	for index, spec := range subtree.Nodes {
		entry := true
		for _, need := range spec.Needs {
			if inSubtree[need.NodeID] {
				entry = false
				break
			}
		}
		if !entry {
			continue
		}
		existing := make(map[string]bool, len(spec.Needs))
		for _, need := range spec.Needs {
			existing[need.NodeID] = true
		}
		for _, source := range sources {
			if existing[source] {
				continue
			}
			subtree.Nodes[index].Needs = append(subtree.Nodes[index].Needs,
				store.Need{NodeID: source, Kind: store.FeedsInto})
		}
	}
	return subtree
}
