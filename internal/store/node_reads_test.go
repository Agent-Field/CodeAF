package store

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestNodeIDPrefixReadsMatchTheGraph pins the indexed id-namespace reads to the
// literal string comparison they replaced, including the underscore that a
// pattern match would have treated as a wildcard.
func TestNodeIDPrefixReadsMatchTheGraph(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	for _, root := range []string{"job-a", "job-a-x1", "job-a-x10", "job-ab", "job_a-x3"} {
		if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
			{ID: root, Brief: "work named " + root},
		}}, Provenance{Origin: OriginUser, Intent: "prefix reads"}); err != nil {
			t.Fatalf("Splice %q: %v", root, err)
		}
	}
	if err := graph.Splice("job-a-x1", Subtree{Nodes: []NodeSpec{
		{ID: "job-a-x1-n5", Brief: "split leaf"},
	}}, Provenance{Origin: OriginSelf, Intent: "prefix reads"}); err != nil {
		t.Fatalf("Splice leaf: %v", err)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}

	for _, prefix := range []string{"job-a", "job-a-x1", "job-ab", "job-b", "job_a"} {
		want := false
		for _, node := range nodes {
			if node.ID == prefix || len(node.ID) > len(prefix) &&
				node.ID[:len(prefix)+1] == prefix+"-" {
				want = true
				break
			}
		}
		got, err := graph.NodeIDExistsWithPrefix(prefix)
		if err != nil {
			t.Fatalf("NodeIDExistsWithPrefix(%q): %v", prefix, err)
		}
		if got != want {
			t.Errorf("NodeIDExistsWithPrefix(%q) = %t, want %t", prefix, got, want)
		}
	}

	ids, err := graph.NodeIDsWithPrefix("job-a-x")
	if err != nil {
		t.Fatalf("NodeIDsWithPrefix: %v", err)
	}
	want := []string{"job-a-x1", "job-a-x1-n5", "job-a-x10"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("NodeIDsWithPrefix = %v, want %v", ids, want)
	}
	if ids, err := graph.NodeIDsWithPrefix("job_a-x"); err != nil ||
		!reflect.DeepEqual(ids, []string{"job_a-x3"}) {
		t.Errorf("NodeIDsWithPrefix underscore = %v, %v", ids, err)
	}
}

func TestPendingSiblingCountMatchesTheGraph(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "goal", Brief: "the job"},
		{ID: "first", Parent: "goal", Brief: "one"},
		{ID: "second", Parent: "goal", Brief: "two"},
		{ID: "third", Parent: "goal", Brief: "three"},
	}}, Provenance{Origin: OriginUser, Intent: "sibling counts"}); err != nil {
		t.Fatalf("Splice: %v", err)
	}
	compare := func(stage string) {
		t.Helper()
		nodes, err := graph.Nodes()
		if err != nil {
			t.Fatalf("Nodes: %v", err)
		}
		for _, node := range nodes {
			queued := 0
			for _, candidate := range nodes {
				if candidate.ID != node.ID && candidate.Parent == node.Parent &&
					candidate.Status == Pending {
					queued++
				}
			}
			got, err := graph.PendingSiblingCount(node.ID, node.Parent)
			if err != nil {
				t.Fatalf("PendingSiblingCount(%q): %v", node.ID, err)
			}
			if got != queued {
				t.Errorf("%s: PendingSiblingCount(%q) = %d, want %d", stage, node.ID, got, queued)
			}
		}
	}
	compare("all pending")
	claim := mustClaim(t, graph, "first", "runner")
	if err := graph.Start(claim); err != nil {
		t.Fatalf("Start: %v", err)
	}
	compare("one running")
}
