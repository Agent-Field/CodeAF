package store

import (
	"path/filepath"
	"testing"
)

// The exact corruption the fresh-store bug produced: ReleaseOrphans released
// the always-Running spine root, a runner claimed and "completed" the trunk,
// and every later splice failed on a closed root. Every layer must now hold.
func TestSpineRootIsNeverOrphanWorkAndSelfHeals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spine.db")
	graph := openTestStore(t, path)

	// Layer 1: ReleaseOrphans on a fresh store touches nothing — the root's
	// Running status is structural, not a stale claim.
	released, err := graph.ReleaseOrphans()
	if err != nil {
		t.Fatalf("ReleaseOrphans: %v", err)
	}
	if len(released) != 0 {
		t.Fatalf("fresh store released orphans: %v", released)
	}

	// Layer 2: the root is never claimable, and never ready work.
	if _, won, err := graph.Claim(RootID, "runner"); err != nil || won {
		t.Fatalf("root claim: won=%v err=%v — the spine must refuse claims", won, err)
	}
	ready, err := graph.Ready(10)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	for _, node := range ready {
		if node.ID == RootID {
			t.Fatal("Ready returned the spine root")
		}
	}

	// Layer 3: releasing the root by hand is refused.
	if err := graph.Release(Claim{ID: RootID, Owner: "", Token: 0}); err == nil {
		t.Fatal("Release accepted the spine root")
	}

	// Layer 4: a store already corrupted (root closed) self-heals at open,
	// journal-consistently, and splices work again after the repair.
	if _, err := graph.db.Exec(
		`UPDATE nodes SET status = ?, owner = 'chat-runner' WHERE id = ?`, Done, RootID); err != nil {
		t.Fatalf("corrupt root: %v", err)
	}
	if err := graph.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	healed := openTestStore(t, path)
	node, ok, err := healed.Node(RootID)
	if err != nil || !ok {
		t.Fatalf("root after reopen: ok=%v err=%v", ok, err)
	}
	if node.Status != Running {
		t.Fatalf("root not healed: status %q", node.Status)
	}
	if err := healed.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: "job-after-heal", Brief: "works again"}}},
		Provenance{Origin: OriginUser, Intent: "prove the trunk is open"}); err != nil {
		t.Fatalf("splice after heal: %v", err)
	}
	// Rebuild must reproduce the healed state, not replay back to done.
	if err := healed.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	node, _, err = healed.Node(RootID)
	if err != nil || node.Status != Running {
		t.Fatalf("root after Rebuild: status %q err %v", node.Status, err)
	}
}
