package syncsetup

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestC6ReturningAfterReleaseKeepsNewerHistory proves C6: moving a chat preserves its history and its single holder.
func TestC6ReturningAfterReleaseKeepsNewerHistory(t *testing.T) {
	ctx := context.Background()
	h := newTwoHomes(t)
	a := h.openA()
	a.mustSay("A first")
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	took, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("B unique work")
	if err := b.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	back := h.openA()
	if err := back.drive.Gate(); err == nil {
		t.Fatal("a stale reopened tree was writable before continuing the newer history")
	}
	resumed, err := h.continuerA().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	back = h.openOn(h.a, h.engine, resumed.Taken.Cell, h.work, nameA)
	back.mustSay("A returned after continue here")
	if err := back.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	arrived, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(transcriptOf(arrived.Taken.Cell))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "B unique work") {
		t.Fatalf("reopening A replaced B's released history; B received transcript=%s", raw)
	}
}
