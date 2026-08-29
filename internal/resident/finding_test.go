package resident

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// removedChecks is the finding happy-dom v4-flash s13 raised four times: the
// verification lane's own word for the measurement, and the four check names it
// cited, identical on every one of the four gates.
func removedChecks(names ...string) store.DeliveryGate {
	if len(names) == 0 {
		names = []string{
			"IntersectionObserver disconnect() Does nothing",
			"IntersectionObserver observe() Does nothing",
			"IntersectionObserver takeRecords() Returns empty array",
			"IntersectionObserver unobserve() Does nothing",
		}
	}
	return store.DeliveryGate{
		Finding: "removed-checks", Quotes: names,
		Gap: "This work removed checks that existed before it: " + strings.Join(names, ", "),
	}
}

// repairRound is one round of a job that repairs a top-level job: the sink is
// spliced BESIDE the job, so it is a top-level node and therefore its own job
// root — which is the shape that cost the journal its memory.
func repairRound(t *testing.T, graph *store.Store, id string) store.Node {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Brief: "Finish the Grid layout in src/grid.ts."},
	}}, store.Provenance{Origin: store.OriginSelf, SessionID: "s1",
		Intent: "Fix the Grid layout in src/grid.ts so Box children lay out correctly."}); err != nil {
		t.Fatal(err)
	}
	return jobNode(t, graph, id)
}

// A FINDING HAS ITS OWN FIXED POINT.
//
// happy-dom v4-flash s13: four delivery judgements carried one identical
// finding, each bought a repair round, and the finding never moved. Two rounds
// are the floor — a repair that has not been tried once is not one that failed —
// and there is no third.
func TestAFindingThatStoodTwiceBuysNoThirdRound(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)
	finding := FindingOf(removedChecks())

	ask := func(node store.Node, round int, moved string) GrowVerdict {
		t.Helper()
		request := GrowRequest{
			JobRoot: node.ID, Node: node, Lineage: "task-2", Reason: GrowGap,
			Round: round, Measured: true, Workspace: root,
			Artifacts: scratchRun(t, root, moved), Finding: finding,
		}
		verdict, err := growJob(context.Background(), graph, nil, request)
		if err != nil {
			t.Fatal(err)
		}
		if verdict.Allow {
			admitGrowth(graph, request, verdict, 1)
		}
		return verdict
	}

	first := jobNode(t, graph, "task-2")
	if verdict := ask(first, 1, "src/grid.ts"); !verdict.Allow {
		t.Fatalf("the first round a finding buys is never refused: %+v", verdict)
	}
	if verdict := ask(repairRound(t, graph, "task-2-x1"), 2, "src/box.ts"); !verdict.Allow {
		t.Fatalf("the second round on a standing finding was refused: %+v", verdict)
	}
	third := ask(repairRound(t, graph, "task-2-x2"), 3, "src/index.ts")
	if third.Allow || third.Cause != CauseFindingStood {
		t.Fatalf("a third round was bought for a finding that stood twice: %+v", third)
	}

	// THE JOURNAL IS THE JOB'S. All three decisions are under one key — the
	// lineage root — and not one under each round's own top-level id, which is
	// how three rows that answer each other came to be three journals of one.
	rounds, err := graph.JobGrowthRounds("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 3 {
		t.Fatalf("the job's journal holds %d rows, want all three of its decisions", len(rounds))
	}
	for _, stray := range []string{"task-2-x1", "task-2-x2"} {
		if scattered, err := graph.JobGrowths(stray); err != nil || len(scattered) != 0 {
			t.Fatalf("%s kept a journal of its own: %+v %v", stray, scattered, err)
		}
	}
	// And every row says which finding it was bought for, so an autopsy can see
	// that four rounds were four attempts at one thing.
	for _, round := range rounds {
		if !round.Finding.Same(finding) {
			t.Fatalf("a round does not name the finding it was bought for: %+v", round.Finding)
		}
	}

	// And the person reads it, at the end, as the fact it is.
	standing, ok := GovernorStanding(graph, "task-2-x2")
	if !ok || !strings.Contains(standing, "stood through 2 rounds of repair") ||
		!strings.Contains(standing, "IntersectionObserver disconnect() Does nothing") {
		t.Fatalf("closing line = %q ok=%t", standing, ok)
	}
}

// The other half of the same rule: a finding is refused, and A FINDING OF
// ANOTHER KIND IS ANOTHER FINDING. It buys its own first round, which is the
// floor holding rather than an exception to it.
func TestAnotherFindingStillBuysItsOwnRound(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)
	stood := FindingOf(removedChecks())
	node := jobNode(t, graph, "task-2")

	for round := 1; round <= 2; round++ {
		request := GrowRequest{JobRoot: "task-2", Node: node, Lineage: "task-2",
			Reason: GrowGap, Round: round, Measured: true, Workspace: root,
			Artifacts: scratchRun(t, root, "src/grid.ts"), Finding: stood}
		admitGrowth(graph, request, GrowVerdict{Allow: true, Round: round}, 1)
	}

	other := FindingOf(store.DeliveryGate{
		Finding: "regression", Quotes: []string{"grid lays out box children"},
		Gap: "a check that passed before this work fails after it",
	})
	verdict, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "task-2", Node: node, Lineage: "task-2", Reason: GrowGap,
		Round: 3, Measured: true, Workspace: root,
		Artifacts: scratchRun(t, root, "src/box.ts"), Finding: other})
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Allow {
		t.Fatalf("a finding nobody has worked on yet was refused: %+v", verdict)
	}
}

// A finding is its kind and its names, taken from the record. The same names in
// another order, or another case, are the same finding; one more name is not.
func TestAFindingIsItsKindAndItsNamesAndNotItsSentence(t *testing.T) {
	first := FindingOf(removedChecks())
	if first.Kind != "removed-checks" {
		t.Fatalf("the measurement's own word was not kept: %+v", first)
	}
	shuffled := removedChecks(
		"IntersectionObserver UNOBSERVE() Does nothing",
		"IntersectionObserver takeRecords()   Returns empty array",
		"IntersectionObserver observe() Does nothing",
		"IntersectionObserver disconnect() Does nothing")
	shuffled.Gap = "an entirely different sentence about the same four checks"
	if !first.Same(FindingOf(shuffled)) {
		t.Fatal("one finding written twice read as two")
	}
	wider := removedChecks(
		"IntersectionObserver disconnect() Does nothing",
		"IntersectionObserver observe() Does nothing",
		"IntersectionObserver takeRecords() Returns empty array",
		"IntersectionObserver unobserve() Does nothing",
		"IntersectionObserver root margin is parsed")
	if first.Same(FindingOf(wider)) {
		t.Fatal("a finding that grew a name read as the finding it grew from")
	}
	// A gate that passed, and one whose finding was weighed against the world
	// and lost, raise nothing a round could be bought for.
	passed := removedChecks()
	passed.Pass = true
	if !FindingOf(passed).Empty() {
		t.Fatal("a passing gate raised a finding")
	}
	overturned := removedChecks()
	overturned.Overturned = true
	if !FindingOf(overturned).Empty() {
		t.Fatal("a finding that lost is still standing")
	}
	// And an empty finding is never the same as another empty one: "nobody
	// recorded what this round was for" is not evidence that two rounds were
	// for one thing.
	if (store.GrowthFinding{}).Same(store.GrowthFinding{}) {
		t.Fatal("two rounds nobody named were read as one finding")
	}
}
