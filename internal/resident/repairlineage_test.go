package resident

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// §13.3's second half, pinned.
//
// A delivery gate fires on a job ROOT and nowhere else, so the node a gate
// repair is planned from is routinely a sink whose children hold the work. One
// measured run rejected a twelve-part job on a false negative, and the repair it
// bought was wired to the sink alone: it could reach none of the twelve finished
// siblings, said so — "the workspace is empty ... the trace log holds no profile
// content" — correctly refused to invent the missing ten, and handed back two.
// A correct answer, already bought, was replaced by a broken one.
//
// What this pins is that the repair is born knowing its siblings: every finished
// part of the failed job arrives as one of its inputs, in the order the job made
// them, alongside the job's own result.
func TestGateRepairConsumesTheFinishedSiblings(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-12", Brief: "twelve profiles, one message"},
		{ID: "task-12-n1", Parent: "task-12", Brief: "Milvus"},
		{ID: "task-12-n2", Parent: "task-12", Brief: "Qdrant"},
		{ID: "task-12-n3", Parent: "task-12", Brief: "FAISS"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "profile twelve vector databases"}); err != nil {
		t.Fatal(err)
	}
	// Two parts land with results; one dies. Only the landed pair has anything a
	// repair could consume, and a dependency on the dead one would be a
	// dependency on an empty digest.
	settle(t, graph, "task-12-n1", "**Milvus** — a shared-storage architecture", true)
	settle(t, graph, "task-12-n2", "**Qdrant** — written in Rust", true)
	settle(t, graph, "task-12-n3", "", false)

	claim, ok, err := graph.Claim("task-12", "w1")
	if err != nil || !ok {
		t.Fatalf("claim job root: ok=%t err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	root, _, _ := graph.Node("task-12")

	planned := func(ctx context.Context, goal, prefix string) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: prefix + "-n1", Brief: "assemble the twelve", Title: "Assemble"},
		}}, nil
	}
	spliced, sink, err := ReplanOverrunAs(context.Background(), graph, root,
		"two of twelve", "the other ten profiles are missing", nil, 0, "",
		Growth{Reason: GrowGap}, planned)
	if err != nil || spliced != 1 {
		t.Fatalf("replan: spliced=%d sink=%q err=%v", spliced, sink, err)
	}

	repair, found, err := graph.Node(sink)
	if err != nil || !found {
		t.Fatalf("repair sink %q: found=%t err=%v", sink, found, err)
	}
	// The delivery law: only a root's completion is announced, so a repair that
	// is the finished assignment has to stand where a deliverable stands.
	if repair.Parent != store.RootID {
		t.Fatalf("repair parent = %q, want the root so its result is announced", repair.Parent)
	}
	edges, err := graph.ActiveEdges()
	if err != nil {
		t.Fatal(err)
	}
	feeds := map[string]bool{}
	for _, edge := range edges {
		if edge.To == sink && edge.Kind == store.FeedsInto {
			feeds[edge.From] = true
		}
	}
	for _, want := range []string{"task-12", "task-12-n1", "task-12-n2"} {
		if !feeds[want] {
			t.Fatalf("repair %s does not consume %s — its lineage is lost (edges into it: %v)", sink, want, feeds)
		}
	}
	if feeds["task-12-n3"] {
		t.Fatalf("repair consumes the part that never landed; it has no result to give")
	}
}

func settle(t *testing.T, graph *store.Store, id, summary string, done bool) {
	t.Helper()
	claim, ok, err := graph.Claim(id, "w0")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if done {
		if err := graph.Complete(claim, summary); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := graph.Fail(claim, "it stopped"); err != nil {
		t.Fatal(err)
	}
}
