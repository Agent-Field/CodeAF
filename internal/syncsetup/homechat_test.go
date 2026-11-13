package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// homeIs makes dir the home folder this process reports, as each machine of a
// two-homes test has a home of its own.
func homeIs(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

// TestAHomeChatNeverLandsInTheSendersHomePathOnB: A's chat works in A's home
// folder, so A saves the conversation and never the folder's files. On B the
// same path exists and is not B's home (a different person's folder, or a
// same-named one). Neither B's take, nor B's own turns, nor B's take in place
// after A took the chat back may restore the chat's files into that folder or
// capture it: B keeps the chat in a folder of the chat's own. (A first arrival
// is a copy of its own, see fileCopy; the chat's own-folder mark keeps every
// later seal and restore there, see cellstore.ForeignFolder.)
func TestAHomeChatNeverLandsInTheSendersHomePathOnB(t *testing.T) {
	h := newTwoHomes(t)
	ctx := context.Background()
	homeB := t.TempDir()
	precious := filepath.Join(h.work, "precious.txt")
	if err := os.WriteFile(precious, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	onA := func() { homeIs(t, h.work); h.backToA() }
	onB := func() *Continuer {
		homeIs(t, homeB)
		cb := h.continuerB()
		h.backToA() // A's home path exists on B too
		return cb
	}

	onA()
	a := h.openA()
	a.mustSay("a one")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	took, err := onB().Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	rootB := took.Taken.Cell.Root
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(rootB), nameB)
	b.mustSay("b one")
	h.durable(h.cell.ID, took.Taken.Cell)
	if err := b.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	onA()
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	a2 := h.openA()
	a2.mustSay("a two")
	h.durable(h.cell.ID, h.cell)
	if err := a2.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := onB().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	if got := workspaceOf(rootB); got == h.link || got == h.work {
		t.Fatalf("B works in A's home path %s", got)
	}
	if body, err := os.ReadFile(precious); err != nil || string(body) != "mine\n" {
		t.Fatalf("a file in A's home path was changed: %q, %v", body, err)
	}
	for _, chatFile := range []string{"meta.json", ".cell", "transcript.jsonl"} {
		if _, err := os.Lstat(filepath.Join(h.work, chatFile)); err == nil {
			t.Fatalf("the chat's %s was restored into A's home path", chatFile)
		}
	}
}
