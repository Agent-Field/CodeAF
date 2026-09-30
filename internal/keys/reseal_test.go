package keys

import (
	"errors"
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

// A reseal that stopped after the identity changed and before the vault
// followed is finished by the vault's next read.
func TestVaultReadFinishesAStagedReseal(t *testing.T) {
	_, home := mustOpen(t)
	_, newKey, _ := reseal(t, home)
	// The identity changed under the vault: a vault for newKey, staged, and a live one for the old key.
	v := &Vault{path: filepath.Join(home, "vault.enc"), key: newKey}
	e, err := v.Get("p/K")
	if err != nil || e.Value != "s3cret" {
		t.Fatalf("secret = %+v, %v", e, err)
	}
	if _, err := os.Stat(filepath.Join(home, nextFile)); err == nil {
		t.Fatal("the staged file is still there after it was adopted")
	}
}

// A vault that is another identity's and was never staged for this one is
// still refused, as before.
func TestVaultOfAnotherKeyIsStillRefused(t *testing.T) {
	_, home := mustOpen(t)
	v := &Vault{path: filepath.Join(home, "vault.enc"), key: make([]byte, 32)}
	if _, err := v.Get("x"); err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	real, _ := Open(home)
	_ = real.Put("a", Entry{Name: "A", Value: "v", Scope: "s"})
	if _, err := v.Get("a"); !errors.Is(err, ErrOtherKey) {
		t.Fatalf("read under the wrong key = %v, want ErrOtherKey", err)
	}
}
