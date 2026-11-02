package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// oldEngineEnv names an older furrow release binary. The store-format tests
// below skip without it: they prove that a store written by that release opens
// and keeps capturing under the embedded engine, and the other way round.
const oldEngineEnv = "CODEAF_FURROW_OLD"

// TestStoreWrittenByAnOlderEngineOpensUnderTheEmbeddedOne seals and captures a
// cell with the old release, then with the embedded engine on the SAME data
// root, then with the old release again. The chain must continue unbroken in
// both directions: a user who upgrades keeps their history, and a codeaf that
// is rolled back still reads what the newer one wrote.
func TestStoreWrittenByAnOlderEngineOpensUnderTheEmbeddedOne(t *testing.T) {
	old := os.Getenv(oldEngineEnv)
	if old == "" {
		t.Skipf("%s not set", oldEngineEnv)
	}
	current, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("no embedded engine: %v", err)
	}
	if current == old {
		t.Skip("the embedded engine is the old engine")
	}
	ctx := context.Background()
	dataRoot := t.TempDir()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	engineOf := func(bin string) Engine {
		return Engine{Binary: bin, DataRoot: dataRoot, Transport: Spawn{Binary: bin}}
	}
	note := filepath.Join(c.Root, "note.txt")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(note, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("written by the old engine")
	first, err := engineOf(old).Seal(ctx, c, TurnInfo{})
	if err != nil {
		t.Fatalf("old engine seal: %v", err)
	}

	write("written under the new engine")
	second, err := engineOf(current).Seal(ctx, c, TurnInfo{})
	if err != nil {
		t.Fatalf("embedded engine could not seal into the old store: %v", err)
	}
	if second.Turn.Parent != first.Turn.ID {
		t.Fatalf("chain broke across the upgrade: parent %q, want %q", second.Turn.Parent, first.Turn.ID)
	}

	snap, err := engineOf(current).Capture(ctx, c)
	if err != nil || snap == "" {
		t.Fatalf("embedded engine capture in the old store: %q, %v", snap, err)
	}
	write("scribbled over")
	if err := engineOf(current).Restore(ctx, c, snap, []string{"note.txt"}); err != nil {
		t.Fatalf("embedded engine restore: %v", err)
	}
	if got, _ := os.ReadFile(note); string(got) != "written under the new engine" {
		t.Fatalf("restore gave %q", got)
	}

	write("written after rolling back")
	third, err := engineOf(old).Seal(ctx, c, TurnInfo{})
	if err != nil {
		t.Fatalf("old engine could not seal into a store the newer engine wrote: %v", err)
	}
	if third.Turn.Parent != second.Turn.ID {
		t.Fatalf("chain broke across the rollback: parent %q, want %q", third.Turn.Parent, second.Turn.ID)
	}
}
