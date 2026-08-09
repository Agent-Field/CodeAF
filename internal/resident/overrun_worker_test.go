package resident

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func overrunWorkerFixture(t *testing.T) (*store.Store, store.Node) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "the oversized part"},
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
	return graph, node
}

func twoNodeRemainder(_ context.Context, _, prefix string) (store.Subtree, error) {
	return store.Subtree{Nodes: []store.NodeSpec{
		{ID: prefix + "-n9", Brief: "finish it", Title: "Finish"},
		{ID: prefix + "-n5", Parent: prefix + "-n9", Brief: "remaining piece", Title: "Remaining piece"},
	}}, nil
}

// The judge that decided there was a remainder may also decide who takes it,
// and the answer has to be durable: the continuation is a real node, claimed
// minutes or a restart later, and a promise the splice did not journal is a
// promise nobody keeps.
func TestAJudgedContinuationCarriesItsWorkerDurably(t *testing.T) {
	graph, node := overrunWorkerFixture(t)
	spliced, sink, err := ReplanOverrunOn(context.Background(), graph, node,
		"partial", "", nil, 0, "swe", twoNodeRemainder)
	if err != nil || spliced != 2 {
		t.Fatalf("spliced=%d err=%v", spliced, err)
	}
	for _, id := range []string{sink, "job-a-x1-n5"} {
		continued, found, err := graph.Node(id)
		if err != nil || !found {
			t.Fatalf("read %s: found=%t err=%v", id, found, err)
		}
		if continued.Subharness != "swe" || continued.Provenance.Subharness != "swe" {
			t.Fatalf("%s = %q (provenance %q), want the judged worker",
				id, continued.Subharness, continued.Provenance.Subharness)
		}
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if continued, _, _ := graph.Node(sink); continued.Subharness != "swe" {
		t.Fatalf("the choice did not survive a rebuild: %q", continued.Subharness)
	}
}

// And when nothing was judged, nothing is claimed. The exhausted node's own
// worker is not inherited: running out of resources on a piece of work says
// nothing about who should finish it, and the graph's answer to a question
// nobody asked is the baseline.
func TestAnUnjudgedContinuationStaysOnTheBaselineWorker(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the whole job"},
		{ID: "job-a", Parent: "job", Brief: "the oversized part"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test",
		Subharness: "swe"}); err != nil {
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
	if node.Subharness != "swe" {
		t.Fatalf("the fixture's leaf is not on the specialist: %q", node.Subharness)
	}
	_, sink, err := ReplanOverrun(context.Background(), graph, node, "partial", "", nil, 0, twoNodeRemainder)
	if err != nil {
		t.Fatal(err)
	}
	if continued, _, _ := graph.Node(sink); continued.Subharness != "" {
		t.Fatalf("the continuation inherited a worker nobody chose for it: %q", continued.Subharness)
	}
}

// The rail is a pause, not a decision unmade: a repair held for consent resumes
// on the worker it was planned for.
func TestADeferredContinuationRemembersItsWorker(t *testing.T) {
	graph, node := overrunWorkerFixture(t)
	// A rail already reached: the repair is journaled and nothing is spliced.
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "job-a", Cost: 99}); err != nil {
		t.Fatal(err)
	}
	spliced, _, err := ReplanOverrunOn(context.Background(), graph, node,
		"partial", "", nil, 1, "swe", twoNodeRemainder)
	if err != nil || spliced != 0 {
		t.Fatalf("the rail did not hold the repair: spliced=%d err=%v", spliced, err)
	}
	pending, err := graph.PendingOverruns(10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%d err=%v", len(pending), err)
	}
	if pending[0].Subharness != "swe" {
		t.Fatalf("the deferred repair forgot its worker: %q", pending[0].Subharness)
	}
	resumed, err := ResumeDeferredOverruns(context.Background(), graph, 0, twoNodeRemainder)
	if err != nil || resumed != 2 {
		t.Fatalf("resumed=%d err=%v", resumed, err)
	}
	continued, found, err := graph.Node("job-a-x1-n9")
	if err != nil || !found {
		t.Fatalf("the resumed repair is missing: found=%t err=%v", found, err)
	}
	if continued.Subharness != "swe" {
		t.Fatalf("the resumed repair came back on %q", continued.Subharness)
	}
}
