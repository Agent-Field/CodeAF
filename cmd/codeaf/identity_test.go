package main

import (
	"encoding/hex"
	"os"

	"bytes"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"path/filepath"
	"strings"
	"testing"
)

func door(t *testing.T, home, phrase string) (identityDoor, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return identityDoor{home: home, out: out, passphrase: func(bool) (string, error) { return phrase, nil }}, out
}

func TestIdentityExportImportAcrossHomes(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	blob := filepath.Join(t.TempDir(), "id.blob")
	da, showA := door(t, a, "pw")
	if err := runIdentityAt([]string{"export", blob}, da); err != nil {
		t.Fatal(err)
	}
	db, showB := door(t, b, "pw")
	if err := runIdentityAt([]string{"import", blob}, db); err != nil {
		t.Fatal(err)
	}
	showA.Reset()
	showB.Reset()
	runIdentityAt([]string{"show"}, da)
	runIdentityAt([]string{"show"}, db)
	if showA.String() == "" || showHead(showA.String()) != showHead(showB.String()) || showA.String() == showB.String() {
		t.Fatalf("shows differ:\n%s\n%s", showA, showB)
	}
}

func TestIdentityImportRefusesOtherIdentityWithoutReplace(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	blob := filepath.Join(t.TempDir(), "id.blob")
	da, _ := door(t, a, "pw")
	runIdentityAt([]string{"export", blob}, da)
	db, _ := door(t, b, "pw")
	runIdentityAt([]string{"show"}, db) // b now has its own identity
	if err := runIdentityAt([]string{"import", blob}, db); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("want a --replace refusal, got %v", err)
	}
	if err := runIdentityAt([]string{"import", "--replace", blob}, db); err != nil {
		t.Fatal(err)
	}
	dw, _ := door(t, b, "wrong")
	if err := runIdentityAt([]string{"import", "--replace", blob}, dw); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
}

func TestIdentityNeedsCells(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "0")
	if err := runIdentity([]string{"show"}); err == nil || !strings.Contains(err.Error(), "CODEAF_CELLS") {
		t.Fatalf("got %v", err)
	}
}

func TestIdentityUsage(t *testing.T) {
	d, _ := door(t, t.TempDir(), "pw")
	for _, args := range [][]string{{}, {"nope"}, {"show", "x"}, {"import"}, {"export", "a", "b"}} {
		if err := runIdentityAt(args, d); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

// showHead is the three identity lines of `show`; the device line differs per machine.
func showHead(shown string) string { return strings.Join(strings.Split(shown, "\n")[:3], "\n") }

// A home whose vault predates identities (vault.key, no identity.json) keeps it
// through a plain import: refused, and the vault still opens.
func TestIdentityImportKeepsAnUnmigratedVault(t *testing.T) {
	old, other := t.TempDir(), t.TempDir()
	v, err := keys.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	v.Put("a", keys.Entry{Name: "K", Value: "kept", Scope: "p"})
	id, _ := identity.Load(old)
	os.Remove(filepath.Join(old, identity.File))
	os.WriteFile(filepath.Join(old, "vault.key"), []byte(hex.EncodeToString(id.CellKey())), 0o600)

	blob := filepath.Join(t.TempDir(), "id.blob")
	do, _ := door(t, other, "pw")
	runIdentityAt([]string{"export", blob}, do)
	di, _ := door(t, old, "pw")
	if err := runIdentityAt([]string{"import", blob}, di); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("want a --replace refusal, got %v", err)
	}
	again, err := keys.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := again.Get("a"); err != nil || e.Value != "kept" {
		t.Fatalf("vault lost: %v %v", e, err)
	}
}
