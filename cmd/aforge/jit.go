package main

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The claim-time half of decomposition, wired.
//
// The mechanism lives in the resident package; what lives here is the one thing
// this process owns and that package must not: which plan document a claimed
// node belongs to, and which lock guards it. The registry is the same one every
// leaf resolves itself through, so a division edits the document a revision
// sentinel is also editing rather than a second copy of it read back out of the
// journal.

// jitExpander builds the runner's claim-time division.
//
// The planning client is this process's, not the job's. A job's retained client
// is the one its leaves run on — the work model — and a division is a planning
// question asked of the planning model, the same one the build asked it of.
//
// planContextTokens is that model's window, read from the catalog by the caller
// — the surface owns the catalog and hands facts down, the same doctrine the
// leaf's own context length travels by. Zero is the honest answer for a model
// the catalog cannot place, and every budget sized from it falls back.
func jitExpander(graph *store.Store, plans *jobPlans, settings config.Config, planClient *liveClient, planContextTokens int) resident.JITExpander {
	planner := func() plan.Completer {
		if planClient == nil {
			return nil
		}
		_, structuring := planClient.Snapshot()
		if structuring == nil {
			return nil
		}
		return structuring
	}
	return resident.JITExpander{
		Graph:          graph,
		DailyBudgetUSD: settings.DailyBudgetUSD,
		ContextTokens:  planContextTokens,
		Resolve: func(node store.Node) (resident.JITTarget, bool) {
			return plans.divisionTarget(node.ID, settings, planner, planContextTokens)
		},
	}
}

// divisionTarget resolves a claimed store node to the plan node it was minted
// from, and hands back everything a division needs to grow that document.
//
// Not ok is the ordinary answer and covers three cases that all mean the same
// thing: the node's id carries no plan node (a craft node, a bare splice), the
// job is not retained and could not be rehydrated, or the document holds no node
// by that id any more. All three run the node whole, which is what every node
// did before this existed.
//
// The one-leaf job used to be a fourth, and it was the only one that was an
// accident. A task-scale ask is spliced as a single node carrying the bare
// prefix, which is the same spelling a planned job's deliverable sink wears — so
// the id alone could not tell "this is the whole job's answer, never divide it"
// from "this IS the work". Refusing both meant a task-scale coding errand could
// never divide at claim, structurally, whatever the reading of it said, which is
// the commonest job there is left out of the one mechanism that decides shape
// against what is really there. The document draws the distinction the id
// cannot (resident.SoleWorkNode), and the judgment is the free predicate's, as
// everywhere else.
func (j *jobPlans) divisionTarget(nodeID string, settings config.Config, planner func() plan.Completer, planContextTokens int) (resident.JITTarget, bool) {
	prefix, planID, named := planNodeID(nodeID)
	if !named {
		// The other id a plan node can wear: its job's own namespace, unadorned.
		prefix = nodeID
	}
	// A repair runs flat, and it says so where it is planned: replanRemainder
	// builds its graph at MaxDepth 0 because depth multiplies, and because the
	// one time nesting grew under a running leaf it grew twenty-seven rounds
	// deep. Dividing its nodes at claim time would be that decision taken back
	// by a different file, so a repair namespace is not a division's business.
	if _, round := resident.OverrunLineage(prefix); round > 0 {
		return resident.JITTarget{}, false
	}
	entry, found := j.get(prefix)
	if !found || entry.graph == nil {
		return resident.JITTarget{}, false
	}
	if !named {
		// One node in the document means the bare prefix is the work itself.
		// Anything else means it is the sink, and a sink is a gathering step.
		sole, ok := resident.SoleWorkNode(entry.graph)
		if !ok {
			return resident.JITTarget{}, false
		}
		planID = sole
	}
	if planner == nil {
		return resident.JITTarget{}, false
	}
	structuring := planner()
	if structuring == nil {
		return resident.JITTarget{}, false
	}
	locks := j.locksFor(entry.graph)
	return resident.JITTarget{
		Plan:     entry.graph,
		Lock:     &locks.document,
		Prefix:   prefix,
		PlanNode: planID,
		Client:   structuring,
		Options: plan.Options{
			// The ceilings the job was planned under. The division raises the
			// depth one of them for itself — a build's depth ceiling is a
			// statement about how much shape to decide in advance, which is the
			// question this wave moved.
			MaxDepth:   settings.MaxDepth + 1,
			NodeBudget: settings.NodeBudget,
			// The window the sub-plan is written through. It is the same one
			// the build used, because it is the same model: a division is a
			// planning question asked of the planning model.
			ContextTokens: planContextTokens,
		},
		// The deeper document is journaled so a restart, and every reader that
		// rehydrates from the journal, sees the shape the job actually has. The
		// document lock is taken for the same reason put takes it: journalling
		// serialises the whole graph, and a graph being edited while it is being
		// serialised is a journal entry of a shape that never existed.
		Journal: func() {
			if j.journal == nil {
				return
			}
			locks.document.Lock()
			defer locks.document.Unlock()
			j.journal(prefix, entry)
		},
	}, true
}

// planNodeID splits a store node id back into the namespace it was minted under
// and the plan node it was minted from.
//
// A job's root carries the bare prefix and therefore has no plan id here. That
// is a fact about the SPELLING and not a verdict: which plan node the bare
// prefix stands for is a question only the plan document answers, and its two
// answers — the deliverable sink of a planned job, the single leaf of a
// task-scale one — want opposite treatment. See divisionTarget, which asks.
func planNodeID(nodeID string) (string, int, bool) {
	cut := strings.LastIndex(nodeID, "-n")
	if cut <= 0 {
		return "", 0, false
	}
	planID, err := strconv.Atoi(nodeID[cut+2:])
	if err != nil || planID <= 0 {
		return "", 0, false
	}
	return nodeID[:cut], planID, true
}
