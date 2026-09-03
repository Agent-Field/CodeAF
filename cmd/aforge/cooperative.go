package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/splitgate"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The cooperative split, wired.
//
// The mechanism is resident's; what lives here is the same thing jit.go's
// wiring owns and that package must not — which plan document a node belongs
// to, which lock guards it, and which registry the division's own document has
// to be put back into so the leaves it just minted can be looked up when they
// are claimed.
//
// It resolves through divisionTarget, the same function claim-time division
// resolves through, because it is asking the same question of the same node.
// A node that cannot be resolved there cannot be divided here either, and the
// caller's answer to that is the answer it already had: deliver the partial.

// splitAsAsked routes a leaf's own division request into the governed growth
// path, and reports how many nodes it added.
//
// Zero is the ordinary refusal and is not an error: the request was malformed,
// the node has no plan document, a governor said no, or the expansion came back
// as a division not worth keeping. Every one of those ends the same way — the
// leaf's partial is what the node delivers — which is exactly what a
// governor-refused overrun does, and the caller reads the count rather than
// learning four ways to be told no.
func splitAsAsked(ctx context.Context, graph *store.Store, plans *jobPlans, settings config.Config,
	planClient, workClient *liveClient, planContextTokens int,
	node store.Node, outcome *exec.Outcome, artifacts []string) (int, error) {
	if graph == nil || plans == nil || !outcome.SplitRequest.Valid() {
		return 0, nil
	}
	// Split gate: refuse divisions that won't pay for their overhead — WHERE
	// SOMEBODY HAS ASKED FOR THAT. Unpinned the gate has no say and a leaf's
	// division stands as it asked for it (splitgate's modes.go: the experiment
	// that moved the default is in docs/design/plan-gate-doe/REPORT.md). Under
	// `AFORGE_SPLITGATE=1` a leaf's own division earns its keep under the same
	// rule as the planner's: the work it found enumerates many independent
	// items.
	//
	// NO LEAVES ARE HANDED OVER BECAUSE THERE ARE NONE TO HAND. A running leaf
	// asking to divide has written down evidence and nothing else — the parts
	// it wants do not exist yet, so nothing has sized them — and the mode that
	// reads the plan's own sizing (splitgate.ModeJudgment) is left with the
	// count here, honestly, rather than being fed an empty graph to read a yes
	// out of.
	if decision := splitgate.Judge(outcome.SplitRequest.Evidence, nil); !decision.Keep {
		log.Printf("split gate: refused division for %s (evidence enumerates %d items, floor %d)",
			node.ID, decision.Items, divisionFloor)
		return 0, nil
	}
	// The planning client, not the job's. A division is a planning question
	// asked of the planning model — the same one the build asked it of — and
	// the job's retained client is the one its leaves run on.
	planner := func() plan.Completer {
		if planClient == nil {
			return nil
		}
		_, structuring := planClient.Snapshot()
		return structuring
	}
	target, ok := plans.divisionTarget(graph, node.ID, settings, planner, planContextTokens)
	if !ok {
		return 0, nil
	}
	workingModel, workingClient := workClient.Snapshot()
	// The division's own document is retained under the namespace its nodes were
	// minted into, exactly as a replanned remainder's is. Without it the parts
	// exist in the store and nowhere else, and the first of them to be claimed
	// resolves to no plan at all — which silently switches claim-time division
	// off for every node this path creates.
	var divided *plan.Graph
	var namespace string
	retain := func(sub *plan.Graph, usage plan.Usage, prefix string) {
		// The two calls were paid for whatever the acceptance check decides, so
		// the spend is journalled here rather than behind the check.
		journalPlanSpend(graph, plans, planClient, prefix, usage)
		divided, namespace = sub, prefix
	}
	divide := resident.DivideAsRequested(graph, node, target, planContextTokens,
		outcome.SplitRequest, outcome.Text, retain)
	spliced, sink, err := resident.SplitCooperatively(ctx, graph, node, outcome.SplitRequest,
		outcome.Text, artifacts, settings.DailyBudgetUSD,
		resident.Growth{State: resident.LeafState(outcome)}, divide)
	// Retained only once the parts are really in the store. A document filed
	// under a namespace that holds no nodes is a lookup that answers a leaf
	// which does not exist, and the sink is not known until the splice has
	// named it.
	if spliced > 0 && divided != nil {
		plans.put(namespace, divided, sink, workingModel, workingClient)
	}
	// Inhibition: each parallel worker the division minted gets a brief that
	// names its own scope and the scopes the other workers own, so the parts
	// do not redo one another's work. The scopes come from the leaf's own
	// account of the division (SplitRequest.Parts); the nodes are the ones
	// this splice just created, found by the namespace it minted them under.
	if spliced > 0 {
		scopes := make([]string, 0, len(outcome.SplitRequest.Parts))
		for _, part := range outcome.SplitRequest.Parts {
			scopes = append(scopes, firstScope(part.Brief))
		}
		all := strings.Join(scopes, "; ")
		committed := 0
		if ids, err := graph.NodeIDsWithPrefix(namespace); err == nil {
			for _, id := range ids {
				if id == sink {
					continue
				}
				nd, ok, err := graph.Node(id)
				if err != nil || !ok {
					continue
				}
				owned := firstScope(nd.Brief)
				block := "SCOPE OWNERSHIP:\nYou own: " + owned +
					"\nOther agents own: " + all +
					"\nDo NOT redo work outside your scope.\n\n"
				if graph.AmendPending(id, block+nd.Brief, "") == nil {
					committed++
				}
			}
		}
		if committed > 0 {
			log.Printf("inhibition: %d scopes committed", committed)
		}
	}
	return spliced, err
}

// firstScope reduces a brief to a one-line scope: the first sentence, or the
// first 60 characters, whichever is shorter.
func firstScope(brief string) string {
	s := strings.TrimSpace(brief)
	if i := strings.IndexAny(s, ".!\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// The enumeration evidence gate now lives in internal/splitgate, because the
// v3 session engine asks the SAME question of a task's own work
// (internal/session's task_divide.go) and two implementations of one decision
// would drift into two answers. The names below are what this binary has always
// called it; the counting and the floor are the package's.
func enumeratedItems(text string) int { return splitgate.Items(text) }

// divisionFloor is the smallest item count at which division has ever paid in
// the bench corpus: twelve image files won, four modules and three bugs lost.
const divisionFloor = splitgate.Floor

// divisionWorthIt reports whether the ask itself enumerates enough
// independent items for a division to beat one agent doing them in sequence.
func divisionWorthIt(evidence string) bool { return splitgate.WorthIt(evidence) }

// gatePlanDivision collapses a freshly built plan to a single undivided leaf
// when the goal does not enumerate enough independent items for division to
// pay. It is the plan-time half of the split gate: the leaf-time half in
// splitAsAsked only sees divisions a leaf asks for, but most divisions are
// born here, in the planner's first reading of the ask.
//
// The collapse follows collapseAtomicChain's pattern: one work node, the goal
// as its brief, the fold recorded in Undivided so later passes can see why
// the graph is one leaf when the spine drew several. It returns the number of
// leaves folded, zero when the plan stands as drawn.
func gatePlanDivision(graph *plan.Graph, goal string) int {
	if graph == nil {
		return 0
	}
	leaves := graph.Leaves()
	if len(leaves) < 2 {
		return 0
	}
	// ONE SPELLING OF THE WHOLE ANSWER, AND IT IS QUIET BY DEFAULT. The two
	// halves of the gate read the same switch and used to spell the reading two
	// different ways, one of them the negation of the other; [splitgate.Judge]
	// is now the only reader of both the pin and the question behind it — and
	// with nothing pinned it keeps every division, which is what the plan-gate
	// experiment measured as the best front (splitgate's modes.go). The leaves
	// go with the goal because `AFORGE_SPLITGATE=judgment` prefers the plan's
	// own sizing to anything the brief said, and this is the one caller that
	// has a plan to offer.
	if splitgate.Judge(goal, gateLeaves(graph, leaves)).Keep {
		return 0
	}
	folded := len(leaves)
	title := graph.Nodes[0].Title
	summary := graph.Nodes[0].Summary
	graph.Nodes = graph.Nodes[:0]
	if len(graph.Stages) > 1 {
		graph.Stages = graph.Stages[:1]
	}
	graph.Add(plan.Node{
		Kind:    plan.KindWork,
		Stage:   1,
		Title:   title,
		Summary: summary,
		Brief:   graph.Goal,
		Undivided: fmt.Sprintf("split gate: goal enumerates %d items, under the %d-item floor — one sitting",
			splitgate.Items(goal), divisionFloor),
	})
	log.Printf("split gate: collapsed %d leaves to one (goal names %d items, floor %d)",
		folded, splitgate.Items(goal), divisionFloor)
	return folded
}

// gateLeaves reduces the planned work nodes to the two facts the gate reads:
// how big the planner made each one and what each one waits on. The synthesis
// node is already out, because [plan.Graph.Leaves] leaves it out — the gate is
// asking whether the WORK divides, and the node that gathers it is neither a
// part nor a reason the parts are not.
func gateLeaves(graph *plan.Graph, leaves []int) []splitgate.Leaf {
	work := make([]splitgate.Leaf, 0, len(leaves))
	byID := make(map[int]bool, len(leaves))
	for _, id := range leaves {
		byID[id] = true
	}
	for _, node := range graph.Nodes {
		if !byID[node.ID] {
			continue
		}
		work = append(work, splitgate.Leaf{
			ID:    node.ID,
			Size:  string(node.Size),
			Needs: node.Needs,
		})
	}
	return work
}
