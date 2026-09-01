package revision

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
)

// The §6 regression test.
//
// Measured on a real run: a leaf whose brief named the module, the file, the
// interface type, the constructor and a `go build` acceptance check failed, and
// the node that replaced it carried 319 characters of title-and-summary — every
// one of those facts gone. Nothing truncated them. The replacement was a
// brand-new node authored from failure context, so there was nothing to lose
// them from: no object survived the re-target.
//
// What this pins is the rule the object makes enforceable. A replacement may be
// re-aimed and may not be re-authored: Done is identical, byte for byte, and
// the original instruction is still inside the one the replacement receives.
func TestReplacementInheritsTheFailedNodesCriterion(t *testing.T) {
	graph := &plan.Graph{Goal: "build the tree", NextID: 1}
	failedID := graph.Add(plan.Node{
		Stage: 1, Title: "Tree structure", Summary: "Define the node struct and tree type.",
		Brief:    "Create rbtree.go in the named module. Define the Node struct with colour, key, value and left/right pointers, and a constructor that returns an empty tree.",
		Contract: "Read what exists before writing anything.",
		Spec: plan.Spec{
			Instruction: "Create rbtree.go in the named module. Define the Node struct with colour, key, value and left/right pointers, and a constructor that returns an empty tree.",
			Method:      "Read what exists before writing anything.",
			Done: plan.Done{
				Produces: []string{"rbtree.go", "the exported constructor"},
				Conditions: []plan.Check{
					{Kind: plan.CheckRun, Check: "go build ./...", Expect: "it completes with no error"},
					{Kind: plan.CheckRead, Check: "the Node struct", Expect: "it carries colour, key, value and both child pointers"},
				},
			},
			Sources: []string{"rbtree.go"},
		},
	})
	failed := graph.Node(failedID)
	failed.State = plan.StateFailed
	failed.Failure = "the worker crashed before producing any result"

	// What the sentinel actually returns when it reacts to a failure: a title
	// and a summary, and nothing else. This is the whole of what the old path
	// had to build a replacement from.
	replacementID := graph.Add(plan.Node{
		Stage: 1, Title: "Tree structure",
		Summary: "Define the fundamental structure for the red-black tree.",
	})
	operations := []plan.Operation{{
		Op: "add", Node: replacementID, Applied: true,
		Reason: "Node 1 failed to produce any result due to a crash",
	}}

	if carried := RetargetAdds(graph, failed, operations); carried != 1 {
		t.Fatalf("carried = %d, want the one replacement", carried)
	}
	replacement := graph.Node(replacementID)

	// The criterion is inherited, not rewritten.
	if got, want := replacement.Spec.Done, failed.Spec.Done; !sameDone(got, want) {
		t.Fatalf("the replacement's criterion differs from the one it replaces:\n%+v\n%+v", got, want)
	}
	// And with it, everything the old path dropped on the floor.
	for _, want := range []string{"rbtree.go", "the named module", "constructor"} {
		if !strings.Contains(replacement.Spec.Instruction, want) {
			t.Fatalf("the replacement lost %q:\n%s", want, replacement.Spec.Instruction)
		}
	}
	if !strings.Contains(replacement.Spec.Instruction, resident.SpecUnchangedNotice) {
		t.Fatal("the replacement was not told its criterion is unchanged")
	}
	if !strings.Contains(replacement.Spec.Instruction, "crashed") {
		t.Fatal("the replacement was not told what happened to the last attempt")
	}
	if !strings.Contains(replacement.Spec.Instruction, "red-black tree") {
		t.Fatal("the sentinel's own aim was dropped from the re-target")
	}
	// Method travels too: how this kind of work is done well is a fact about
	// the work, not about the attempt.
	if replacement.Spec.Method != failed.Spec.Method || replacement.Contract != failed.Spec.Method {
		t.Fatalf("the working method did not travel: %q / %q", replacement.Spec.Method, replacement.Contract)
	}
	// Brief is still the executor's read this release, so the carried
	// instruction has to be there as well or the object would be durable and
	// unread — which is the defect wearing a different hat.
	if replacement.Brief != replacement.Spec.Instruction {
		t.Fatalf("brief and spec disagree:\n%q\n%q", replacement.Brief, replacement.Spec.Instruction)
	}
}

// A node with no spec to inherit leaves the replacement exactly as the sentinel
// wrote it. That is today's behaviour, byte for byte, and it is what a store
// written before this wave replays as.
func TestRetargetIsANoOpWithoutASpec(t *testing.T) {
	graph := &plan.Graph{Goal: "build the tree", NextID: 1}
	failedID := graph.Add(plan.Node{Stage: 1, Title: "Old", Summary: "from before specs existed"})
	failed := graph.Node(failedID)
	failed.State = plan.StateFailed

	addedID := graph.Add(plan.Node{Stage: 1, Title: "New", Summary: "the replacement"})
	before := *graph.Node(addedID)
	if carried := RetargetAdds(graph, failed, []plan.Operation{{Op: "add", Node: addedID, Applied: true}}); carried != 0 {
		t.Fatalf("carried = %d from a node with no spec", carried)
	}
	after := *graph.Node(addedID)
	if after.Brief != before.Brief || !after.Spec.Empty() {
		t.Fatalf("a spec-less failure still rewrote its replacement: %+v", after)
	}
}

// Only an applied add is a replacement. A refused operation names a node the
// graph never accepted, and writing a spec onto it would put a criterion on
// work that does not exist.
func TestRetargetSkipsRefusedOperations(t *testing.T) {
	graph := &plan.Graph{Goal: "g", NextID: 1}
	failedID := graph.Add(plan.Node{Stage: 1, Title: "Failed", Spec: plan.Spec{
		Instruction: "do it", Done: plan.Done{Produces: []string{"a result"}},
	}})
	addedID := graph.Add(plan.Node{Stage: 1, Title: "Refused"})
	operations := []plan.Operation{
		{Op: "add", Node: addedID, Applied: false, Refused: "it edited locked work"},
		{Op: "retitle", Node: addedID, Applied: true},
	}
	if carried := RetargetAdds(graph, graph.Node(failedID), operations); carried != 0 {
		t.Fatalf("carried = %d, want none", carried)
	}
	if !graph.Node(addedID).Spec.Empty() {
		t.Fatal("a refused add was given a criterion")
	}
}

// The mapping between a store id and the plan node it was minted from is the id
// scheme and nothing else, and a sink — which takes the bare prefix — has no
// plan id to be found by, so it answers nothing rather than guessing.
func TestPlanNodeForResolvesByTheIDScheme(t *testing.T) {
	graph := &plan.Graph{Goal: "g", NextID: 1}
	first := graph.Add(plan.Node{Stage: 1, Title: "One"})
	second := graph.Add(plan.Node{Stage: 1, Title: "Two"})
	if node := PlanNodeFor(graph, "job", fmt.Sprintf("job-n%d", second)); node == nil || node.ID != second {
		t.Fatalf("resolved %v, want node %d", node, second)
	}
	if node := PlanNodeFor(graph, "job", fmt.Sprintf("job-n%d", first)); node == nil || node.ID != first {
		t.Fatalf("resolved %v, want node %d", node, first)
	}
	if node := PlanNodeFor(graph, "job", "job"); node != nil {
		t.Fatalf("the bare prefix resolved to node %d", node.ID)
	}
	if node := PlanNodeFor(graph, "other", "job-n1"); node != nil {
		t.Fatal("an id from another job resolved")
	}
}

func sameDone(got, want plan.Done) bool {
	if len(got.Produces) != len(want.Produces) || len(got.Conditions) != len(want.Conditions) {
		return false
	}
	for index := range got.Produces {
		if got.Produces[index] != want.Produces[index] {
			return false
		}
	}
	for index := range got.Conditions {
		if got.Conditions[index] != want.Conditions[index] {
			return false
		}
	}
	return true
}
