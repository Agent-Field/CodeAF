package plan

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// chain builds a graph of n nodes in one stage, wired a → b → c … so that the
// last node transitively depends on the first. It returns the ids in order.
func chain(t *testing.T, graph *Graph, titles ...string) []int {
	t.Helper()
	var ids []int
	for index, title := range titles {
		id := graph.Add(Node{Stage: 1, Title: title})
		if index > 0 {
			if err := graph.AddNeed(id, ids[index-1]); err != nil {
				t.Fatalf("AddNeed %s: %v", title, err)
			}
		}
		ids = append(ids, id)
	}
	return ids
}

// TestAddNeedRefusesSelfReference is the shortest cycle there is.
func TestAddNeedRefusesSelfReference(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1}
	only := graph.Add(Node{Stage: 1, Title: "Only"})
	if err := graph.AddNeed(only, only); err == nil {
		t.Fatal("a node was allowed to depend on itself")
	}
	if got := graph.Node(only).Needs; len(got) != 0 {
		t.Errorf("refused edge left needs %v behind", got)
	}
	if graph.hasCycle() {
		t.Error("the refusal left a cycle in the graph")
	}
}

// TestAddNeedRefusesLongCycle walks the closing edge all the way around a
// five-node chain, which is the case a one-step check would miss.
func TestAddNeedRefusesLongCycle(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1}
	ids := chain(t, graph, "A", "B", "C", "D", "E")
	first, last := ids[0], ids[len(ids)-1]

	if err := graph.AddNeed(first, last); err == nil {
		t.Fatal("the edge closing a five-node cycle was accepted")
	}
	if got := graph.Node(first).Needs; len(got) != 0 {
		t.Errorf("refused edge left needs %v behind on the head of the chain", got)
	}
	if graph.hasCycle() {
		t.Error("the refusal left a cycle in the graph")
	}
	// The chain itself must be untouched by the refusal.
	for index := 1; index < len(ids); index++ {
		got := graph.Node(ids[index]).Needs
		if len(got) != 1 || got[0] != ids[index-1] {
			t.Errorf("node %d needs %v, want [%d]", ids[index], got, ids[index-1])
		}
	}
}

// TestAddNeedAllowsDiamond is the shape a cycle check must not mistake for one:
// two paths reconverging is reconvergence, not a loop, and a check that gave up
// on seeing a node twice would refuse the whole pattern.
func TestAddNeedAllowsDiamond(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1}
	top := graph.Add(Node{Stage: 1, Title: "Top"})
	left := graph.Add(Node{Stage: 1, Title: "Left"})
	right := graph.Add(Node{Stage: 1, Title: "Right"})
	bottom := graph.Add(Node{Stage: 1, Title: "Bottom"})
	for _, edge := range [][2]int{{left, top}, {right, top}, {bottom, left}, {bottom, right}} {
		if err := graph.AddNeed(edge[0], edge[1]); err != nil {
			t.Fatalf("AddNeed %d → %d: %v", edge[1], edge[0], err)
		}
	}
	// The shortcut across the diamond is redundant but perfectly legal.
	if err := graph.AddNeed(bottom, top); err != nil {
		t.Errorf("the shortcut edge across a diamond was refused: %v", err)
	}
	// The same edge reversed closes the loop and must not be.
	if err := graph.AddNeed(top, bottom); err == nil {
		t.Error("the edge closing the diamond into a loop was accepted")
	}
	if graph.hasCycle() {
		t.Error("the diamond was reported as a cycle")
	}
}

// TestAddNeedAcrossDisconnectedComponents pins that an edge is judged by what
// it can reach, not by what else the graph happens to contain. Two independent
// chains may be joined in either direction.
func TestAddNeedAcrossDisconnectedComponents(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1}
	left := chain(t, graph, "L1", "L2", "L3")
	right := chain(t, graph, "R1", "R2", "R3")

	if err := graph.AddNeed(right[0], left[2]); err != nil {
		t.Fatalf("joining two disconnected chains was refused: %v", err)
	}
	// Reaching back the other way is now a cycle, and only now.
	if err := graph.AddNeed(left[0], right[2]); err == nil {
		t.Error("the edge closing the joined chains into a loop was accepted")
	}
	if graph.hasCycle() {
		t.Error("joining two disconnected chains produced a cycle")
	}
	// A third component stays untouched and stays joinable.
	lone := graph.Add(Node{Stage: 1, Title: "Lone"})
	if err := graph.AddNeed(lone, right[2]); err != nil {
		t.Errorf("a fresh node could not depend on an existing chain: %v", err)
	}
	if err := graph.AddNeed(left[0], lone); err == nil {
		t.Error("an edge back through the fresh node closed a loop and was accepted")
	}
}

// TestAddNeedIsIdempotent keeps the repeat case out of the cycle check: an edge
// that already exists is not a new edge and is not a cycle.
func TestAddNeedIsIdempotent(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1}
	ids := chain(t, graph, "A", "B")
	if err := graph.AddNeed(ids[1], ids[0]); err != nil {
		t.Fatalf("re-adding an existing edge: %v", err)
	}
	if got := graph.Node(ids[1]).Needs; len(got) != 1 {
		t.Errorf("re-adding an existing edge duplicated it: %v", got)
	}
}

// TestAddNeedMatchesWholeGraphCheck is the equivalence proof behind the
// targeted probe. AddNeed used to answer by adding the edge, sweeping the whole
// graph for any cycle anywhere, and rolling back; it now asks only whether the
// two nodes were already connected the other way. On a graph that is acyclic to
// begin with — which is the only kind that exists here — the two answers must
// agree on every edge, so this proposes several thousand random edges and
// checks that they do, edge by edge, keeping the graph acyclic throughout.
func TestAddNeedMatchesWholeGraphCheck(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	for trial := 0; trial < 200; trial++ {
		const size = 12
		graph := &Graph{Goal: "goal", NextID: 1}
		var ids []int
		for index := 0; index < size; index++ {
			ids = append(ids, graph.Add(Node{Stage: 1, Title: fmt.Sprintf("N%d", index)}))
		}
		for step := 0; step < 60; step++ {
			from, to := ids[random.Intn(size)], ids[random.Intn(size)]
			node := graph.Node(from)

			// What the whole-graph sweep would have answered.
			var swept bool
			if from != to && !contains(node.Needs, to) {
				restore := append([]int(nil), node.Needs...)
				node.Needs = mergeNeeds(node.Needs, []int{to})
				swept = graph.hasCycle()
				node.Needs = restore
			}

			err := graph.AddNeed(from, to)
			if from == to {
				if err == nil {
					t.Fatalf("trial %d step %d: self-edge on %d accepted", trial, step, from)
				}
				continue
			}
			if (err != nil) != swept {
				t.Fatalf("trial %d step %d: edge %d → %d probe says err=%v, whole-graph sweep says cycle=%v",
					trial, step, to, from, err, swept)
			}
			if graph.hasCycle() {
				t.Fatalf("trial %d step %d: edge %d → %d left a cycle behind", trial, step, to, from)
			}
		}
	}
}

// TestLoadRejectsCycles is what lets AddNeed stop scanning the whole graph. A
// generated graph cannot contain a cycle and every edit is checked, so the only
// way one can arrive is through a file, and that is where it is caught.
func TestLoadRejectsCycles(t *testing.T) {
	cases := []struct {
		name  string
		blob  string
		wants string
	}{
		{
			name: "self loop",
			blob: `{"goal":"g","next_id":2,"stages":[{"title":"One"}],
				"nodes":[{"id":1,"stage":1,"title":"A","needs":[1],"state":"pending","kind":"work"}]}`,
			wants: "cycle",
		},
		{
			name: "two node cycle",
			blob: `{"goal":"g","next_id":3,"stages":[{"title":"One"}],
				"nodes":[{"id":1,"stage":1,"title":"A","needs":[2],"state":"pending","kind":"work"},
				         {"id":2,"stage":1,"title":"B","needs":[1],"state":"pending","kind":"work"}]}`,
			wants: "cycle",
		},
		{
			name: "long cycle",
			blob: `{"goal":"g","next_id":5,"stages":[{"title":"One"}],
				"nodes":[{"id":1,"stage":1,"title":"A","needs":[4],"state":"pending","kind":"work"},
				         {"id":2,"stage":1,"title":"B","needs":[1],"state":"pending","kind":"work"},
				         {"id":3,"stage":1,"title":"C","needs":[2],"state":"pending","kind":"work"},
				         {"id":4,"stage":1,"title":"D","needs":[3],"state":"pending","kind":"work"}]}`,
			wants: "cycle",
		},
		{
			name: "diamond is not a cycle",
			blob: `{"goal":"g","next_id":5,"stages":[{"title":"One"}],
				"nodes":[{"id":1,"stage":1,"title":"Top","needs":[],"state":"pending","kind":"work"},
				         {"id":2,"stage":1,"title":"Left","needs":[1],"state":"pending","kind":"work"},
				         {"id":3,"stage":1,"title":"Right","needs":[1],"state":"pending","kind":"work"},
				         {"id":4,"stage":1,"title":"Bottom","needs":[1,2,3],"state":"pending","kind":"work"}]}`,
		},
		{
			name: "disconnected components are not a cycle",
			blob: `{"goal":"g","next_id":5,"stages":[{"title":"One"}],
				"nodes":[{"id":1,"stage":1,"title":"L1","needs":[],"state":"pending","kind":"work"},
				         {"id":2,"stage":1,"title":"L2","needs":[1],"state":"pending","kind":"work"},
				         {"id":3,"stage":1,"title":"R1","needs":[],"state":"pending","kind":"work"},
				         {"id":4,"stage":1,"title":"R2","needs":[3],"state":"pending","kind":"work"}]}`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			graph, err := Load([]byte(test.blob))
			if test.wants == "" {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if graph.hasCycle() {
					t.Error("an acyclic graph was reported as cyclic")
				}
				return
			}
			if err == nil {
				t.Fatal("a cyclic graph loaded without complaint")
			}
			if !strings.Contains(err.Error(), test.wants) {
				t.Errorf("error %q does not mention %q", err, test.wants)
			}
		})
	}
}

// TestLoadRoundTripsAPlannedGraph keeps the new check from rejecting anything
// the planner actually produces.
func TestLoadRoundTripsAPlannedGraph(t *testing.T) {
	graph := &Graph{Goal: "goal", NextID: 1, Stages: []Stage{{Title: "One"}, {Title: "Two"}}}
	ids := chain(t, graph, "A", "B", "C")
	late := graph.Add(Node{Stage: 2, Title: "Late"})
	for _, need := range ids {
		if err := graph.AddNeed(late, need); err != nil {
			t.Fatalf("AddNeed: %v", err)
		}
	}
	blob, err := graph.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Nodes) != len(graph.Nodes) {
		t.Errorf("loaded %d nodes, want %d", len(loaded.Nodes), len(graph.Nodes))
	}
}

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
