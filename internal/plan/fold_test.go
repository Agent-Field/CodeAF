package plan

import "testing"

// The predicate has to separate two nodes that read alike and cost 23× apart:
// the join whose material is already in its prompt, and the leaf that has to go
// and get things. Every fixture here is structure — inbound edges, sources,
// done-conditions, kind — and none of it is a word in a title, because a rule
// that read titles would fire on "synthesise a benchmark" and miss "the final
// list".
func TestFoldsSeparatesAssemblyFromDiscoveryOnStructureAlone(t *testing.T) {
	for name, testCase := range map[string]struct {
		node *Node
		want bool
	}{
		"the harness's own synthesis, gathering three parts": {
			node: &Node{Kind: KindSynthesis, Needs: []int{1, 2, 3},
				Title: "Synthesis", Summary: "Assemble the finished answer."},
			want: true,
		},
		"a work node that gathers and was given nothing to touch": {
			node: &Node{Kind: KindWork, Needs: []int{4, 5},
				Title: "The final list", Spec: Spec{Instruction: "put them in one list"}},
			want: true,
		},
		"a work node the plan told to go and touch things": {
			node: &Node{Kind: KindWork, Needs: []int{4, 5},
				Title:   "Compare the three vendors",
				Sources: []string{"vendor A pricing page", "vendor B pricing page"}},
			want: false,
		},
		"a work node whose spec names sources": {
			node: &Node{Kind: KindWork, Needs: []int{4, 5},
				Spec: Spec{Sources: []string{"internal/store/query.go"}}},
			want: false,
		},
		"a gathering node judged by running something": {
			node: &Node{Kind: KindWork, Needs: []int{4, 5}, Spec: Spec{Done: Done{
				Conditions: []Check{{Kind: CheckRun, Check: "make check", Expect: "exit 0"}}}}},
			want: false,
		},
		"a synthesis node judged by running something": {
			node: &Node{Kind: KindSynthesis, Needs: []int{1, 2}, Spec: Spec{Done: Done{
				Conditions: []Check{{Kind: CheckRun, Check: "go test ./...", Expect: "green"}}}}},
			want: false,
		},
		"a gathering node judged by reading its own output": {
			node: &Node{Kind: KindWork, Needs: []int{4, 5}, Spec: Spec{Done: Done{
				Produces:   []string{"comparison.md"},
				Conditions: []Check{{Kind: CheckRead, Check: "comparison.md", Expect: "three rows"}}}}},
			want: true,
		},
		"a leaf nothing feeds, however much it looks like an assembly": {
			node: &Node{Kind: KindWork, Title: "Assemble the report",
				Summary: "Synthesise the findings into one document"},
			want: false,
		},
		"a leaf one thing feeds": {
			node: &Node{Kind: KindWork, Needs: []int{4}, Title: "Polish the draft"},
			want: false,
		},
		"nothing at all": {node: nil, want: false},
	} {
		if got := Folds(testCase.node); got != testCase.want {
			t.Errorf("%s: Folds = %v, want %v", name, got, testCase.want)
		}
	}
}

// The shape the harness builds for itself, end to end: a plan that fans out and
// gathers must produce a sink the predicate recognises, or the predicate is
// answering a question no real graph ever asks it.
func TestTheGraphsOwnGatheringSinkFolds(t *testing.T) {
	graph := &Graph{}
	graph.Add(Node{Stage: 1, Title: "Read the diff", Kind: KindWork,
		Sources: []string{"the branch"}})
	graph.Add(Node{Stage: 1, Title: "Read the issue", Kind: KindWork,
		Sources: []string{"the tracker"}})
	graph.addSynthesis()

	sink := graph.deliverableSink()
	if sink == 0 {
		t.Fatal("a two-leaf plan grew no gathering sink")
	}
	if !Folds(graph.Node(sink)) {
		t.Fatalf("the harness's own sink is not recognised as an assembly: %+v", graph.Node(sink))
	}
	// And its producers are not: each was given something to go and touch, which
	// is what a leaf's turns are for.
	for _, id := range graph.Leaves() {
		if Folds(graph.Node(id)) {
			t.Fatalf("node %d was read as an assembly; it has sources to reach", id)
		}
	}
}
