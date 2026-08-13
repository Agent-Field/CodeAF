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

// The floor under every scheduler that will ever exist, in the shape that
// exposed the need for one: three researchers fanned out and an assembler that
// reads all three.
//
// The probe's assembler reached done while the second researcher was still
// running, shipped a brief with that country's section invented, and the person
// was told all three had been pulled together. The edges were the thing that
// went missing there — the layout that admitted those four nodes recorded none
// — so this pins the other half: wherever the inputs ARE recorded, no caller,
// no ordering, no race and no retry may start a node while one of them is still
// live. It is a property of the write, not of the queue that leads to it, which
// is why both the offer and the claim are checked and why the claim is tried
// directly rather than through anything that reads Ready first.
func TestAnAssemblerIsUnclaimableUntilEveryInputIsTerminal(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "fanout.db"))

	researchers := []string{"task-8-n1", "task-8-n2", "task-8-n3"}
	const assembler = "task-8-n4"
	nodes := []NodeSpec{{ID: "task-8", Brief: "Compare how three countries measure road distance", Stage: 2}}
	inputs := make([]Need, 0, len(researchers))
	for _, id := range researchers {
		nodes = append(nodes, NodeSpec{ID: id, Parent: "task-8", Brief: "research " + id, Stage: 1})
		inputs = append(inputs, Need{NodeID: id, Kind: FeedsInto})
	}
	nodes = append(nodes, NodeSpec{ID: assembler, Parent: "task-8", Brief: "assemble the three sections",
		Stage: 1, Needs: inputs})
	nodes[0].Needs = append(inputs, Need{NodeID: assembler, Kind: FeedsInto})
	if err := graph.Splice(RootID, Subtree{Nodes: nodes},
		Provenance{Origin: OriginUser, SessionID: "s1", Intent: "compare three countries"}); err != nil {
		t.Fatalf("splice fan-out: %v", err)
	}

	offered := func() map[string]bool {
		t.Helper()
		ready, err := graph.Ready(0)
		if err != nil {
			t.Fatalf("Ready: %v", err)
		}
		names := make(map[string]bool, len(ready))
		for _, node := range ready {
			names[node.ID] = true
		}
		return names
	}

	// Land the researchers one at a time. Until the last of them is terminal
	// the assembler is neither offered nor takeable — including at the moment
	// two of its three inputs are already done, which is exactly the state the
	// probe's assembler started in.
	for index, id := range researchers {
		if names := offered(); names[assembler] {
			t.Fatalf("the assembler was offered with %d of 3 sections written", index)
		}
		if _, won, err := graph.Claim(assembler, "runner"); err != nil || won {
			t.Fatalf("the assembler was claimable with %d of 3 sections written: won=%v err=%v", index, won, err)
		}
		claim, won, err := graph.Claim(id, "runner")
		if err != nil || !won {
			t.Fatalf("claim %s: won=%v err=%v", id, won, err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		// Running, not merely claimed: a live input is the case that shipped.
		if _, won, err := graph.Claim(assembler, "runner"); err != nil || won {
			t.Fatalf("the assembler was claimable while %s was running: won=%v err=%v", id, won, err)
		}
		if err := graph.Complete(claim, "section for "+id); err != nil {
			t.Fatalf("complete %s: %v", id, err)
		}
	}

	if names := offered(); !names[assembler] {
		t.Fatal("the assembler is still not offered with every section written")
	}
	claim, won, err := graph.Claim(assembler, "runner")
	if err != nil || !won {
		t.Fatalf("claim the assembler: won=%v err=%v", won, err)
	}
	// And the sink waits for the assembler in turn, which is why the probe's
	// root was correctly pending while the assembler read done — the parent of
	// a bundle is a node with edges, not a projection of its children.
	if names := offered(); names["task-8"] {
		t.Fatal("the delivery was offered while the assembler was still claimed")
	}
	if err := graph.Complete(claim, "the comparison brief"); err != nil {
		t.Fatalf("complete the assembler: %v", err)
	}
	if names := offered(); !names["task-8"] {
		t.Fatal("the delivery never became ready")
	}

	// A failed input is terminal too: the assembler runs and says what is
	// missing rather than waiting forever on work that will never land.
	dead := openTestStore(t, filepath.Join(t.TempDir(), "failed.db"))
	if err := dead.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "assemble", Needs: []Need{{NodeID: "part", Kind: FeedsInto}}},
		{ID: "part", Parent: "job", Brief: "research"},
	}}, Provenance{Origin: OriginUser, Intent: "one part, one assembler"}); err != nil {
		t.Fatalf("splice failing shape: %v", err)
	}
	partClaim, won, err := dead.Claim("part", "runner")
	if err != nil || !won {
		t.Fatalf("claim part: won=%v err=%v", won, err)
	}
	if err := dead.Fail(partClaim, "the source was down"); err != nil {
		t.Fatalf("fail part: %v", err)
	}
	if _, won, err := dead.Claim("job", "runner"); err != nil || !won {
		t.Fatalf("a failed input must not wedge its consumer: won=%v err=%v", won, err)
	}
}
