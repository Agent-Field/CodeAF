package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func twoParts() *executor.SplitRequest {
	return &executor.SplitRequest{
		Evidence: "the directory holds two suites with no shared fixtures",
		Parts: []executor.SplitPart{
			{Title: "The Python cases", Summary: "the eight python fixtures", Brief: "Work through tests/py."},
			{Title: "The Go cases", Summary: "the four go fixtures", Brief: "Work through tests/go."},
		},
	}
}

// A job with one running leaf and one waiter on it — the same shape the overrun
// test uses, because the splice is the same splice.
func splitFixture(t *testing.T, name string) (*store.Store, store.Claim, store.Node) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "run every test case in tests/"},
		{ID: "job-b", Parent: "job", Brief: "consumes a", Needs: []store.Need{{NodeID: "job-a", Kind: store.FeedsInto}}},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test"}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("job-a", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("job-a")
	return graph, claim, node
}

// The scenario the whole path exists for: a leaf opens the material, finds two
// independent jobs in it, and hands them over — and what happens next is the
// governed splice, not the leaf's own division admitted on its say-so.
func TestACooperativeSplitRoutesThroughTheGovernedSplice(t *testing.T) {
	graph, claim, node := splitFixture(t, "cooperative.db")

	var planned string
	divide := func(_ context.Context, goal, prefix string) (store.Subtree, error) {
		planned = goal
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: prefix + "-n9", Brief: "gather both", Title: "Both suites"},
			{ID: prefix + "-n1", Parent: prefix + "-n9", Brief: "Work through tests/py.", Title: "The Python cases"},
			{ID: prefix + "-n2", Parent: prefix + "-n9", Brief: "Work through tests/go.", Title: "The Go cases"},
		}}, nil
	}
	spliced, sink, err := SplitCooperatively(context.Background(), graph, node, twoParts(),
		"half of it, written down", nil, 20, Growth{}, divide)
	if err != nil {
		t.Fatal(err)
	}
	if spliced != 3 || sink != "job-a-x1-n9" {
		t.Fatalf("spliced=%d sink=%q", spliced, sink)
	}

	// What the planner was handed. The exhaustion phrasing is a claim and it is
	// false here: nothing ran out, so a planner told that plans a remainder,
	// which is the wrong shape.
	if strings.Contains(planned, "resources ran out") {
		t.Fatalf("the division was planned as a remainder:\n%s", planned)
	}
	for _, want := range []string{
		"Divide this assignment",
		"run every test case in tests/",
		CooperativeFindingHeader,
		"two suites with no shared fixtures",
		"The Python cases",
		"half of it, written down",
	} {
		if !strings.Contains(planned, want) {
			t.Fatalf("the division goal lost %q:\n%s", want, planned)
		}
	}

	// The splice is the overrun splice: parts under the job, consuming the
	// node's own result, with the sink inheriting whoever was waiting.
	sinkNode, ok, err := graph.Node(sink)
	if err != nil || !ok || sinkNode.Parent != "job" {
		t.Fatalf("sink = %+v ok=%t err=%v", sinkNode, ok, err)
	}
	edges, err := graph.ActiveEdges()
	if err != nil {
		t.Fatal(err)
	}
	sinkFeedsB, nodeFeedsPart := false, false
	for _, edge := range edges {
		if edge.From == sink && edge.To == "job-b" {
			sinkFeedsB = true
		}
		if edge.From == "job-a" && edge.To == "job-a-x1-n1" {
			nodeFeedsPart = true
		}
	}
	if !sinkFeedsB || !nodeFeedsPart {
		t.Fatalf("wiring incomplete: sink→b=%t a→part=%t\n%v", sinkFeedsB, nodeFeedsPart, edges)
	}
	// Nothing runs until the leaf's partial lands: the parts consume it.
	if err := graph.Complete(claim, "half of it, written down"); err != nil {
		t.Fatal(err)
	}
	ready, err := graph.Ready(10)
	if err != nil {
		t.Fatal(err)
	}
	readyIDs := map[string]bool{}
	for _, each := range ready {
		readyIDs[each.ID] = true
	}
	if !readyIDs["job-a-x1-n1"] || !readyIDs["job-a-x1-n2"] || readyIDs["job-b"] {
		t.Fatalf("after the leaf lands: ready=%v, want both parts ready and b held", readyIDs)
	}

	// The journal says which of the growth paths spent the round. It is the one
	// growth reason that is evidence of the machinery working rather than of a
	// repair, and a battery asking how often a leaf divided before it burned a
	// budget instead of after is asking for exactly this column.
	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	admitted := 0
	for _, growth := range growths {
		if !growth.Allowed {
			continue
		}
		admitted++
		if growth.Reason != GrowCooperative {
			t.Fatalf("growth journalled as %q, want %q", growth.Reason, GrowCooperative)
		}
		if growth.Lineage != "job-a" {
			t.Fatalf("growth lineage = %q, want the leaf's own", growth.Lineage)
		}
	}
	if admitted != 1 {
		t.Fatalf("%d admitted growths, want exactly one", admitted)
	}

	// A splice names no worker on its provenance: there is one worker, so there
	// is nothing to name and nothing a reader could mistake for a choice.
	if got := sinkNode.Provenance.Subharness; got != "" {
		t.Fatalf("the division was pinned to %q", got)
	}
}

// A governor refusal is the caller's cue to deliver the partial, and it is
// spelled the way the overrun caller already reads it: no nodes, no sink, no
// error. The planner is never called, because a refusal that costs a planning
// round is not a refusal.
func TestACooperativeSplitRefusedByTheGovernorDeliversThePartial(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "cooperative-ceiling.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	specs := []store.NodeSpec{{ID: "big-job", Brief: "the whole job"}}
	// One node clear of the ceiling, so the free check on the way in — which
	// can only ask "is there room for anything at all" — is the one that
	// refuses. That is the point: a refusal that costs a planning round is not
	// a refusal.
	for i := 0; i < maxJobNodes-1; i++ {
		specs = append(specs, store.NodeSpec{ID: fmt.Sprintf("big-job-n%d", i), Parent: "big-job", Brief: "piece"})
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: specs}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s9", Intent: "test",
	}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("big-job-n0", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("big-job-n0")

	divisions := 0
	divide := func(_ context.Context, _, prefix string) (store.Subtree, error) {
		divisions++
		return store.Subtree{Nodes: []store.NodeSpec{{ID: prefix + "-n1", Brief: "a part"}}}, nil
	}
	spliced, sink, err := SplitCooperatively(context.Background(), graph, node, twoParts(),
		"half of it", nil, 20, Growth{}, divide)
	if err != nil || spliced != 0 || sink != "" {
		t.Fatalf("refused split: spliced=%d sink=%q err=%v", spliced, sink, err)
	}
	if divisions != 0 {
		t.Fatalf("the division was planned %d times past the job ceiling", divisions)
	}
	if _, ok, err := graph.Node("big-job-n0-x1-n1"); err != nil || ok {
		t.Fatalf("a part landed past the job ceiling: ok=%t err=%v", ok, err)
	}
	// The refusal is a record on the work and never a line in the conversation:
	// how large a job is allowed to grow is the machinery's own arithmetic, and
	// the delivery that follows says what the person got.
	assertRecordOnly(t, graph, "s9", node.ID, "grown as large")
}

// The other refusal, and it is free: a division the expansion came back unable
// to justify. plan.WorthKeeping is what decides that, and its verdict reaches
// this path as an empty subtree — which the splice reads as nothing to add and
// the settlement reads as "deliver the partial", exactly as a capped governor
// does.
func TestADivisionNotWorthKeepingDeliversThePartial(t *testing.T) {
	graph, _, node := splitFixture(t, "cooperative-refused.db")
	refuses := func(_ context.Context, _, _ string) (store.Subtree, error) {
		return store.Subtree{}, nil
	}
	spliced, sink, err := SplitCooperatively(context.Background(), graph, node, twoParts(),
		"half of it", nil, 20, Growth{}, refuses)
	if err != nil || spliced != 0 || sink != "" {
		t.Fatalf("refused division: spliced=%d sink=%q err=%v", spliced, sink, err)
	}
	growths, err := graph.JobGrowths("job")
	if err != nil {
		t.Fatal(err)
	}
	for _, growth := range growths {
		if growth.Allowed {
			t.Fatalf("a division that added nothing was journalled as a round spent: %+v", growth)
		}
	}
}

// A request the growth path cannot act on is not an error and not a refusal —
// it is a leaf that said something incomplete, and the caller's next move is
// the one it already had. Nothing is planned and nothing is journalled.
func TestAnIncompleteRequestGrowsNothing(t *testing.T) {
	graph, _, node := splitFixture(t, "cooperative-invalid.db")
	divisions := 0
	divide := func(_ context.Context, _, prefix string) (store.Subtree, error) {
		divisions++
		return store.Subtree{Nodes: []store.NodeSpec{{ID: prefix + "-n1", Brief: "a part"}}}, nil
	}
	for _, request := range []*executor.SplitRequest{
		nil,
		{Evidence: "found it"},
		{Parts: twoParts().Parts[:1]},
	} {
		spliced, sink, err := SplitCooperatively(context.Background(), graph, node, request,
			"half of it", nil, 20, Growth{}, divide)
		if err != nil || spliced != 0 || sink != "" {
			t.Fatalf("request %+v: spliced=%d sink=%q err=%v", request, spliced, sink, err)
		}
	}
	if divisions != 0 {
		t.Fatalf("an incomplete request bought %d planning rounds", divisions)
	}
}

// The overrun path must be exactly what it was. Growth.Goal is opt-in, so a
// caller that sets nothing gets OverrunGoal's own words — including the
// exhaustion sentence, which is true of that path and only of that path.
func TestTheOverrunGoalIsUnchangedByTheCooperativeSeam(t *testing.T) {
	graph, _, node := splitFixture(t, "cooperative-inert.db")
	var planned string
	divide := func(_ context.Context, goal, prefix string) (store.Subtree, error) {
		planned = goal
		return store.Subtree{Nodes: []store.NodeSpec{{ID: prefix + "-n9", Brief: "finish it"}}}, nil
	}
	if _, _, err := ReplanOverrun(context.Background(), graph, node, "partial", "", nil, 20, divide); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planned, "resources ran out") {
		t.Fatalf("the overrun goal lost its own phrasing:\n%s", planned)
	}
}
