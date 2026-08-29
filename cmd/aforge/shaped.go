package main

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// repairJournal is where the structured-answer seam's repairs are written down.
//
// internal/shaped knows how to get an answer out of a model that was cut off or
// answered in prose; it deliberately knows nothing about a graph, so it announces
// what it did to whatever journal the door installed on the context. This is that
// journal for both doors that have a store: the run's own, keyed to the node the
// repair belongs to.
//
// FAILSAFE.md clauses 3 and 4 are the whole of why it exists. A repair costs
// money and time and changes what somebody watching should expect; a run that
// repaired itself twice must not be indistinguishable, afterwards or at the
// time, from one that never had to.
type repairJournal struct {
	graph *store.Store
	node  string
}

// Repaired writes one row. A failed write is swallowed on purpose: this is an
// account of the work and never a part of it, and a journal that could not be
// written is not a reason to stop a run that is otherwise recovering.
func (j repairJournal) Repaired(repair shaped.Repair) {
	if j.graph == nil || j.node == "" {
		return
	}
	_ = j.graph.RecordStructuredRepair(j.node, store.StructuredRepair{
		Lane:    repair.Lane,
		Model:   repair.Model,
		Kind:    string(repair.Kind),
		Round:   repair.Round,
		Spent:   repair.Spent,
		Ceiling: repair.Ceiling,
		Line:    repair.Line(),
	})
}

// withRepairJournal points every structured call made under ctx at one node's
// record.
//
// The planner's repairs are journaled against the job root, because they happen
// BEFORE any node of the job exists — which is exactly the case the s4 sweep
// lost a whole run to, and exactly the case a record anchored on a node could
// never have covered.
func withRepairJournal(ctx context.Context, graph *store.Store, node string) context.Context {
	if graph == nil || node == "" {
		return ctx
	}
	return shaped.WithJournal(ctx, repairJournal{graph: graph, node: node})
}
