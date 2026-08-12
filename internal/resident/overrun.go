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
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// SpecUnchangedNotice is what a spec says about its own criterion when it is
// carried onto a remainder or onto a replacement attempt.
//
// It is one sentence and it is the whole of the re-target contract in words:
// the bar did not move because the attempt did. Without it, a planner handed a
// criterion reads it as material — something to summarise, improve, or expand
// on — and a criterion that grows every round is a criterion that cannot be met.
const SpecUnchangedNotice = "The criterion this work is judged against has not changed and is given below " +
	"unaltered. Plan against it; do not restate it, extend it, or replace it."

// overrunCriterionLimit bounds the criterion inside a replan goal. The goal
// already carries the assignment, the partial and the gap; the criterion is the
// smallest of the four and must stay that way.
const overrunCriterionLimit = 1200

// OverrunPlanFunc plans the remaining work of an exhausted leaf into a
// subtree, with prefix as the id namespace for the new nodes.
type OverrunPlanFunc func(ctx context.Context, goal, prefix string) (store.Subtree, error)

// overrunMarker tags re-expansion namespaces. The counter advances for every
// repair while replacing the old suffix, so journal ids stay unique without
// growing a stack of -x1 markers.
const overrunMarker = store.SplitNamespace

// The caps that keep re-decomposition a repair rather than a lifestyle live in
// grow.go now, with every other path that can grow a running job: MaxOverrunRounds
// and maxJobNodes were this file's alone while this file was the only mechanism
// that could add work, and they stopped being that the day the revision sentinel
// started adding nodes of its own.

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
	// The criterion travels with the remainder rather than being re-derived
	// from the brief. A replan that re-derives it writes a new one, and a new
	// one written from a partial result is a criterion aimed at the work that
	// happened rather than at the work that was asked for — which is how a
	// round's invented verification became the next round's premise.
	if criterion := DecodeSpec(node.Spec).Done; !criterion.Empty() {
		goal.WriteString("\n\n")
		goal.WriteString(SpecUnchangedNotice)
		goal.WriteString("\n")
		goal.WriteString(plan.Spec{Done: criterion}.Render(overrunCriterionLimit))
	}
	if strings.TrimSpace(partial) != "" {
		goal.WriteString("\n\nWhat the previous agent produced before stopping (its partial result arrives as a dependency input; build on it):\n")
		goal.WriteString(partial)
	}
	if strings.TrimSpace(gap) != "" {
		goal.WriteString("\n\nA reviewer compared that result against the assignment and named what is missing. Plan the work that closes these gaps and nothing else:\n")
		goal.WriteString(gap)
		// What comes back from this round is what the person reads, and it is
		// the last thing they read: a repair on a top-level job continues as a
		// top-level job and is announced as its deliverable. Without this
		// sentence the round answers the reviewer instead of the person — one
		// measured cell delivered "no bug to find" as its opening line, on work
		// whose fix had already been applied and verified.
		goal.WriteString("\n\nWhat this work hands back is the finished assignment as the person will read it — the whole answer, standing on its own. It is not a reply to the review, not a note on what was missing, and not an account of what was repaired.")
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
	return ReplanOverrunOn(ctx, graph, node, partial, gap, artifacts, dailyBudgetUSD, "", planRemainder)
}

// ReplanOverrunOn is the same splice with the remaining work handed to a named
// worker.
//
// The name comes from the judgement that decided there was a remainder at all —
// the same call, one question wider — and it rides the subtree's provenance,
// which is where every other whole-subtree choice already rides. Empty is the
// default worker and is what every caller passed before this existed, so the
// splice is unchanged for a build with nothing to choose between.
//
// The choice is deliberately not inherited from the exhausted node. A worker
// that ran out of resources on a piece of work has said nothing about who
// should finish it, and the graph's answer to a question nobody asked is the
// baseline — which is what "degradation, never failure" means at a splice.
func ReplanOverrunOn(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, worker string, planRemainder OverrunPlanFunc) (int, string, error) {
	return ReplanOverrunAs(ctx, graph, node, partial, gap, artifacts, dailyBudgetUSD, worker, Growth{Reason: GrowOverrun}, planRemainder)
}

// ReplanOverrunAs is the same splice with the growth named for what asked.
//
// The delivery gate grows a job for a reason this file never had — a reviewer
// found the result wrong, not the budget short — and it used to inherit this
// path's governors by borrowing its whole function, which left the journal
// unable to say afterwards which of the two had spent the round. The reason
// travels now; everything else is identical.
func ReplanOverrunAs(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, worker string, growth Growth, planRemainder OverrunPlanFunc) (int, string, error) {
	spliced, sink, _, err := replanOverrun(ctx, graph, node, partial, gap, artifacts, dailyBudgetUSD, "", worker, growth, planRemainder)
	return spliced, sink, err
}

// replanOverrun reports capped=true when a governor refused the splice: the
// repair is abandoned for good, unlike the rail's zero-splice pause, which is
// waiting for consent. Deferred resumption needs the difference — a capped
// repair must resolve rather than wait forever.
func replanOverrun(ctx context.Context, graph *store.Store, node store.Node, partial, gap string, artifacts []string, dailyBudgetUSD float64, prefix, worker string, growth Growth, planRemainder OverrunPlanFunc) (int, string, bool, error) {
	var err error
	if prefix == "" {
		prefix, err = nextOverrunPrefix(graph, node.ID)
		if err != nil {
			return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
		}
	}
	// The round is the prefix's own arithmetic rather than the journal's count:
	// nextOverrunPrefix numbers rounds past the highest round that actually
	// spliced, so a refusal never consumes one, and a resumed repair carries the
	// round it was deferred at. The governor is told the answer rather than
	// asked to derive a second one that could disagree.
	lineage, round := OverrunLineage(prefix)
	request := GrowRequest{
		JobRoot: jobRootID(graph, node), Node: node, Lineage: lineage,
		Reason: growth.reason(), Round: round, DailyBudgetUSD: dailyBudgetUSD,
		Ungated: growth.Ungated,
	}
	verdict, err := growJob(ctx, graph, growth.Ask, request)
	if err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: check daily rail: %w", node.ID, err)
	}
	if !verdict.Allow {
		if verdict.Cause == CauseRail {
			deferred := store.DeferredOverrun{NodeID: node.ID, Partial: partial, Gap: gap,
				Artifacts: artifacts, Prefix: prefix, Subharness: worker}
			if err := graph.DeferOverrun(deferred); err != nil {
				return 0, "", false, fmt.Errorf("replan overrun %s: defer at daily rail: %w", node.ID, err)
			}
			return 0, "", false, nil
		}
		return 0, "", true, nil
	}
	anchor := PlanAnchor{NodeID: request.JobRoot, SessionID: node.Provenance.SessionID}
	planCtx := withPlanAnchor(ctx, anchor)
	subtree, err := planRemainder(planCtx, OverrunGoal(node, partial, artifacts, gap), prefix)
	if err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
	}
	if len(subtree.Nodes) == 0 {
		return 0, "", false, nil
	}
	// The ceiling is read twice on purpose. On the way in it can only ask
	// whether there is room for anything at all, because until the plan exists
	// nobody knows how many nodes it holds; here it asks the exact question. The
	// second look is a recheck — the free caps again, never the paid one, which
	// was already asked and answered on the way in.
	exact := request
	exact.Adding = len(subtree.Nodes)
	exact.Rechecking = true
	exact.DailyBudgetUSD = 0
	if recheck, err := growJob(ctx, graph, nil, exact); err == nil && !recheck.Allow {
		return 0, "", true, nil
	}
	subtree = attachNeeds(subtree, repairSources(graph, node))

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
	// its finished result is announced like any other deliverable. That second
	// arm is a delivery law and not a convenience — announceNode says nothing
	// at all for a completion whose parent is not the root, and absorbable()
	// reads the same column — so a repair parented into the settled job would
	// finish correctly and be delivered to nobody.
	//
	// What the repair must NOT lose by standing beside its job is the job
	// itself: the finished siblings whose results it exists to assemble, and
	// the directory they wrote into. The first travels as consumer edges
	// (repairSources, above); the second travels as the id namespace, which is
	// what jobIDOf reads. Losing both is what made one measured repair say
	// "the workspace is empty ... the trace log holds no profile content",
	// refuse to fabricate, and hand back two of twelve finished answers.
	parent := node.Parent
	if parent == "" {
		parent = store.RootID
	}
	provenance := store.Provenance{
		Origin:      store.OriginSelf,
		SessionID:   node.Provenance.SessionID,
		Intent:      node.Provenance.Intent,
		Attachments: append([]string(nil), node.Provenance.Attachments...),
		// The worker the remainder was judged to belong to, journaled at the
		// splice exactly as the compiler's own choice is — once, durably, on the
		// subtree, so every leaf under it is claimed by what it was promised.
		Subharness: strings.TrimSpace(worker),
	}
	if err := graph.Splice(parent, subtree, provenance); err != nil {
		return 0, "", false, fmt.Errorf("replan overrun %s: %w", node.ID, err)
	}
	admitGrowth(graph, request, verdict, len(subtree.Nodes))

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

// postGovernorNotice is the calm receipt a refused splice leaves on the work's
// own record: a governor stopping work quietly reads as work finishing, and the
// difference is what a reader opening that part needs to know.
//
// It is a record and not a conversation line (13.18) because what it describes
// is how the machinery divided the work — rounds spent, ceilings reached — and
// the sentence the person is owed is the delivery that follows it, which says
// what they got and carries the same "handing over what's done" in its own
// words.
func postGovernorNotice(graph *store.Store, node store.Node, body string) {
	_, _ = thread.Record(graph, store.Message{
		Role:   store.RoleSystem,
		NodeID: node.ID,
		Body:   body,
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
			dailyBudgetUSD, deferred.Prefix, deferred.Subharness, Growth{Reason: GrowOverrun}, planRemainder)
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
		// "splitting the remaining work -- 1 pieces queued" is the machinery
		// counting its own pieces. The person raised the rail and the work
		// carries on; what they hear next is the result, not the arithmetic.
		_, _ = thread.Record(graph, store.Message{
			Role:   store.RoleSystem,
			NodeID: node.ID,
			Body:   OverrunContinuationMessage(spliced),
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

// repairSourceLimit bounds the fan-in of one repair. A job that finished forty
// parts is a job whose repair reads a digest of forty parts, and the entry node
// has a context window like every other leaf; twenty-four is the same order as
// the widest split this system plans and well inside what one brief can hold.
const repairSourceLimit = 24

// repairSources names the finished work a repair must be able to see.
//
// It is the exhausted or rejected node itself — which is what this always
// passed — and, when that node is a job whose parts already landed, those
// parts' own results. The distinction is the whole of §13.3's second half. A
// gate fires on a job ROOT (shouldGate admits nothing else), so the node handed
// to a repair is routinely a sink whose twelve children hold the work and whose
// own summary is a joined or reconciled view of it. A repair wired only to the
// sink is wired to one node's account of twelve; a repair the gate rejected the
// account of is then wired to nothing it can trust, and the honest ones say so
// and refuse to invent the rest.
//
// Only settled children with something to say are named. A part that failed,
// was cancelled, or is still running has no result to consume, and naming it
// would either fail the splice's dependency check or hand the repair an empty
// digest that reads as "there was nothing there".
func repairSources(graph *store.Store, node store.Node) []string {
	sources := []string{node.ID}
	if graph == nil {
		return sources
	}
	nodes, err := graph.SubtreeNodes(node.ID)
	if err != nil {
		return sources
	}
	children := make([]store.Node, 0, len(nodes))
	for _, candidate := range nodes {
		if candidate.ID == node.ID || candidate.Parent != node.ID {
			continue
		}
		if candidate.Status != store.Done || strings.TrimSpace(candidate.Summary) == "" {
			continue
		}
		children = append(children, candidate)
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].CreatedSeq != children[j].CreatedSeq {
			return children[i].CreatedSeq < children[j].CreatedSeq
		}
		return children[i].CreatedOrder < children[j].CreatedOrder
	})
	if len(children) > repairSourceLimit {
		children = children[:repairSourceLimit]
	}
	for _, child := range children {
		sources = append(sources, child.ID)
	}
	return sources
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
