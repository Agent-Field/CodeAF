package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// silentPlanner stands in for the process's planning client. A division never
// reaches it in this test — what is being checked is which nodes it is even
// offered.
type silentPlanner struct{}

func (silentPlanner) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	return nil, context.Canceled
}

// Which claimed nodes a division may even look at. Everything refused here runs
// whole, which is what every node did before claim-time division existed.
func TestOnlyAPlannedJobsOwnNodesAreDivisible(t *testing.T) {
	document := &plan.Graph{Goal: "note every unit", NextID: 1}
	document.Add(plan.Node{Title: "Gather", Kind: plan.KindWork})
	document.Add(plan.Node{Title: "Write", Kind: plan.KindWork})

	plans := &jobPlans{graphs: map[string]plannedJob{
		"task-3":    {graph: document, root: "task-3"},
		"task-4-x1": {graph: document, root: "task-4-x1"},
	}}
	planner := func() plan.Completer { return silentPlanner{} }
	settings := config.Config{MaxDepth: 2, NodeBudget: 40}

	for _, test := range []struct {
		name     string
		nodeID   string
		resolved bool
		planNode int
	}{
		{"a planned node", "task-3-n2", true, 2},
		{"the job's own sink", "task-3", false, 0},
		{"a node of a job nobody planned", "task-9-n2", false, 0},
		{"a repair, which is planned flat on purpose", "task-4-x1-n2", false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, ok := plans.divisionTarget(test.nodeID, settings, planner, 0)
			if ok != test.resolved {
				t.Fatalf("resolved = %t, want %t", ok, test.resolved)
			}
			if ok && target.PlanNode != test.planNode {
				t.Fatalf("plan node = %d, want %d", target.PlanNode, test.planNode)
			}
			if ok && target.Options.MaxDepth != settings.MaxDepth+1 {
				t.Fatalf("MaxDepth = %d, want the job's own %d", target.Options.MaxDepth, settings.MaxDepth+1)
			}
		})
	}
}
