package standing

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ownedItem is a file watch in workspace that keeps report current.
func ownedItem(workspace, report string) Item {
	return Item{
		Words: "keep " + report + " current", Workspace: workspace,
		When:  When{Kind: WhenFile, Glob: "inbox/*"},
		Does:  Action{Kind: ActionTask, Brief: "Summarise inbox/.", Report: report},
		Rails: Rails{MaxPerDay: DefaultMaxPerDay, PerRunUSD: DefaultPerRunUSD},
	}
}

// A REPORT PATH HAS ONE LIVE OWNER, kept on its own record: a second item is
// refused as [ErrReportOwned], the owner's stop frees the path, and the record
// goes with the stop.
func TestAReportPathKeepsOneLiveOwnerOnItsRecord(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	first, err := store.Create(ownedItem(workspace, "reports/r.md"))
	if err != nil {
		t.Fatal(err)
	}
	path := ReportPath(first)
	if owner, live := store.LiveOwner(path); !live || owner.ID != first.ID {
		t.Fatalf("the path's owner = %+v (live %v), want %s", owner, live, first.ID)
	}
	var owned *ReportOwnedError
	if _, err := store.Create(ownedItem(workspace, "reports/./r.md")); !errors.Is(err, ErrReportOwned) || !errors.As(err, &owned) || owned.Owner.ID != first.ID {
		t.Fatalf("a second owner spelled another way was not refused as the first's: %v", err)
	}
	if _, err := store.SetStatus(first.ID, StatusRetired, StoppedWhy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.pathRecord(path, ".owner.json")); !os.IsNotExist(err) {
		t.Fatalf("a stopped owner's record was kept (%v)", err)
	}
	second, err := store.Create(ownedItem(workspace, "reports/r.md"))
	if err != nil {
		t.Fatalf("a successor after the stop was refused: %v", err)
	}
	moved, _, err := store.Revise(second.ID, second.SpecRevision, func(item *Item) error {
		item.Does.Report = "reports/elsewhere.md"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, live := store.LiveOwner(path); live {
		t.Fatal("a report moved elsewhere kept its claim on the old path")
	}
	if owner, live := store.LiveOwner(ReportPath(moved)); !live || owner.ID != second.ID {
		t.Fatalf("the moved report's path is not its: %+v", owner)
	}
}

// THE OWNERS ALREADY THERE ARE RECORDED ONCE, and the one that published keeps
// the path: two live items from before the record, one of which published
// under its own receipt, and a third that is stopped.
func TestTheOwnersBeforeTheRecordAreRecordedOnce(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	older, newer := ownedItem(workspace, "reports/r.md"), ownedItem(workspace, "reports/r.md")
	older.ID, newer.ID = "0000000000000001", "0000000000000002"
	older.Status, newer.Status = StatusActive, StatusActive
	older.Created, newer.Created = store.now().Add(-2), store.now().Add(-1)
	for _, item := range []Item{older, newer} {
		if err := store.writeUnlocked(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(store.ItemDir(newer.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.ItemDir(newer.ID), publicationsFile), []byte(`{"reports/r.md":{"path":"reports/r.md","sha256":"newer","bytes":5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := ReportPath(older)
	if owner, live := store.LiveOwner(path); !live || owner.ID != newer.ID {
		t.Fatalf("the recorded owner = %+v, want the one that published (%s)", owner, newer.ID)
	}
	if receipt, err := store.Receipt(path); err != nil || receipt == nil || receipt.SHA256 != "newer" {
		t.Fatalf("its receipt was not moved to the path: %+v (%v)", receipt, err)
	}
	if _, err := os.Stat(filepath.Join(store.root, receiptsDir, ownersMarker)); err != nil {
		t.Fatalf("the one-time record left no marker: %v", err)
	}
}
