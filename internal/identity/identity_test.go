package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/lawcheck"
)

var _ = lawcheck.UpdateFlag()

func init() { cost = wrapped{Time: 1, MemKiB: 64, Threads: 1} } // fast tests

func sample() Identity {
	var id Identity
	for i := range id.seed {
		id.seed[i], id.dedup[i], id.cell[i] = byte(i), byte(0x40+i), byte(0x80+i)
	}
	return id
}

func TestGoldenIdentityFile(t *testing.T) {
	lawcheck.Golden(t, filepath.Join("testdata", "identity.golden.json"), sample().document())
}

func TestGoldenExport(t *testing.T) {
	lawcheck.Golden(t, filepath.Join("testdata", "export.golden.json"), wrapped{V: 1, KDF: kdfName, Time: 3, MemKiB: 65536,
		Threads: 4, Salt: "00112233445566778899aabbccddeeff", Nonce: "00112233445566778899aabbccddeeff0011223344556677", Data: "cafe"})
}

func TestEnsureIsStableAndPrivate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	a, err := Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Ensure(home)
	if err != nil || a.ID() != b.ID() || a.CellKeyID() != b.CellKeyID() {
		t.Fatalf("second Ensure differs: %v", err)
	}
	if info, err := os.Stat(filepath.Join(home, File)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("identity file: %v %v", info, err)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 2 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestIdentityShape(t *testing.T) {
	id := sample()
	if len(id.ID()) != 35 || len(id.Fingerprint()) != 19 || len(id.CellKeyID()) != 32 {
		t.Fatalf("shapes: %s %s %s", id.ID(), id.Fingerprint(), id.CellKeyID())
	}
	msg := []byte("hello")
	if !ed25519.Verify(id.PublicKey(), msg, id.Sign(msg)) {
		t.Fatal("signature does not verify")
	}
}

func TestExportImportOnSecondHome(t *testing.T) {
	first, _ := Ensure(t.TempDir())
	blob, err := Export(first, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	second := t.TempDir()
	got, err := Import(blob, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Adopt(second, got, false); err != nil {
		t.Fatal(err)
	}
	loaded, _ := Load(second)
	if loaded.ID() != first.ID() || loaded.CellKeyID() != first.CellKeyID() || string(loaded.DedupSecret()) != string(first.DedupSecret()) {
		t.Fatal("second machine does not hold the same identity")
	}
}

func TestWrongPassphraseAndDamageRefused(t *testing.T) {
	blob, _ := Export(sample(), "right")
	if _, err := Import(blob, "wrong"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if _, err := Import([]byte(`{"V":1}`), "right"); err == nil {
		t.Fatal("accepted a non-export")
	}
	if _, err := Import([]byte(`{"V":1,"kdf":"argon2id","t":1,"m_kib":4294967295,"p":1}`), "x"); err == nil {
		t.Fatal("accepted absurd key stretching")
	}
}

func TestAdoptRefusesADifferentIdentityWithoutReplace(t *testing.T) {
	home := t.TempDir()
	mine, _ := Ensure(home)
	if _, err := Adopt(home, sample(), false); !errors.Is(err, ErrDifferent) {
		t.Fatalf("overwrote a different identity: %v", err)
	}
	if again, _ := Load(home); again.ID() != mine.ID() {
		t.Fatal("identity changed despite refusal")
	}
	if replaced, err := Adopt(home, mine, false); err != nil || replaced {
		t.Fatalf("re-adopting the same identity: %v %v", replaced, err)
	}
	if replaced, err := Adopt(home, sample(), true); err != nil || !replaced {
		t.Fatalf("replace: %v %v", replaced, err)
	}
}

func TestGoldenDeviceCert(t *testing.T) {
	lawcheck.Golden(t, filepath.Join("testdata", "device.golden.json"), Cert{V: 1,
		Device: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		Made:   1759049990000, Sig: "cafe"})
}

func TestTwoMachinesShareIdentityNotDevice(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	first, _ := Ensure(a)
	blob, _ := Export(first, "pw")
	root, _ := Import(blob, "pw")
	if _, err := Adopt(b, root, false); err != nil {
		t.Fatal(err)
	}
	da, err1 := Device(a)
	db, err2 := Device(b)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if got, _ := Load(b); got.ID() != first.ID() {
		t.Fatal("identity ids differ")
	}
	if da.ID() == db.ID() || len(da.ID()) != 36 {
		t.Fatalf("device ids: %s %s", da.ID(), db.ID())
	}
	if again, _ := Device(a); again.ID() != da.ID() {
		t.Fatal("device is not stable")
	}
	if err := db.Cert.Verify(first.PublicKey()); err != nil {
		t.Fatalf("B's cert under the shared root: %v", err)
	}
	msg := []byte("lease")
	pub, _ := hex.DecodeString(db.Cert.Device)
	if !ed25519.Verify(pub, msg, db.Sign(msg)) {
		t.Fatal("device signature does not verify")
	}
	if blobHasDevice := strings.Contains(string(blob), "device"); blobHasDevice {
		t.Fatal("export carries device material")
	}
}

func TestCertVerifiesOnlyUnderItsIdentityAndUntampered(t *testing.T) {
	home := t.TempDir()
	d, _ := Device(home)
	root, _ := Load(home)
	if err := d.Cert.Verify(root.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := d.Cert.Verify(sample().PublicKey()); err == nil {
		t.Fatal("cert verified under a different identity")
	}
	tampered := d.Cert
	tampered.Made++
	if err := tampered.Verify(root.PublicKey()); err == nil {
		t.Fatal("tampered cert verified")
	}
}

func TestReplacingTheIdentityRenewsTheDevice(t *testing.T) {
	home := t.TempDir()
	old, _ := Device(home)
	if _, err := Adopt(home, sample(), true); err != nil {
		t.Fatal(err)
	}
	now, _ := Device(home)
	if now.ID() == old.ID() || now.Cert.Verify(sample().PublicKey()) != nil {
		t.Fatal("device did not follow the new identity")
	}
}

// A home holding only an unmigrated vault.key: importing another identity is
// refused, and the key survives as the identity's cell key.
func TestAdoptRefusesOverAnUnmigratedVaultKey(t *testing.T) {
	home := t.TempDir()
	k := sample().cell
	os.WriteFile(filepath.Join(home, "vault.key"), []byte(hex.EncodeToString(k[:])), 0o600)
	if _, err := Adopt(home, sample(), false); !errors.Is(err, ErrDifferent) {
		t.Fatalf("want ErrDifferent, got %v", err)
	}
	if kept, _ := Load(home); kept.CellKeyID() != sample().CellKeyID() {
		t.Fatal("vault key was not adopted as the cell key")
	}
}
