package placegraph

import (
	"errors"
	"os"
	"testing"
)

func TestContextReceiptCrossProcessProvenanceAndStrictUndo(t *testing.T) {
	s, path := newStore(t)
	a, b := mk(t, s, "A"), mk(t, s, "B")
	_, first, err := s.AddChat("chat", a.ID, AddedByYou)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := s.AddChat("chat", b.ID, AddedByYou)
	if err != nil {
		t.Fatal(err)
	}
	// Read the file contract without access to the mutating Store.
	got := ContextUndoReceipt(path, first.BeforeRevision, first.AfterRevision)
	if len(got) != 1 || got[0] != first.ID {
		t.Fatalf("wrong receipt: %v", got)
	}
	if got := ContextUndoReceipt(path, first.BeforeRevision, second.AfterRevision); len(got) != 0 {
		t.Fatalf("guessed multicommit: %v", got)
	}
	if _, err := s.Undo(first.ID); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("older undo: %v", err)
	}
	if len(snap(t, s).Memberships) != 2 {
		t.Fatal("refusal lost membership")
	}
	if _, err := s.Undo(second.ID); err != nil {
		t.Fatal(err)
	}
	if len(snap(t, s).Memberships) != 1 {
		t.Fatal("latest undo did not remove only latest")
	}
	if _, err := s.Undo("unknown"); !errors.Is(err, ErrNoReceipt) {
		t.Fatal(err)
	}
	reopened, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Undo(first.ID); !errors.Is(err, ErrNoReceipt) {
		t.Fatalf("restart invented undo authority: %v", err)
	}
	if err = os.WriteFile(path+".context-receipts.json", []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ContextUndoReceipt(path, first.BeforeRevision, first.AfterRevision); len(got) != 0 {
		t.Fatal(got)
	}
}
