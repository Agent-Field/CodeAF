package keys

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// setStamp rewrites one slot's stamp (and the document's, when later), so a
// test does not depend on the wall clock.
func setStamp(t *testing.T, v *Vault, id string, ts int64) {
	t.Helper()
	d, err := v.read()
	if err != nil {
		t.Fatal(err)
	}
	r := d.Secrets[id]
	r.Updated = ts
	d.Secrets[id], d.Updated = r, max(d.Updated, ts)
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

func TestMergeNewerStampWinsOnTheSameID(t *testing.T) {
	a, b := twoHomes(t)
	a.Put("s1", Entry{"K", "old", "p"})
	b.Put("s1", Entry{"K", "new", "p"})
	setStamp(t, a, "s1", 100)
	setStamp(t, b, "s1", 200)
	mergeFrom(t, a, b)
	if e, _ := a.Get("s1"); e.Value != "new" {
		t.Fatalf("the newer copy must win, got %q", e.Value)
	}
	b.Put("s1", Entry{"K", "older", "p"})
	setStamp(t, b, "s1", 50)
	mergeFrom(t, a, b)
	if e, _ := a.Get("s1"); e.Value != "new" {
		t.Fatalf("an older copy must not win, got %q", e.Value)
	}
}

func TestMergeKeepsTheNewerStampAndIsIdempotent(t *testing.T) {
	a, b := twoHomes(t)
	b.Put("s1", Entry{"K", "v", "p"})
	setStamp(t, b, "s1", 500)
	mergeFrom(t, a, b)
	if d, _ := a.read(); d.Secrets["s1"].Updated != 500 {
		t.Fatalf("merge must keep the slot stamp, got %d", d.Secrets["s1"].Updated)
	}
	first, _ := statMod(a.path)
	mergeFrom(t, a, b)
	if again, _ := statMod(a.path); again != first {
		t.Fatal("a merge that changes nothing must not rewrite the vault")
	}
}

func TestDeleteSurvivesMergeWithAnOlderCopy(t *testing.T) {
	a, b := twoHomes(t)
	a.Put("leaked", Entry{"KEY", "v", "p"})
	mergeFrom(t, b, a) // b holds the secret too
	must(t, a.Delete("leaked"))
	mergeFrom(t, a, b) // b's older copy must not resurrect it
	if _, err := a.Get("leaked"); err != ErrNotFound {
		t.Fatalf("a deleted secret came back: %v", err)
	}
	mergeFrom(t, b, a) // and the delete travels
	if got, _ := b.Entries("p"); len(got) != 0 {
		t.Fatalf("the delete must reach b, got %+v", got)
	}
	if env, _ := b.Env("p"); len(env) != 0 {
		t.Fatalf("Env must not surface a tombstone: %v", env)
	}
}

func TestReAddedSecretLivesAfterADelete(t *testing.T) {
	a, b := twoHomes(t)
	a.Put("s1", Entry{"KEY", "v1", "p"})
	mergeFrom(t, b, a)
	must(t, a.Delete("s1"))
	// The same id written again with a newer stamp.
	b.Put("s1", Entry{"KEY", "v2", "p"})
	setStamp(t, b, "s1", 1<<50)
	mergeFrom(t, a, b)
	if e, err := a.Get("s1"); err != nil || e.Value != "v2" {
		t.Fatalf("a newer re-add must live: %+v %v", e, err)
	}
	// The same name under a fresh id, through the dotenv import, also lives.
	must(t, a.Delete("s1"))
	path := filepath.Join(t.TempDir(), ".env")
	must(t, os.WriteFile(path, []byte("KEY=v3\n"), 0o600))
	if _, err := a.ImportDotenv(path, "p"); err != nil {
		t.Fatal(err)
	}
	if env, _ := a.Env("p"); len(env) != 1 || env[0] != "KEY=v3" {
		t.Fatalf("got %v", env)
	}
}

func TestTombstoneCarriesNoSecret(t *testing.T) {
	a, _ := twoHomes(t)
	a.Put("s1", Entry{"KEY", "topsecret", "p"})
	must(t, a.Delete("s1"))
	d, _ := a.read()
	if r := d.Secrets["s1"]; !r.Deleted || r.Entry != (Entry{}) || r.Updated == 0 {
		t.Fatalf("got %+v", r)
	}
	must(t, a.Delete("missing")) // leaves nothing behind
	if d, _ := a.read(); len(d.Secrets) != 1 {
		t.Fatal("deleting an unknown id must not write a tombstone")
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

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func statMod(path string) (int64, error) {
	i, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return i.ModTime().UnixNano(), nil
}

// PutAt stamps a slot with the time of the edit, so an old edit recorded late
// loses to a newer edit from another machine, and Deleted tells a tombstone from
// a secret that never existed.
func TestPutAtStampsWithTheEditTimeAndDeletedSeesTombstones(t *testing.T) {
	a, b := twoHomes(t)
	now := time.Now()
	put := func(v *Vault, value string, at time.Time) {
		if err := v.PutAt("s", Entry{Name: "N", Value: value, Scope: "p"}, at); err != nil {
			t.Fatal(err)
		}
	}
	put(b, "newer", now.Add(-time.Minute))
	put(a, "older", now.Add(-time.Hour))
	enc, err := b.Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(enc); err != nil {
		t.Fatal(err)
	}
	if e, _ := a.Get("s"); e.Value != "newer" {
		t.Fatalf("the older edit recorded later won: %q", e.Value)
	}
	if gone, _ := a.Deleted("s"); gone {
		t.Fatal("a live secret reads as deleted")
	}
	_ = a.Delete("s")
	if gone, _ := a.Deleted("s"); !gone {
		t.Fatal("a tombstone is not seen")
	}
	if gone, _ := a.Deleted("never"); gone {
		t.Fatal("an unknown id reads as deleted")
	}
}
