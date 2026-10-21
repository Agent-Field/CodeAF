package syncsetup

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestC6TakeBackKeepsUnsealedTranscriptOnlyWork proves C6: a reply that
// reached the journal but no seal or upload survives the same takeover path.
func TestC6TakeBackKeepsUnsealedTranscriptOnlyWork(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	a := h.openA()
	a.mustSay("initial")
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	appendTo(t, transcriptOf(first.Taken.Cell), `{"type":"message","role":"assistant","content":"reply not sealed or uploaded"}`+"\n")
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	again, err := h.continuerB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Taken.Kept == "" {
		t.Fatal("taking the chat back lost a journaled reply with no workspace edit")
	}
	branch, err := h.continuerB().Take(ctx, again.Taken.Kept)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(transcriptOf(branch.Taken.Cell))
	if err != nil || !strings.Contains(string(raw), "reply not sealed or uploaded") {
		t.Fatalf("kept branch lost the reply: %s, %v", raw, err)
	}
}
