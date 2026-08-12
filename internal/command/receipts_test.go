package command

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// TestASubtreesReceiptsReachTheWindowAndAWindowWithoutOneSaysSoRatherThanZero
// locks both halves of the seam 13.11 needed: a surface can ask what each level
// of a tree cost without knowing what a usage row is, and a window that cannot
// see the journal answers absence rather than a number it made up.
func TestASubtreesReceiptsReachTheWindowAndAWindowWithoutOneSaysSoRatherThanZero(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "the errand", Stage: 2},
		{ID: "part", Parent: "job", Brief: "a part of it", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: "the errand"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "part", Cost: 0.25}); err != nil {
		t.Fatal(err)
	}

	commander := New(Options{Store: graph})
	ledger, err := commander.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	part, found := ledger.Receipt("part")
	if !found || !part.Billed() || part.Cost != 0.25 {
		t.Fatalf("the part's receipt = %+v (found %v); want $0.25 at the window", part, found)
	}
	// The root's own line carries no money; its branch carries the part's.
	root, found := ledger.Receipt("job")
	if !found || root.Billed() {
		t.Fatalf("the root's receipt = %+v (found %v); want a line with no money of its own", root, found)
	}
	roll, found, err := commander.SubtreeRollup("job")
	if err != nil || !found {
		t.Fatalf("SubtreeRollup = found %v, err %v", found, err)
	}
	if roll.Nodes != 2 || roll.Cost != 0.25 {
		t.Fatalf("rollup at the window = %+v; want two nodes at $0.25", roll)
	}

	// A visitor window holds no store. Every lookup in what it answers with is
	// absent, which is the — the renderer draws, and never $0.00.
	visitor := New(Options{})
	blind, err := visitor.SubtreeReceipts("job")
	if err != nil {
		t.Fatalf("a window with no store returned an error; want an empty ledger: %v", err)
	}
	if _, found := blind.Receipt("part"); found {
		t.Fatalf("a window with no store answered with a receipt; want absence")
	}
	if _, found, err := visitor.SubtreeRollup("job"); found || err != nil {
		t.Fatalf("a window with no store = found %v, err %v; want absence", found, err)
	}

	// And a root nobody has heard of is absent too, at a window that CAN see
	// the journal — the two absences are the same glyph and neither is a zero.
	if _, found, err := commander.SubtreeRollup("no-such-job"); found || err != nil {
		t.Fatalf("an unknown root = found %v, err %v; want absence", found, err)
	}
}
