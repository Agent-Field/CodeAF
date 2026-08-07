package plan

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// expandScope is the context a sub-planner is given. It is deliberately thin.
//
// The node being expanded needs to know the goal it ultimately serves, where it
// sits, and what its neighbours are called so it does not wander into them. It
// does not need their summaries, their sources, or anything from another
// subtree — that is context pollution with extra steps, and it would also break
// the frozen prefix that every expansion at this level shares.
type expandScope struct {
	Goal     string
	Ancestry []string
	Siblings []string
	Inputs   []string
}

func (s expandScope) render(node *Node) string {
	var block strings.Builder
	fmt.Fprintf(&block, "This is part of a larger goal:\n%s\n\n", s.Goal)
	if len(s.Ancestry) > 0 {
		fmt.Fprintf(&block, "It sits under: %s\n\n", strings.Join(s.Ancestry, " → "))
	}
	if len(s.Siblings) > 0 {
		fmt.Fprintf(&block, "Other work happening alongside it, which it must not duplicate or stray into:\n  %s\n\n",
			strings.Join(s.Siblings, "\n  "))
	}
	if len(s.Inputs) > 0 {
		fmt.Fprintf(&block, "It will receive the results of:\n  %s\n\n", strings.Join(s.Inputs, "\n  "))
	}
	fmt.Fprintf(&block, "Break down only this piece of it:\n%s — %s", node.Title, node.Summary)
	if len(node.Sources) > 0 {
		fmt.Fprintf(&block, "\nIt must touch: %s", strings.Join(node.Sources, "; "))
	}
	return block.String()
}

// expansion is one node's attempt at being decomposed, before we decide whether
// to keep it.
type expansion struct {
	nodeID int
	sub    *Graph
	usage  Usage
	err    error
}

// ExpandLevel decomposes every node worth decomposing, all at the same time,
// and returns how many were actually spliced in.
//
// The parallelism is the point. Each expansion is a complete four-pass build,
// so a level costs four call-rounds no matter how many nodes expand — the cost
// of recursion is measured in depth, never in width. Depth is the only thing
// here that is genuinely serial, which is why the depth cap is the guard that
// matters most.
func ExpandLevel(ctx context.Context, client Completer, graph *Graph, options Options) (int, Usage, error) {
	candidates := selectForExpansion(graph, options)
	if len(candidates) == 0 {
		return 0, Usage{}, nil
	}

	results := make([]expansion, len(candidates))
	var group sync.WaitGroup
	for index, nodeID := range candidates {
		group.Add(1)
		go func(index, nodeID int) {
			defer group.Done()
			// A faulted expansion is a node that did not split. Nothing is
			// spliced, the level reports the failure, and the node runs whole —
			// which is what a failed sub-plan already means here.
			defer func() {
				if recovered := recover(); recovered != nil {
					results[index] = expansion{nodeID: nodeID, err: guard.Note(fmt.Sprintf("plan/expand node %d", nodeID), recovered)}
				}
			}()
			results[index] = expandOne(ctx, client, graph, nodeID, options)
		}(index, nodeID)
	}
	group.Wait()

	var usage Usage
	var failures []error
	spliced := 0
	for _, result := range results {
		usage.merge(result.usage)
		if result.err != nil {
			failures = append(failures, result.err)
			continue
		}
		if !worthKeeping(graph, result) {
			continue
		}
		// The budget is enforced here rather than at selection, because until a
		// node has actually been expanded nobody knows how many children it
		// produced. Estimating beforehand overshot by a third on the first real
		// run; counting at the splice cannot.
		incoming := workNodes(result.sub)
		if len(graph.Nodes)+incoming > options.NodeBudget {
			continue
		}
		if err := graph.Splice(result.nodeID, result.sub); err != nil {
			failures = append(failures, err)
			continue
		}
		spliced++
	}
	return spliced, usage, joinErrors(failures)
}

// selectForExpansion decides who gets to expand, and it is where the model's
// judgment meets the budget. Oversized nodes always qualify; borderline ones
// only when there is room left, so the budget is spent on the nodes most likely
// to be hiding serial work rather than on ties.
func selectForExpansion(graph *Graph, options Options) []int {
	remaining := options.NodeBudget - len(graph.Nodes)
	var oversized, borderline []int
	for _, node := range graph.Nodes {
		if node.Kind != KindWork || node.State.Frozen() || node.Depth >= options.MaxDepth {
			continue
		}
		// The pre-check. Sizing already named the parts this node would split
		// into, at no extra cost, and a node that could not name two of them is
		// a node whose expansion the shrinkage guard is going to throw away
		// anyway. Skipping it here saves the whole fan-out — one run burned
		// 17,000 output tokens producing splits that were all rejected.
		if len(node.Parts) < 2 {
			continue
		}
		switch node.Size {
		case SizeOversized:
			oversized = append(oversized, node.ID)
		case SizeBorderline:
			borderline = append(borderline, node.ID)
		}
	}

	// Each expansion adds roughly a handful of nodes; budgeting at four keeps
	// the estimate honest without needing to know the answer in advance.
	const nodesPerExpansion = 4
	selected := oversized
	if len(selected)*nodesPerExpansion > remaining {
		affordable := remaining / nodesPerExpansion
		if affordable < 0 {
			affordable = 0
		}
		if affordable < len(selected) {
			return selected[:affordable]
		}
	}
	room := remaining - len(selected)*nodesPerExpansion
	for _, id := range borderline {
		if room < nodesPerExpansion {
			break
		}
		selected = append(selected, id)
		room -= nodesPerExpansion
	}
	return selected
}

func expandOne(ctx context.Context, client Completer, graph *Graph, nodeID int, options Options) expansion {
	node := graph.Node(nodeID)
	if node == nil {
		return expansion{nodeID: nodeID, err: fmt.Errorf("expand: node %d does not exist", nodeID)}
	}
	scope := scopeFor(graph, node)
	goal := scope.render(node)

	// An expansion reuses the fan-out and sizing passes, but it is not doing what
	// they do at the top of a plan: the premise is one node rather than the whole
	// goal, the catalog is short, and the question is narrower. Ability on the two
	// is not the same measurement, so the class the inner calls would name for
	// themselves is overridden for the whole subtree.
	ctx = provider.WithCallClass(ctx, provider.ClassPlanExpand)

	// A sub-decomposition is deliberately flat: one fan-out, no spine, no
	// binding. Running a full staged build inside each node was the first
	// instinct and it was wrong — every subtree contributed its own internal
	// depth, and two levels of recursion turned a graph with a critical path of
	// 3 into one with a critical path of 12. Depth multiplies where width adds.
	//
	// The restriction is also the honest reading of what expansion is for. We
	// split an oversized node to find work that can happen at the same time; if
	// what is inside it is a sequence, splitting it buys nothing and the node
	// should stay whole, which is exactly what the shrinkage guard then decides.
	// As a side effect the expansion costs two calls instead of eight.
	// The subtree inherits the settled points verbatim, the evidence standard
	// included. Without this a sub-planner rebinds the goal's free variables for
	// itself, which is exactly how one expansion produced Berlin, Paris and
	// Madrid while another produced Amsterdam — each answer defensible, the pair
	// useless — and how a subtree of a written report decides on its own that the
	// comparison needs a benchmark harness first.
	sub := &Graph{
		Goal:     goal,
		Settled:  graph.Settled,
		Open:     graph.Open,
		Evidence: graph.Evidence,
		Stages:   []Stage{{Title: node.Title, Summary: node.Summary}},
		NextID:   1,
	}
	nodes, fanUsage, err := FanOut(ctx, client, sub.context(), sub.Stages)
	usage := fanUsage
	if len(nodes) == 0 {
		return expansion{nodeID: nodeID, usage: usage, err: fmt.Errorf("expand %q: %w", node.Title, err)}
	}
	for _, child := range nodes {
		sub.Add(child)
	}
	sizeUsage, sizeErr := SizeNodes(ctx, client, sub)
	usage.merge(sizeUsage)
	if sizeErr != nil {
		err = joinErrors([]error{err, sizeErr})
	}
	sub.Goal = node.Title
	return expansion{nodeID: nodeID, sub: sub, usage: usage}
}

// scopeFor assembles what the sub-planner is allowed to see.
func scopeFor(graph *Graph, node *Node) expandScope {
	scope := expandScope{Goal: graph.Goal}
	for ancestor := node.Parent; ancestor != 0; {
		parent := graph.Node(ancestor)
		if parent == nil {
			break
		}
		scope.Ancestry = append([]string{parent.Title}, scope.Ancestry...)
		ancestor = parent.Parent
	}
	for _, other := range graph.Nodes {
		if other.ID == node.ID || other.Kind == KindSynthesis || other.Parent != node.Parent {
			continue
		}
		scope.Siblings = append(scope.Siblings, other.Title)
	}
	for _, need := range node.Needs {
		if source := graph.Node(need); source != nil {
			scope.Inputs = append(scope.Inputs, source.Title)
		}
	}
	return scope
}

// worthKeeping is the guard against decomposition that only restates.
//
// A model asked to break something down will always produce something, and the
// cheapest way to comply is to rewrite the parent as a list of near-copies of
// itself. That looks like progress and costs a full round of calls while
// leaving every part exactly as serial as before. Two symptoms give it away: a
// split into one thing, and children that size out no smaller than the parent
// did. Either means the split bought nothing, so the parent stays a leaf.
func worthKeeping(graph *Graph, result expansion) bool {
	if result.sub == nil {
		return false
	}
	children := 0
	stillOversized := 0
	for _, child := range result.sub.Nodes {
		if child.Kind == KindSynthesis {
			continue
		}
		children++
		if child.Size == SizeOversized {
			stillOversized++
		}
	}
	if children < 2 {
		return false
	}
	// Most of the children have to have actually shrunk. Requiring merely that
	// one did was too weak in practice — a split where five of eight children
	// came back still oversized passed the check and bought almost nothing,
	// while costing a full round of calls and eight more nodes.
	return stillOversized*2 < children
}

// workNodes counts what a sub-graph would really add, ignoring the synthesis
// node that the parent absorbs.
func workNodes(sub *Graph) int {
	count := 0
	for _, node := range sub.Nodes {
		if node.Kind == KindWork {
			count++
		}
	}
	return count
}
