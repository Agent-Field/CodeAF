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

// overrunMarker tags re-expansion namespaces. The counter advances for every
// repair while replacing the old suffix, so journal ids stay unique without
// growing a stack of -x1 markers.
const overrunMarker = store.SplitNamespace

// The two caps that keep re-decomposition a repair rather than a lifestyle.
//
// Splitting used to be bounded by dollars alone, and one real run showed what
// that bound is worth on a cheap model: a leaf that had already finished was
// replanned 27 rounds deep — each round inventing verification of the round
// before it — and burned $4.48 of a $20 rail in 22 minutes while the job's
// actual work sat pending behind it. Dollars bound the damage, not the loop.
const (
	// MaxOverrunRounds bounds how many times one leaf's lineage may be
	// re-planned. Rounds are sequential by construction — each replans the
	// remainder of the last — so a lineage that is still overrunning after
	// three fresh budgets is not too big, it is thrashing, and the honest
	// move is to hand over what exists.
	MaxOverrunRounds = 3

	// maxJobNodes is the job-lifetime ceiling on dynamic growth, the runtime
	// twin of the planner's NodeBudget: that ceiling is enforced per planning
	// pass, so a job that keeps splicing repairs could sprawl past it without
	// any single pass noticing. 1.5x the default plan budget leaves real room
	// for legitimate repair while refusing the sprawl the round cap alone
	// might miss when many siblings each split within their allowance.
	maxJobNodes = 90
)

// OverrunGoal phrases the replan brief. The partial result is in the goal on
// purpose — "based on the current result" is the whole point: the planner
// sees what was actually produced and plans only what remains.
//
// The phrasing is deliberately neutral about how much is left. The first
// version asserted the work "could not be completed", and handed that premise
// to a planner that cannot answer "nothing" — so when the partial was in fact
// complete, the planner obliged the premise by inventing verification of it,
// and each round of invented verification became the next round's premise.
// Naming what a reviewer found missing, when a reviewer ran, keeps the replan
// aimed at the actual gap instead of at whatever sounds like more work.
func OverrunGoal(node store.Node, partial string, artifacts []string, gap string) string {
	var goal strings.Builder
	goal.WriteString("Finish work a previous agent started. It stopped when its resources ran out, so parts of the assignment may already be complete. Plan only what the assignment still needs — work that is already done must not be redone, and do not add verification, re-verification, or review of existing results unless the assignment itself asks for it.\n\nThe original assignment:\n")
	goal.WriteString(node.Brief)
	if strings.TrimSpace(partial) != "" {
		goal.WriteString("\n\nWhat the previous agent produced before stopping (its partial result arrives as a dependency input; build on it):\n")
		goal.WriteString(partial)
	}
	if strings.TrimSpace(gap) != "" {
		goal.WriteString("\n\nA reviewer compared that result against the assignment and named what is missing. Plan the work that closes these gaps and nothing else:\n")
		goal.WriteString(gap)
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
// The gap, when non-empty, is what a reviewer found missing from the partial —
// it aims the replan at the actual remainder.
// Returns the spliced node count and the repair sink's id. DailyBudgetUSD zero
// is unlimited; at the rail the durable question is posted and no splice lands.
func ReplanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, planRemainder OverrunPlanFunc) (int, string, error) {
	spliced, sink, _, err := replanOverrun(ctx, graph, node, partial, gap, artifacts, dailyBudgetUSD, "", planRemainder)
	return spliced, sink, err
}

// replanOverrun reports capped=true when a governor refused the splice: the
// repair is abandoned for good, unlike the rail's zero-splice pause, which is
// waiting for consent. Deferred resumption needs the difference — a capped
// repair must resolve rather than wait forever.
func replanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, prefix string, planRemainder OverrunPlanFunc) (int, string, bool, error) {
	var err error
	if prefix == "" {
		prefix, err = nextOverrunPrefix(graph, node.ID)
		if err != nil {
			return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
		}
	}
	if overrunRoundsSpent(prefix) {
		postGovernorNotice(graph, node, "this work has split as many times as splitting helps — handing over what's done")
		return 0, "", true, nil
	}
	if dailyBudgetUSD > 0 {
		rail, _, err := graph.PauseDailyRail(dailyBudgetUSD, node.Provenance.SessionID)
		if err != nil {
			return 0, "", false, fmt.Errorf("replan overrun %s: check daily rail: %w", node.ID, err)
		}
		if rail.Reached {
			deferred := store.DeferredOverrun{NodeID: node.ID, Partial: partial, Gap: gap, Artifacts: artifacts, Prefix: prefix}
			if err := graph.DeferOverrun(deferred); err != nil {
				return 0, "", false, fmt.Errorf("replan overrun %s: defer at daily rail: %w", node.ID, err)
			}
			return 0, "", false, nil
		}
	}
	anchor := PlanAnchor{NodeID: jobRootID(graph, node), SessionID: node.Provenance.SessionID}
	planCtx := withPlanAnchor(ctx, anchor)
	subtree, err := planRemainder(planCtx, OverrunGoal(node, partial, artifacts, gap), prefix)
	if err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
	}
	if len(subtree.Nodes) == 0 {
		return 0, "", false, nil
	}
	// The ceiling is enforced at the splice rather than before planning for the
	// same reason the planner's own budget is enforced there: until the plan
	// exists nobody knows how many nodes it holds.
	if ids, err := graph.NodeIDsWithPrefix(jobRootID(graph, node)); err == nil && len(ids)+len(subtree.Nodes) > maxJobNodes {
		postGovernorNotice(graph, node, "this job has grown as large as jobs are allowed to grow — handing over what's done")
		return 0, "", true, nil
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
		return 0, "", false, fmt.Errorf("replan overrun %s: subtree has no sink", node.ID)
	}

	// The repair joins the exhausted node's own job when there is one; an
	// exhausted top-level job continues as a new top-level job instead, so
	// its finished result is announced like any other deliverable.
	parent := node.Parent
	if parent == "" {
		parent = store.RootID
	}
	provenance := store.Provenance{
		Origin:      store.OriginSelf,
		SessionID:   node.Provenance.SessionID,
		Intent:      node.Provenance.Intent,
		Attachments: append([]string(nil), node.Provenance.Attachments...),
	}
	if err := graph.Splice(parent, subtree, provenance); err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
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
	return len(subtree.Nodes), sink, false, nil
}

// OverrunLineage names the lineage a node belongs to and how deep into it the
// node already is: the id it was split from and its round number, or its own id
// and zero when it has never been split.
//
// It is exported for the same reason SplitContinuation is: the "-x" arithmetic
// is the id law and it lives here. A caller that wants to read a whole job's
// history — every round of it, under one namespace — asks for the base rather
// than parsing the suffix itself.
func OverrunLineage(nodeID string) (string, int) {
	if base, round, ok := splitOverrunID(nodeID); ok {
		return base, round
	}
	return nodeID, 0
}

// overrunRoundsSpent reports that a lineage has used its splitting allowance.
// The round is read off the prefix rather than counted from the graph because
// the prefix already carries it: nextOverrunPrefix numbers rounds past the
// highest spliced round, so an unspliced refusal does not consume one.
func overrunRoundsSpent(prefix string) bool {
	_, round, ok := splitOverrunID(prefix)
	return ok && round > MaxOverrunRounds
}

// postGovernorNotice is the calm receipt a refused splice leaves in the thread:
// a governor stopping work quietly reads as work finishing, and the difference
// is exactly what the user needs to know.
func postGovernorNotice(graph *store.Store, node store.Node, body string) {
	_, _ = graph.PostMessage(store.Message{
		SessionID: node.Provenance.SessionID,
		Role:      store.RoleSystem,
		NodeID:    node.ID,
		Body:      body,
	})
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
		spliced, _, capped, err := replanOverrun(ctx, graph, node, deferred.Partial, deferred.Gap, deferred.Artifacts,
			dailyBudgetUSD, deferred.Prefix, planRemainder)
		if err != nil {
			return resumed, err
		}
		// A capped repair is abandoned for good: resolve it so it stops
		// occupying the queue. A rail pause keeps waiting for consent.
		if capped {
			if err := graph.ResolveOverrun(deferred); err != nil {
				return resumed, err
			}
			continue
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

// SplitContinued reports that a node's summary is not its last word. A leaf that
// ran out of budget mid-thought completes with whatever it had said so far, and
// that sentence is a paragraph cut in half — "the file conflicted, let me clean
// up and run the test properly" — stamped with the receipt that says the rest was
// re-planned elsewhere. Read as a result it is a lie of tense: it narrates as
// present something the graph finished a minute later.
func SplitContinued(summary string) bool {
	return strings.Contains(summary, overrunSplitPrefix)
}

// SplitContinuation follows a stamped node into its own split namespace and
// returns the pieces that carry on from it, oldest round first. It is a read of
// ids and statuses and nothing else: the namespace is an id-index range, the
// sink of each round is that round's prefix exactly (SubtreeFromPlan gives the
// plan root the prefix itself), and a node that is itself a piece only ever
// continues into rounds above its own.
//
// It lives here because the id law lives here. Anyone who re-derived the "-x"
// arithmetic at the reading end would own a second copy of it, and the day the
// counter changes shape the second copy quietly starts answering with the stale
// half of the job — which is the failure it exists to end.
func SplitContinuation(graph *store.Store, node store.Node) ([]store.Node, bool) {
	if graph == nil || !SplitContinued(node.Summary) {
		return nil, false
	}
	base, after := node.ID, 0
	if marked, round, ok := splitOverrunID(node.ID); ok {
		base, after = marked, round
	}
	ids, err := graph.NodeIDsWithPrefix(base + overrunMarker)
	if err != nil {
		return nil, false
	}
	rounds := make(map[int]bool, len(ids))
	highest := 0
	for _, id := range ids {
		candidateBase, round, ok := splitOverrunID(id)
		if !ok || candidateBase != base || round <= after {
			continue
		}
		rounds[round] = true
		if round > highest {
			highest = round
		}
	}
	pieces := make([]store.Node, 0, len(rounds))
	for round := after + 1; round <= highest; round++ {
		if !rounds[round] {
			continue
		}
		piece, found, err := graph.Node(fmt.Sprintf("%s%s%d", base, overrunMarker, round))
		if err != nil || !found {
			continue
		}
		pieces = append(pieces, piece)
	}
	return pieces, len(pieces) > 0
}

func overrunPrefixExists(graph *store.Store, prefix string) (bool, error) {
	return graph.NodeIDExistsWithPrefix(prefix)
}

func nextOverrunPrefix(graph *store.Store, nodeID string) (string, error) {
	base := nodeID
	if marked, _, ok := splitOverrunID(nodeID); ok {
		base = marked
	}
	// Only ids inside this node's own split namespace can carry its rounds, and
	// that namespace is an id-index range rather than a reason to read the graph.
	candidates, err := graph.NodeIDsWithPrefix(base + overrunMarker)
	if err != nil {
		return "", err
	}
	maxRound := 0
	for _, candidate := range candidates {
		candidateBase, round, ok := splitOverrunID(candidate)
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
