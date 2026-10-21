package syncsetup

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestC6TakeBackKeepsSealedUnuploadedTurns proves C6: moving a chat preserves its history and its single holder.
func TestC6TakeBackKeepsSealedUnuploadedTurns(t *testing.T) {
	ctx := context.Background()
	h := newTwoHomes(t)
	a := h.openA()
	a.mustSay("A initial")
	a.mustSay("A another uploaded turn")
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := first.Taken.Cell
	work := workspaceOf(at.Root)
	appendTo(t, transcriptOf(at), "{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"B sealed but never uploaded\"}\n")
	if err := os.WriteFile(filepath.Join(work, "B-unuploaded.txt"), []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	eng := h.engB
	eng.Workspace = work
	if _, err := eng.Seal(ctx, at, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if dirty, err := eng.Dirty(ctx, at); err != nil || dirty {
		t.Fatalf("fixture must have a clean sealed working tree; dirty=%v err=%v", dirty, err)
	}
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	again, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Taken.Kept == "" {
		_, statErr := os.Stat(filepath.Join(work, "B-unuploaded.txt"))
		t.Fatalf("sealed unuploaded B turn was not branched; B file after take=%v", statErr)
	}
	if again.KeptTurns != 1 {
		t.Fatalf("one unpublished turn was reported as %d kept turns", again.KeptTurns)
	}
	kept, err := h.continuerB().Take(ctx, again.Taken.Kept)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspaceOf(kept.Taken.Cell.Root), "B-unuploaded.txt"))
	if err != nil || string(raw) != "keep me" {
		t.Fatalf("kept branch lost the unpublished file: %q, %v", raw, err)
	}
	raw, err = os.ReadFile(transcriptOf(kept.Taken.Cell))
	if err != nil || !strings.Contains(string(raw), "B sealed but never uploaded") {
		t.Fatalf("kept branch lost the unpublished transcript: %s, %v", raw, err)
	}
}
