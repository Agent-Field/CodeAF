package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
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
		// A task-scale job: one node, minted rather than planned, and spliced
		// under the bare prefix exactly as the sink above is.
		"task-5": {graph: taskSpecGraph("rename the parser package", "work in one branch"), root: "task-5"},
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
		// The same spelling as the sink, and the opposite answer, because the
		// document says it is the work rather than the gathering of it.
		{"a task-scale job, which is its own only leaf", "task-5", true, 1},
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

// The whole of the fix, at the seam it is meant to change: a claimed task-scale
// node reaches the free predicate.
//
// It is not that these jobs should divide — the predicate refuses this one, and
// refusing is the null hypothesis. It is that until now they could not be asked.
// A task-scale coding errand is one node under the bare prefix, the claim-time
// resolver read ids and not documents, and so the one mechanism that decides
// shape against what has actually landed was structurally unreachable for the
// commonest job in the system. What is pinned here is that the question is put:
// the refusal journal on the plan node is the predicate's own handwriting.
func TestATaskScaleNodeIsAskedWhetherItDivides(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	document := taskSpecGraph("rename the parser package across the repo", "work in one branch")
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "task-5", Brief: document.Nodes[0].Brief, Title: "Rename the parser package", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1",
		Intent: "rename the parser package across the repo"}); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := graph.Claim("task-5", "worker-1"); err != nil || !claimed {
		t.Fatalf("claim: %v (claimed=%v)", err, claimed)
	}
	claimed, ok, err := graph.Node("task-5")
	if err != nil || !ok {
		t.Fatalf("read the claimed node: ok=%t err=%v", ok, err)
	}

	plans := &jobPlans{graphs: map[string]plannedJob{"task-5": {graph: document, root: "task-5"}}}
	settings := config.Config{MaxDepth: 2, NodeBudget: 40}
	expander := resident.JITExpander{
		Graph: graph,
		Resolve: func(node store.Node) (resident.JITTarget, bool) {
			return plans.divisionTarget(node.ID, settings,
				func() plan.Completer { return silentPlanner{} }, 0)
		},
	}
	spliced, divided := expander.Expand(context.Background(), claimed)
	if divided || spliced != 0 {
		t.Fatalf("the predicate divided a node it had no argument for: spliced=%d", spliced)
	}
	if document.Nodes[0].Undivided == "" {
		t.Fatal("the node was never asked whether it divides: nothing is journaled against it")
	}
	// And the node the person sees is untouched: still claimed by its worker,
	// still one step, still called what it was called.
	after, _, err := graph.Node("task-5")
	if err != nil {
		t.Fatal(err)
	}
	if after.Title != "Rename the parser package" || after.Owner != "worker-1" {
		t.Fatalf("a refusal moved the node: %+v", after)
	}
}
