package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/shaped"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A REPAIR IS SAID EVEN WHEN THERE IS NOTHING TO SAY IT ABOUT YET.
//
// The planner's repairs happen against a node id nothing has been minted under —
// the job's own anchor, filed before the splice — which is precisely the moment
// the s4 sweep's textual run died. Its whole stream was "still waiting: the task
// is being turned into work" for four minutes, and then exit 1 with zero nodes.
// So a repair is the one narration this watcher reads without first asking
// whether the errand owns the node it is filed under.
func TestARepairBeforeAnyNodeExistsStillReachesTheStream(t *testing.T) {
	graph, watcher, said := narrationFixture(t)
	repair := shaped.Repair{Lane: "plan", Kind: shaped.RepairContinued}
	if err := graph.RecordStructuredRepair("job-not-spliced-yet", store.StructuredRepair{
		Lane: repair.Lane, Kind: string(repair.Kind), Line: repair.Line(),
	}); err != nil {
		t.Fatal(err)
	}
	line := narrated(t, watcher, said)
	if !strings.Contains(line, "↻ plan: answer cut at the ceiling — continued") {
		t.Fatalf("the repair never reached the stream:\n%s", line)
	}
}

// And the record survives for the autopsy, which is the other half of the same
// clause: a $0.50 run that cannot be read afterwards is a run nobody learns
// from.
func TestEveryRepairIsKeptUnderTheNodeItWasMadeFor(t *testing.T) {
	graph, _, _ := narrationFixture(t)
	journal := repairJournal{graph: graph, node: "task-1"}
	journal.Repaired(shaped.Repair{Lane: "gate", Kind: shaped.RepairReasked, Round: 1, Spent: 8192})
	kept, err := graph.StructuredRepairs("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].Lane != "gate" || kept[0].Spent != 8192 {
		t.Fatalf("the repair was not kept as it happened: %+v", kept)
	}
	if kept[0].Line != "gate: the answer was not readable — asked again" {
		t.Fatalf("the record and the stream disagree about what happened: %q", kept[0].Line)
	}
}

// A GATE THAT COULD NOT ANSWER IS NOT A GATE THAT PASSED.
//
// ink s4 delivered work as done, exit 0, under the line "the gate answered with
// nothing this could read; delivering unjudged". The judgement now carries a
// fault, the delivery path records the gap it leaves as one nothing closed, and
// this is the reading the exit code takes off that record.
func TestADeliveryWhoseGateFaultedIsNotWhole(t *testing.T) {
	graph, watcher, _ := narrationFixture(t)
	if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
		Gap: "the review could not be read, so this delivery was never checked", Unclosed: true,
	}); err != nil {
		t.Fatal(err)
	}
	node, found, err := graph.Node("task-1")
	if err != nil || !found {
		t.Fatalf("node task-1: found %t, err %v", found, err)
	}
	if watcher.deliveredWhole(node) {
		t.Fatal("a delivery whose own check never happened was reported as whole")
	}
}

// The smallest plan this system admits is one node carrying the compiled goal,
// and it has exactly one spelling — the planner's fail-safe falls back to the
// same shape every non-project ask already gets, rather than inventing a second
// answer to the same question.
func TestTheSmallestPlanIsOneNodeCarryingTheGoal(t *testing.T) {
	subtree := singleLeafPlan("job-7", "fix the follow state", plan.Spec{})
	if len(subtree.Nodes) != 1 {
		t.Fatalf("the smallest plan has %d nodes", len(subtree.Nodes))
	}
	if subtree.Nodes[0].ID != "job-7" || subtree.Nodes[0].Brief != "fix the follow state" {
		t.Fatalf("the goal did not reach the leaf: %+v", subtree.Nodes[0])
	}
	if subtree.Nodes[0].Stage != 1 {
		t.Fatalf("the one leaf is not the first stage: %+v", subtree.Nodes[0])
	}
}
