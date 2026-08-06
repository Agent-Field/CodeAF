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
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// OverrunPlanFunc plans the remaining work of an exhausted leaf into a
// subtree, with prefix as the id namespace for the new nodes.
type OverrunPlanFunc func(ctx context.Context, goal, prefix string) (store.Subtree, error)

// overrunMarker tags re-expansion namespaces. Splitting is bounded by dollars,
// not by rounds: the counter advances for every repair while replacing the old
// suffix, so journal ids stay unique without growing a stack of -x1 markers.
const overrunMarker = "-x"

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
// Returns the spliced node count and the repair sink's id. DailyBudgetUSD zero
// is unlimited; at the rail the durable question is posted and no splice lands.
func ReplanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial string, artifacts []string, dailyBudgetUSD float64, planRemainder OverrunPlanFunc) (int, string, error) {
	return replanOverrun(ctx, graph, node, partial, artifacts, dailyBudgetUSD, "", planRemainder)
}

func replanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial string, artifacts []string, dailyBudgetUSD float64, prefix string, planRemainder OverrunPlanFunc) (int, string, error) {
	var err error
	if prefix == "" {
		prefix, err = nextOverrunPrefix(graph, node.ID)
		if err != nil {
			return 0, "", fmt.Errorf("replan overrun %s: %w", node.ID, err)
		}
	}
	if dailyBudgetUSD > 0 {
		rail, _, err := graph.PauseDailyRail(dailyBudgetUSD, node.Provenance.SessionID)
		if err != nil {
			return 0, "", fmt.Errorf("replan overrun %s: check daily rail: %w", node.ID, err)
		}
		if rail.Reached {
			deferred := store.DeferredOverrun{NodeID: node.ID, Partial: partial, Artifacts: artifacts, Prefix: prefix}
			if err := graph.DeferOverrun(deferred); err != nil {
				return 0, "", fmt.Errorf("replan overrun %s: defer at daily rail: %w", node.ID, err)
			}
			return 0, "", nil
		}
	}
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
		Intent:    node.Provenance.Intent,
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

// ResumeDeferredOverruns admits journaled repairs after the rail is raised.
// The splice precedes the resolved event; after a crash, an existing prefix is
// enough evidence to resolve without planning or admitting a duplicate.
func ResumeDeferredOverruns(ctx context.Context, graph *store.Store, dailyBudgetUSD float64, planRemainder OverrunPlanFunc) (int, error) {
	pending, err := graph.PendingOverruns(100)
	if err != nil {
		return 0, err
	}
	resumed := 0
	for _, deferred := range pending {
		if err := ctx.Err(); err != nil {
			return resumed, err
		}
		node, ok, err := graph.Node(deferred.NodeID)
		if err != nil {
			return resumed, err
		}
		if !ok {
			if err := graph.ResolveOverrun(deferred); err != nil {
				return resumed, err
			}
			continue
		}
		exists, err := overrunPrefixExists(graph, deferred.Prefix)
		if err != nil {
			return resumed, err
		}
		if exists {
			if err := graph.ResolveOverrun(deferred); err != nil {
				return resumed, err
			}
			continue
		}
		spliced, _, err := replanOverrun(ctx, graph, node, deferred.Partial, deferred.Artifacts,
			dailyBudgetUSD, deferred.Prefix, planRemainder)
		if err != nil {
			return resumed, err
		}
		if spliced == 0 {
			return resumed, nil
		}
		if err := graph.ResolveOverrun(deferred); err != nil {
			return resumed, err
		}
		resumed += spliced
		_, _ = graph.PostMessage(store.Message{
			SessionID: node.Provenance.SessionID,
			Role:      store.RoleSystem,
			NodeID:    node.ID,
			Body:      OverrunContinuationMessage(spliced),
		})
	}
	return resumed, nil
}

// OverrunContinuationMessage is the calm user receipt shared by immediate and
// rail-deferred splitting.
func OverrunContinuationMessage(pieces int) string {
	return fmt.Sprintf("splitting the remaining work -- %d pieces queued", pieces)
}

func overrunPrefixExists(graph *store.Store, prefix string) (bool, error) {
	nodes, err := graph.Nodes()
	if err != nil {
		return false, err
	}
	for _, node := range nodes {
		if node.ID == prefix || strings.HasPrefix(node.ID, prefix+"-") {
			return true, nil
		}
	}
	return false, nil
}

func nextOverrunPrefix(graph *store.Store, nodeID string) (string, error) {
	base := nodeID
	if marked, _, ok := splitOverrunID(nodeID); ok {
		base = marked
	}
	nodes, err := graph.Nodes()
	if err != nil {
		return "", err
	}
	maxRound := 0
	for _, candidate := range nodes {
		candidateBase, round, ok := splitOverrunID(candidate.ID)
		if ok && candidateBase == base && round > maxRound {
			maxRound = round
		}
	}
	return fmt.Sprintf("%s%s%d", base, overrunMarker, maxRound+1), nil
}

func splitOverrunID(id string) (string, int, bool) {
	for offset := 0; offset < len(id); {
		index := strings.Index(id[offset:], overrunMarker)
		if index < 0 {
			return "", 0, false
		}
		index += offset
		start := index + len(overrunMarker)
		end := start
		for end < len(id) && id[end] >= '0' && id[end] <= '9' {
			end++
		}
		if end > start && (end == len(id) || id[end] == '-') {
			round, err := strconv.Atoi(id[start:end])
			if err == nil && round > 0 {
				return id[:index], round, true
			}
		}
		offset = start
	}
	return "", 0, false
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
