package plan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE HALF OF THE MEASUREMENT THAT IS NOT THE MEASUREMENT.
//
// reach.go weighs what a text names against what one worker holds, and
// reach_test.go pins that reading and the per-node verdict computed from it.
// What is pinned here is the other half: the three passes that read the GOAL's
// own measurement and act on the shape of the whole build — the spine's field
// of candidates, the undivided shortcut, and the fold of an atomic chain.
//
// Every one of them reads the goal and none of them reads a node. A node's
// verdict has one computation and one home, correctBeyondReach and
// Node.BeyondReach, because that verdict has a clause about the node's siblings
// in it and none of these three passes can see a sibling. See reach.go.

// The spine is told the measurement rather than asked to guess at it, and a
// sample answering "one stage" — one worker holding all of it — is set aside
// before the vote where the measurement has already ruled that shape out. Two
// such samples used to outvote the one that laid the work out in stages.
func TestAOneStageSampleLosesTheVoteWhenTheMaterialExceedsOneWorker(t *testing.T) {
	staged := `{"stages":[` +
		`{"title":"Block A","summary":"the first lane over the file","needs":[]},` +
		`{"title":"Block B","summary":"the second lane over the file","needs":[1]}` +
		`]}`
	whole := `{"stages":[{"title":"Do the lanes","summary":"all three lanes over the file","needs":[]}]}`
	client := func() *stubClient {
		var calls int
		return &stubClient{reply: func(system, user string) string {
			if !strings.Contains(system, "You break a goal into its ordered stages") {
				return ""
			}
			calls++
			if calls == 1 {
				return staged
			}
			return whole
		}}
	}

	goal := "run three lanes over corpus.txt in order"
	dir := workspaceNaming(t, "corpus.txt", 269_000)
	named := ReachFor(dir, 0).Measure(goal)

	measured := client()
	choice, _, err := spineWithProgress(context.Background(), measured, goal, "", nil, named, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(choice.Stages) != 2 {
		t.Fatalf("the medoid kept %d stage(s) for material 8x one worker's reach", len(choice.Stages))
	}
	// The record of what was drawn is untouched: the measurement narrows the
	// field the medoid chooses from, never the report of what the samples said.
	if joinInts(choice.Drawn) != "1,1,2" {
		t.Fatalf("the spread stopped saying what was drawn: %v", choice.Drawn)
	}
	if !measured.asked("MEASURED — the material this goal names") {
		t.Fatal("the spine was not told the measurement it is no longer asked to judge")
	}

	// The control: the same three samples with nothing measured still take the
	// answer two of them gave.
	unmeasured := client()
	plain, _, err := spineWithProgress(context.Background(), unmeasured, goal, "", nil, Measurement{}, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Stages) != 1 {
		t.Fatalf("with nothing measured the vote changed: %d stages", len(plain.Stages))
	}
	if unmeasured.asked("MEASURED") {
		t.Fatal("an unmeasured run still sent the measurement block")
	}
}

// The undivided shortcut hands the whole goal to one fresh worker. That is
// the road a remainder took after a leaf had just exhausted its context on the
// same material, which is the same exhaustion bought twice.
func TestTheUndividedShortcutIsDeclinedWhenTheMaterialExceedsOneWorker(t *testing.T) {
	goal := "finish the three lanes over corpus.txt"
	dir := workspaceNaming(t, "corpus.txt", 269_000)
	one := `{"stages":[{"title":"Finish the lanes","summary":"finish all three lanes."}]}`

	measured := &countingPlanner{stages: one}
	graph, err := Build(context.Background(), measured, goal, Options{
		SpineSamples: 1, NodeBudget: 12, Ensemble: EnsembleNever, Undivided: true, Workspace: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !measured.reached("fanout") {
		t.Fatalf("material 8x one worker's reach still took the shortcut: %v", measured.passes)
	}
	if graph.Named == "" {
		t.Fatal("the graph did not carry the measurement its passes were sized by")
	}

	// The rollback, on the same goal and the same reply: with no workspace to
	// weigh it in, the shortcut is exactly the road it always was.
	unmeasured := &countingPlanner{stages: one}
	plain, err := Build(context.Background(), unmeasured, goal, Options{
		SpineSamples: 1, NodeBudget: 12, Ensemble: EnsembleNever, Undivided: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Nodes) != 1 || unmeasured.reached("fanout") {
		t.Fatalf("an unmeasured remainder stopped taking the shortcut: %d nodes, %v", len(plain.Nodes), unmeasured.passes)
	}
}

// The fold is a third pass that hands a node to one worker, and the folded node
// carries the whole goal as its brief. Where what the goal names has been
// weighed and is larger than one worker's reach, the chain stands as drawn.
func TestAChainWhoseMaterialExceedsOneWorkerIsNotFoldedIntoOneSitting(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"block-a.txt", "block-b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 24<<10), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	chain := func(workspace string) *Graph {
		graph := &Graph{Goal: "work block-a.txt then block-b.txt", NextID: 1, Workspace: workspace,
			Stages: []Stage{{Title: "A"}, {Title: "B"}}}
		graph.Add(Node{Kind: KindWork, Stage: 1, Title: "Lane A", Summary: "work block A",
			Size: SizeAtomic, Sources: []string{"block-a.txt"}})
		graph.Add(Node{Kind: KindWork, Stage: 2, Title: "Lane B", Summary: "work block B",
			Size: SizeAtomic, Sources: []string{"block-b.txt"}, Needs: []int{1}})
		return graph
	}
	// 48 KB of named material against a 32 KB reach: two sittings, never one.
	if folded := collapseAtomicChain(chain(dir)); folded != 0 {
		t.Fatalf("a chain naming 48 KB was folded into one sitting of %d nodes", folded)
	}
	// The same chain with nothing weighed folds exactly as it always did.
	if folded := collapseAtomicChain(chain("")); folded != 2 {
		t.Fatalf("an unmeasured atomic chain folded %d nodes, want 2", folded)
	}
}
