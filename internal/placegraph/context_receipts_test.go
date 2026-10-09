package placegraph

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
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

func TestContextReceiptRejectsUnboundedOrAmbiguousAuthority(t *testing.T) {
	s, path := newStore(t)
	_, rc, err := s.CreatePlace(NewPlace{Name: "Minimal"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path + ".context-receipts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fields []map[string]any
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || len(fields[0]) != 3 {
		t.Fatalf("not minimal: %s", data)
	}
	good := contextReceipt{rc.ID, rc.BeforeRevision, rc.AfterRevision}
	tests := map[string][]byte{
		"oversize":          []byte(strings.Repeat(" ", contextReceiptByteLimit+1)),
		"duplicateID":       mustJSON(t, []contextReceipt{good, good}),
		"duplicateRevision": mustJSON(t, []contextReceipt{good, {"rc_other", good.Before, good.After}}),
		"malformedID":       mustJSON(t, []contextReceipt{{"../../rc_bad", good.Before, good.After}}),
		"nonCommit":         mustJSON(t, []contextReceipt{{"rc_bad", 1, 4}}),
		"overflow":          mustJSON(t, []contextReceipt{{"rc_bad", ^uint64(0), 0}}),
		"tooMany":           mustJSON(t, make([]contextReceipt, MaxUndo+1)),
	}
	for name, bytes := range tests {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path+".context-receipts.json", bytes, 0600); err != nil {
				t.Fatal(err)
			}
			if got := ContextUndoReceipt(path, good.Before, good.After); len(got) != 0 {
				t.Fatalf("granted invalid authority: %v", got)
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
