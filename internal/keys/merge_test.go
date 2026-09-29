package keys

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// twoHomes returns two vaults of one identity, as two of a person's machines.
func twoHomes(t *testing.T) (a, b *Vault) {
	t.Helper()
	a, homeA := mustOpen(t)
	id, err := identity.Load(homeA)
	if err != nil {
		t.Fatal(err)
	}
	homeB := filepath.Join(t.TempDir(), "home")
	if err := identity.Save(homeB, id); err != nil {
		t.Fatal(err)
	}
	b, err = Open(homeB)
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

// setUpdated rewrites the vault document with a chosen `updated`, so a test does
// not depend on the wall clock.
func setUpdated(t *testing.T, v *Vault, updated int64) {
	t.Helper()
	d, err := v.read()
	if err != nil {
		t.Fatal(err)
	}
	d.Updated = updated
	if err := v.write(d); err != nil {
		t.Fatal(err)
	}
}

func mergeFrom(t *testing.T, into, from *Vault) {
	t.Helper()
	enc, err := from.Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := into.Merge(enc); err != nil {
		t.Fatal(err)
	}
}

func TestMergeIsTheUnionByID(t *testing.T) {
	a, b := twoHomes(t)
	a.Put("s1", Entry{"A", "1", "p"})
	b.Put("s2", Entry{"B", "2", "p"})
	mergeFrom(t, a, b)
	got, _ := a.Env("p")
	if want := []string{"A=1", "B=2"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMergeNewerUpdatedWinsOnTheSameID(t *testing.T) {
	a, b := twoHomes(t)
	a.Put("s1", Entry{"K", "old", "p"})
	b.Put("s1", Entry{"K", "new", "p"})
	setUpdated(t, a, 100)
	setUpdated(t, b, 200)
	mergeFrom(t, a, b)
	if e, _ := a.Get("s1"); e.Value != "new" {
		t.Fatalf("the newer copy must win, got %q", e.Value)
	}
	setUpdated(t, a, 300) // now a is newer, and b's older copy must lose
	mergeFrom(t, a, b)
	if e, _ := a.Get("s1"); e.Value != "new" {
		t.Fatalf("an older copy must not win, got %q", e.Value)
	}
	b.Put("s1", Entry{"K", "older", "p"})
	setUpdated(t, b, 50)
	mergeFrom(t, a, b)
	if e, _ := a.Get("s1"); e.Value != "new" {
		t.Fatalf("an older copy must not win, got %q", e.Value)
	}
}

func TestMergeKeepsTheNewerStampAndIsIdempotent(t *testing.T) {
	a, b := twoHomes(t)
	b.Put("s1", Entry{"K", "v", "p"})
	setUpdated(t, b, 500)
	mergeFrom(t, a, b)
	if d, _ := a.read(); d.Updated != 500 {
		t.Fatalf("merge must keep the newer stamp, got %d", d.Updated)
	}
	first, _ := statMod(a.path)
	mergeFrom(t, a, b)
	if again, _ := statMod(a.path); again != first {
		t.Fatal("a merge that changes nothing must not rewrite the vault")
	}
}

func TestMergeRefusesWhatItCannotOpen(t *testing.T) {
	a, _ := twoHomes(t)
	a.Put("s1", Entry{"K", "v", "p"})
	other, _ := mustOpen(t) // a different identity
	enc, _ := other.Export()
	if err := a.Merge(enc); err == nil {
		t.Fatal("a vault sealed under another key must be refused")
	}
	if err := a.Merge([]byte("not an envelope")); err == nil {
		t.Fatal("garbage must be refused")
	}
	if e, err := a.Get("s1"); err != nil || e.Value != "v" {
		t.Fatalf("the local vault must be untouched: %+v %v", e, err)
	}
}

func TestExportOfAnEmptyVaultMergesAsNothing(t *testing.T) {
	a, b := twoHomes(t)
	a.Put("s1", Entry{"K", "v", "p"})
	mergeFrom(t, a, b)
	if e, _ := a.Get("s1"); e.Value != "v" {
		t.Fatal("merging an empty vault must not remove anything")
	}
}

func TestEntriesAreSortedByName(t *testing.T) {
	v, _ := mustOpen(t)
	v.Put("1", Entry{"A0", "x", "p"})
	v.Put("2", Entry{"A", "y", "p"})
	v.Put("3", Entry{"B", "z", "q"})
	got, _ := v.Entries("p")
	if len(got) != 2 || got[0].Name != "A" || got[1].Name != "A0" {
		t.Fatalf("got %+v", got)
	}
}

func statMod(path string) (int64, error) {
	i, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return i.ModTime().UnixNano(), nil
}
