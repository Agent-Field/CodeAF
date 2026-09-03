package main

import "testing"
import "github.com/Agent-Field/aforge-v2/internal/plan"

// A GRAPH CARRYING AN UNRESOLVED SIZING REFUSAL IS NOT A SETTLED PLAN. The door
// prints and writes it either way; what this pins is the fact the exit code is
// read from, and the two shapes that are NOT it — a size with no refusal on it,
// and a refusal on a node nobody measured past one worker.
func TestARefusedOversizedNodeIsNotASettledPlan(t *testing.T) {
	graph := &plan.Graph{Goal: "three lanes over one register"}
	graph.Add(plan.Node{
		Kind: plan.KindWork, Stage: 1,
		Title:     "North, South, East",
		Size:      plan.SizeOversized,
		Undivided: plan.RefusalUnnamed,
	})
	graph.Add(plan.Node{
		Kind: plan.KindWork, Stage: 1,
		Title: "Sort the South block",
		Size:  plan.SizeOversized,
	})
	graph.Add(plan.Node{
		Kind: plan.KindWork, Stage: 1,
		Title:     "Rewrite the North dates",
		Size:      plan.SizeAtomic,
		Undivided: plan.RefusalWithinReach,
	})
	graph.Add(plan.Node{
		Kind: plan.KindWork, Stage: 1,
		Title:     "the goal, in one sitting",
		Undivided: "split gate: goal enumerates 3 items, under the 6-item floor — one sitting",
	})

	refused := unsettledSizing(graph)
	if len(refused) != 1 {
		t.Fatalf("unsettled nodes = %d, want 1: %+v", len(refused), refused)
	}
	if refused[0].Title != "North, South, East" || refused[0].Undivided != plan.RefusalUnnamed {
		t.Fatalf("unsettled node = %q — %q", refused[0].Title, refused[0].Undivided)
	}

	settled := &plan.Graph{Goal: "one errand"}
	settled.Add(plan.Node{Kind: plan.KindWork, Stage: 1, Title: "do it", Size: plan.SizeAtomic})
	if refused := unsettledSizing(settled); len(refused) != 0 {
		t.Fatalf("a settled plan reported %d unsettled nodes", len(refused))
	}
}
