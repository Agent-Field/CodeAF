package resident

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// stalledJob is the shape ofetch v4-flash s13 ended in: a top-level job root
// waiting on parts that have not started, with the wall about to arrive.
func stalledJob(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "closeout.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	specs := []store.NodeSpec{{ID: "task-2-x1", Brief: "the whole job", Title: "The job"}}
	for _, id := range []string{"task-2-x1-n1", "task-2-x1-n2", "task-2-x1-n3"} {
		specs = append(specs, store.NodeSpec{ID: id, Parent: "task-2-x1", Brief: "a piece", Title: "A piece"})
		specs[0].Needs = append(specs[0].Needs, store.Need{NodeID: id, Kind: store.FeedsInto})
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: specs},
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "add a circuit breaker"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

func readyIDs(t *testing.T, graph *store.Store) []string {
	t.Helper()
	nodes, err := graph.Ready(20)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

// A GATE ALWAYS PRECEDES THE WALL.
//
// ofetch v4-flash s13 refused growth twice in the last ninety seconds of a
// 5400-second wall and kept thirteen pending leaves and a pending job root, so
// nothing ever settled and the run journaled no delivery judgement at all. A
// refusal taken that near the wall now clears the way to the root instead.
func TestClosingOutAJobLeavesItsRootReadyToBeJudged(t *testing.T) {
	graph := stalledJob(t)
	if ready := readyIDs(t, graph); len(ready) != 3 {
		t.Fatalf("the job's parts are what is ready before the close-out: %v", ready)
	}
	runner := NewRunner(graph, nil, "test", 1)

	stopped := runner.CloseOut("task-2-x1", "", "there was not enough time left")
	if stopped != 3 {
		t.Fatalf("stopped %d parts, want all three", stopped)
	}
	for _, id := range []string{"task-2-x1-n1", "task-2-x1-n2", "task-2-x1-n3"} {
		if node := jobNode(t, graph, id); node.Status != store.Cancelled {
			t.Fatalf("%s is %s, want retired so the root can settle", id, node.Status)
		}
	}
	ready := readyIDs(t, graph)
	if len(ready) != 1 || ready[0] != "task-2-x1" {
		t.Fatalf("ready = %v, want the job root and nothing else", ready)
	}
	// The node the refusal is about is left alone: stopping the worker that is
	// asking would release the claim it is settling and put the row back on the
	// queue.
	graph = stalledJob(t)
	runner = NewRunner(graph, nil, "test", 1)
	if stopped := runner.CloseOut("task-2-x1", "task-2-x1-n2", "no time left"); stopped != 2 {
		t.Fatalf("stopped %d parts, want the two that were not asking", stopped)
	}
	if node := jobNode(t, graph, "task-2-x1-n2"); node.Status != store.Pending {
		t.Fatalf("the node whose landing asked was stopped underneath it: %s", node.Status)
	}
}

// And the refusal is what asks. A governor that declines the round and leaves
// the queue alone has stopped the growth and not the run.
func TestARefusalAtTheWallClosesTheJobOut(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)
	node := jobNode(t, graph, "task-2")

	// Two admitted rounds, far enough apart that the job's own pace is longer
	// than the wall it has left.
	for _, name := range []string{"src/grid.ts", "src/box.ts"} {
		request := GrowRequest{JobRoot: "task-2", Node: node, Lineage: "task-2",
			Reason: GrowOverrun, Round: 1, Measured: true, Workspace: root,
			Artifacts: scratchRun(t, root, name)}
		admitGrowth(graph, request, GrowVerdict{Allow: true, Round: 1}, 2)
		time.Sleep(60 * time.Millisecond)
	}

	var closedJob, keptNode, reason string
	SetJobCloser(func(jobRoot, keep, words string) int {
		closedJob, keptNode, reason = jobRoot, keep, words
		return 1
	})
	t.Cleanup(func() { SetJobCloser(nil) })

	tight, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	verdict, err := growJob(tight, graph, nil, GrowRequest{
		JobRoot: "task-2", Node: node, Lineage: "task-2", Reason: GrowOverrun,
		Round: 3, Measured: true, Workspace: root,
		Artifacts: scratchRun(t, root, "src/index.ts")})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Allow || verdict.Cause != CauseOutOfWall {
		t.Fatalf("round bought with no wall left to finish it: %+v", verdict)
	}
	if closedJob != "task-2" || keptNode != "task-2" || reason == "" {
		t.Fatalf("the refusal left the job's queued work running: job=%q keep=%q reason=%q",
			closedJob, keptNode, reason)
	}
}
