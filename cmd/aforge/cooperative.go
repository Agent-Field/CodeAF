package main

import (
	"context"
	"log"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
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
