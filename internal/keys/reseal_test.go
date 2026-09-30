package keys

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
)

func reseal(t *testing.T, home string) (oldKey, newKey []byte, enc []byte) {
	t.Helper()
	v, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put("p/K", Entry{Name: "K", Value: "s3cret", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	old, _ := identity.Load(home)
	next, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	enc, err = StageReseal(home, old.CellKey(), next.CellKey())
	if err != nil {
		t.Fatal(err)
	}
	return old.CellKey(), next.CellKey(), enc
}

// Staging leaves the live vault alone and opens only under the new key.
func TestStageResealLeavesLiveVault(t *testing.T) {
	_, home := mustOpen(t)
	oldKey, newKey, enc := reseal(t, home)
	if _, err := open(newKey, enc); err != nil {
		t.Fatalf("staged vault does not open under the new key: %v", err)
	}
	if _, err := open(oldKey, enc); err == nil {
		t.Fatal("staged vault still opens under the old key")
	}
	live, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	if _, err := open(oldKey, live); err != nil {
		t.Fatalf("live vault changed: %v", err)
	}
}

// Committing puts the staged vault in place, and repeating either step is safe.
func TestCommitResealIsRepeatable(t *testing.T) {
	_, home := mustOpen(t)
	_, newKey, enc := reseal(t, home)
	for range 2 {
		if err := CommitReseal(home); err != nil {
			t.Fatal(err)
		}
	}
	live, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	if string(live) != string(enc) {
		t.Fatal("the live vault is not the staged one")
	}
	again, err := StageReseal(home, newKey, newKey)
	if err != nil || string(again) != string(enc) {
		t.Fatalf("staging a resealed vault changed it: %v", err)
	}
}

// A home with no vault has nothing to stage or commit.
func TestResealWithoutVault(t *testing.T) {
	home := t.TempDir()
	enc, err := StageReseal(home, make([]byte, 32), make([]byte, 32))
	if enc != nil || err != nil || CommitReseal(home) != nil {
		t.Fatalf("enc %v, err %v", enc, err)
	}
}
