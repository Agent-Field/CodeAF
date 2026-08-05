package plan

import "testing"

// TestSpliceMarksParentExpanded guards a bug that cost a whole subtree.
//
// Splice held a *Node into g.Nodes while adding children, and Add appends to
// that slice. Once the backing array grew, the pointer aimed at the old one and
// every write through it — including the flag saying "this node has been
// expanded" — was silently dropped. The parent stayed marked as unexpanded work
// and the next recursion level expanded it a second time, duplicating every
// child. Nothing errored; the graph just quietly did the work twice.
//
// The child count here is deliberately larger than the parent graph, so append
// is guaranteed to reallocate.
func TestSpliceMarksParentExpanded(t *testing.T) {
	graph := &Graph{Goal: "root", Stages: []Stage{{Title: "One"}}, NextID: 1}
	upstream := graph.Add(Node{Stage: 1, Title: "Upstream", Size: SizeAtomic})
	parent := graph.Add(Node{Stage: 1, Title: "Parent", Size: SizeOversized})
	if err := graph.AddNeed(parent, upstream); err != nil {
		t.Fatalf("AddNeed: %v", err)
	}

	sub := &Graph{Goal: "parent", Stages: []Stage{{Title: "One"}}, NextID: 1}
	for _, title := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		sub.Add(Node{Stage: 1, Title: title, Size: SizeAtomic})
	}

	if err := graph.Splice(parent, sub); err != nil {
		t.Fatalf("Splice: %v", err)
	}

	node := graph.Node(parent)
	if node.Kind != KindSynthesis {
		t.Errorf("parent kind = %q, want %q — it would be expanded again", node.Kind, KindSynthesis)
	}
	if node.Size != SizeUnknown {
		t.Errorf("parent size = %q, want cleared — it would be reselected as oversized", node.Size)
	}
	if len(node.Needs) != 8 {
		t.Errorf("parent needs %v, want its 8 children", node.Needs)
	}
	for _, need := range node.Needs {
		if child := graph.Node(need); child == nil || child.Parent != parent {
			t.Errorf("need %d is not a child of the parent", need)
		}
	}
	// Children with no upstream inside the subtree take the parent's inputs, or
	// they would start before data they were promised exists.
	for _, child := range graph.Nodes {
		if child.Parent != parent {
			continue
		}
		if !contains(child.Needs, upstream) {
			t.Errorf("child %d (%s) needs %v, want it to inherit %d", child.ID, child.Title, child.Needs, upstream)
		}
	}
	if graph.hasCycle() {
		t.Error("splice produced a cycle")
	}
}

// TestSpliceRejectsFrozenParent keeps the dynamism rule honest: work that has
// started may not be restructured underneath itself.
func TestSpliceRejectsFrozenParent(t *testing.T) {
	graph := &Graph{Goal: "root", Stages: []Stage{{Title: "One"}}, NextID: 1}
	parent := graph.Add(Node{Stage: 1, Title: "Parent", State: StateRunning})
	sub := &Graph{NextID: 1}
	sub.Add(Node{Stage: 1, Title: "A"})
	sub.Add(Node{Stage: 1, Title: "B"})

	if err := graph.Splice(parent, sub); err == nil {
		t.Fatal("Splice on a running node succeeded, want refusal")
	}
	if len(graph.Nodes) != 1 {
		t.Errorf("graph gained nodes from a refused splice: %d", len(graph.Nodes))
	}
}

// TestAnchorLateStartsWiresLooseDeliverable reproduces the run that motivated
// the anchor. Bind returned almost nothing, audit recovered edges for the
// verify nodes but judged the stage-3 writer "finishable" with only the goal,
// and the node meant to produce the deliverable launched at t=0 with zero
// inputs. The anchor must wire it to the frontier — every earlier node whose
// output nothing else consumes — which also makes it the single sink, so
// deliverable ownership lands on it instead of staying ambiguous.
func TestAnchorLateStartsWiresLooseDeliverable(t *testing.T) {
	graph := &Graph{Goal: "review the pull request", NextID: 1,
		Stages: []Stage{{Title: "Inspect"}, {Title: "Verify"}, {Title: "Write"}}}
	scan := graph.Add(Node{Stage: 1, Title: "Scan"})
	var reads []int
	for _, title := range []string{"Pipeline", "Code", "Tests"} {
		id := graph.Add(Node{Stage: 1, Title: title})
		if err := graph.AddNeed(id, scan); err != nil {
			t.Fatalf("AddNeed: %v", err)
		}
		reads = append(reads, id)
	}
	var verifies []int
	for _, title := range []string{"VerifyA", "VerifyB", "VerifyC", "VerifyD", "VerifyE"} {
		id := graph.Add(Node{Stage: 2, Title: title})
		for _, need := range append([]int{scan}, reads...) {
			if err := graph.AddNeed(id, need); err != nil {
				t.Fatalf("AddNeed: %v", err)
			}
		}
		verifies = append(verifies, id)
	}
	writer := graph.Add(Node{Stage: 3, Title: "Review"})

	forced := graph.anchorLateStarts()

	if forced != len(verifies) {
		t.Errorf("forced %d edges, want %d", forced, len(verifies))
	}
	got := graph.Node(writer).Needs
	if len(got) != len(verifies) {
		t.Fatalf("writer needs %v, want the verify frontier %v", got, verifies)
	}
	for _, need := range verifies {
		if !contains(got, need) {
			t.Errorf("writer needs %v, missing frontier node %d", got, need)
		}
	}
	if len(graph.Node(scan).Needs) != 0 {
		t.Errorf("independent stage-1 node gained needs %v, want none", graph.Node(scan).Needs)
	}
	if sinks := graph.Sinks(); len(sinks) != 1 || sinks[0] != writer {
		t.Errorf("sinks = %v, want just the writer %d", sinks, writer)
	}
	if owner, _ := graph.deliverableOwner(); owner != writer {
		t.Errorf("deliverable owner = %d, want the writer %d", owner, writer)
	}
	if graph.hasCycle() {
		t.Error("anchoring produced a cycle")
	}
}

// TestAnchorLateStartsLeavesSettledNodesAlone pins the boundaries of the rule:
// stage-1 nodes may legitimately need nothing, a late node that is already
// wired is not touched, and a node that has started running is frozen.
func TestAnchorLateStartsLeavesSettledNodesAlone(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1,
		Stages: []Stage{{Title: "One"}, {Title: "Two"}}}
	first := graph.Add(Node{Stage: 1, Title: "First"})
	second := graph.Add(Node{Stage: 1, Title: "Second"})
	wired := graph.Add(Node{Stage: 2, Title: "Wired"})
	if err := graph.AddNeed(wired, first); err != nil {
		t.Fatalf("AddNeed: %v", err)
	}
	running := graph.Add(Node{Stage: 2, Title: "Running", State: StateRunning})

	if forced := graph.anchorLateStarts(); forced != 0 {
		t.Errorf("forced %d edges on a settled graph, want 0", forced)
	}
	for _, id := range []int{first, second, running} {
		if len(graph.Node(id).Needs) != 0 {
			t.Errorf("node %d gained needs %v, want none", id, graph.Node(id).Needs)
		}
	}
	if got := graph.Node(wired).Needs; len(got) != 1 || got[0] != first {
		t.Errorf("already-wired node's needs changed to %v", got)
	}
}

// TestAnchorLateStartsChainsLooseNodes covers two loose nodes in successive
// stages: the earlier one takes the stage-1 frontier, and the later one then
// chains behind it — the frontier is recomputed after every anchoring, so the
// result is a deterministic chain rather than two nodes racing from t=0.
func TestAnchorLateStartsChainsLooseNodes(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1,
		Stages: []Stage{{Title: "One"}, {Title: "Two"}, {Title: "Three"}}}
	root := graph.Add(Node{Stage: 1, Title: "Root"})
	middle := graph.Add(Node{Stage: 2, Title: "Middle"})
	last := graph.Add(Node{Stage: 3, Title: "Last"})

	if forced := graph.anchorLateStarts(); forced != 2 {
		t.Errorf("forced %d edges, want 2", forced)
	}
	if got := graph.Node(middle).Needs; len(got) != 1 || got[0] != root {
		t.Errorf("middle needs %v, want [%d]", got, root)
	}
	if got := graph.Node(last).Needs; len(got) != 1 || got[0] != middle {
		t.Errorf("last needs %v, want it to chain behind [%d]", got, middle)
	}
	if graph.hasCycle() {
		t.Error("chained anchoring produced a cycle")
	}
}

// TestAnchorLateStartsFallsBackToPreviousStage covers a frontier that is
// already fully consumed: the loose node is wired to the nearest earlier stage
// that has nodes, which is the weakest claim that still stops a t=0 launch.
func TestAnchorLateStartsFallsBackToPreviousStage(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1,
		Stages: []Stage{{Title: "One"}, {Title: "Two"}, {Title: "Three"}}}
	left := graph.Add(Node{Stage: 1, Title: "Left"})
	right := graph.Add(Node{Stage: 1, Title: "Right"})
	merge := graph.Add(Node{Stage: 2, Title: "Merge"})
	for _, need := range []int{left, right} {
		if err := graph.AddNeed(merge, need); err != nil {
			t.Fatalf("AddNeed: %v", err)
		}
	}
	tail := graph.Add(Node{Stage: 3, Title: "Tail"})
	if err := graph.AddNeed(tail, merge); err != nil {
		t.Fatalf("AddNeed: %v", err)
	}
	loose := graph.Add(Node{Stage: 3, Title: "Loose"})

	if forced := graph.anchorLateStarts(); forced != 1 {
		t.Errorf("forced %d edges, want 1", forced)
	}
	if got := graph.Node(loose).Needs; len(got) != 1 || got[0] != merge {
		t.Errorf("loose node needs %v, want the previous stage [%d]", got, merge)
	}
	if graph.hasCycle() {
		t.Error("fallback anchoring produced a cycle")
	}
}

// TestWavesIgnoreStages is the central claim of the design: a node that needs
// nothing starts immediately, whatever stage produced it.
func TestWavesIgnoreStages(t *testing.T) {
	graph := &Graph{Goal: "root", Stages: []Stage{{Title: "One"}, {Title: "Two"}}, NextID: 1}
	first := graph.Add(Node{Stage: 1, Title: "First"})
	late := graph.Add(Node{Stage: 2, Title: "Independent"})
	dependent := graph.Add(Node{Stage: 2, Title: "Dependent"})
	if err := graph.AddNeed(dependent, first); err != nil {
		t.Fatalf("AddNeed: %v", err)
	}

	waves := graph.Waves()
	if len(waves) != 2 {
		t.Fatalf("waves = %d, want 2", len(waves))
	}
	if !contains(waves[0], late) {
		t.Errorf("stage-2 node with no inputs is in wave %v, want wave 0", waves)
	}
	if !contains(waves[1], dependent) {
		t.Errorf("dependent node not in wave 1: %v", waves)
	}
}
