package store

import (
	"testing"
	"time"
)

// The stale-claim reaper: a node stuck running past its age returns to
// pending; a node inside it is left alone. This is the live-lock the
// long-horizon run died of — a hung worker blocking the whole downstream DAG.
func TestReleaseStaleReleasesOnlyOldRunningNodes(t *testing.T) {
	graph := openTestStore(t, t.TempDir()+"/stale.db")
	defer graph.Close()

	// Two nodes, both claimed and started.
	for _, id := range []string{"task-stale", "task-fresh"} {
		err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: id, Brief: "x"}}}, Provenance{Origin: OriginSelf})
		if err != nil {
			t.Fatalf("splice %s: %v", id, err)
		}
		claim, ok, err := graph.Claim(id, "runner")
		if err != nil || !ok {
			t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}

	// Backdate task-stale's started_at past the threshold by rewriting it
	// directly — the reaper reads the column, not the clock it was stamped
	// from, so the test does not wait twenty minutes.
	old := formatTime(time.Now().Add(-25 * time.Minute))
	if _, err := graph.db.Exec(`UPDATE nodes SET started_at = ? WHERE id = ?`, old, "task-stale"); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	released, err := graph.ReleaseStale(20 * time.Minute)
	if err != nil {
		t.Fatalf("ReleaseStale: %v", err)
	}
	if len(released) != 1 || released[0] != "task-stale" {
		t.Fatalf("released = %v, want [task-stale]", released)
	}

	// task-stale is pending again and claimable; task-fresh is untouched.
	node, _, err := graph.Node("task-stale")
	if err != nil {
		t.Fatalf("read task-stale: %v", err)
	}
	if node.Status != Pending {
		t.Fatalf("task-stale status = %s, want pending", node.Status)
	}
	node, _, err = graph.Node("task-fresh")
	if err != nil {
		t.Fatalf("read task-fresh: %v", err)
	}
	if node.Status != Running {
		t.Fatalf("task-fresh status = %s, want running", node.Status)
	}
}
