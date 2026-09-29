package keys

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/Agent-Field/codeaf/internal/identity"
)

func mustOpen(t *testing.T) (*Vault, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	v, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	return v, home
}

func TestRoundTrip(t *testing.T) {
	v, home := mustOpen(t)
	want := Entry{Name: "DATABASE_URL", Value: "postgres://u:pw@h/db", Scope: "p1"}
	if err := v.Put("id1", want); err != nil {
		t.Fatal(err)
	}
	again, _ := Open(home)
	got, err := again.Get("id1")
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := v.Delete("id1"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("id1"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFilesArePrivateAndSealed(t *testing.T) {
	v, home := mustOpen(t)
	if err := v.Put("id1", Entry{Name: "K", Value: "hunter2-plain", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"vault.enc", "identity.json"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	if strings.Contains(string(raw), "hunter2-plain") {
		t.Fatal("value stored in plaintext")
	}
}

func TestAtomicWriteLeavesNoTemp(t *testing.T) {
	v, home := mustOpen(t)
	for i := 0; i < 3; i++ {
		if err := v.Put("id", Entry{Name: "K", Value: "v", Scope: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 3 {
		t.Fatalf("want vault.enc, identity.json and device.json only, got %v", entries)
	}
}

func TestTamperAndWrongKeyRejected(t *testing.T) {
	v, home := mustOpen(t)
	v.Put("id", Entry{Name: "K", Value: "v", Scope: "p"})
	os.Remove(filepath.Join(home, "identity.json"))
	other, _ := Open(home) // fresh key
	if _, err := other.Get("id"); err == nil {
		t.Fatal("opened vault under a different key")
	}
}

func TestEnvScopedAndSorted(t *testing.T) {
	v, _ := mustOpen(t)
	v.Put("a", Entry{Name: "B", Value: "2", Scope: "p"})
	v.Put("b", Entry{Name: "A", Value: "1", Scope: "p"})
	v.Put("c", Entry{Name: "C", Value: "3", Scope: "other"})
	got, _ := v.Env("p")
	if want := []string{"A=1", "B=2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestImportDotenv(t *testing.T) {
	v, _ := mustOpen(t)
	file := filepath.Join(t.TempDir(), ".env")
	body := "# comment\n\nA=1\nexport B=\"two words\"\nC='3'\n  D = 4 \nnoequals\n=novalue\n"
	os.WriteFile(file, []byte(body), 0o600)
	n, err := v.ImportDotenv(file, "p")
	if err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := v.Env("p")
	if want := []string{"A=1", "B=two words", "C=3", "D=4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	os.WriteFile(file, []byte("A=changed\n"), 0o600)
	v.ImportDotenv(file, "p")
	got, _ = v.Env("p")
	if got[0] != "A=changed" || len(got) != 4 {
		t.Fatalf("reimport should update in place: %v", got)
	}
}

func TestScanTree(t *testing.T) {
	cases := []struct {
		file, body, rule string // rule "" = clean
	}{
		{".env", "X=1", "dotenv-file"},
		{".env.local", "X=1", "dotenv-file"},
		{"sub/prod.env", "X=1", "dotenv-file"},
		{"aws-credentials.json", "{}", "credentials-file"},
		{"tls/server.pem", "x", "private-key-file"},
		{"id_rsa", "x", "private-key-file"},
		{"id_ed25519.pub", "x", "private-key-file"},
		{"a.txt", "key=sk-abcdefghijklmnop1234", "api-key-sk"},
		{"b.txt", "ghp_" + strings.Repeat("a", 36), "github-token"},
		{"c.txt", "xoxb-1234567890-abcdef", "slack-token"},
		{"d.txt", "AKIAABCDEFGHIJKLMNOP", "aws-access-key"},
		{"e.txt", "-----BEGIN RSA PRIVATE KEY-----", "private-key-block"},
		{"main.go", "package main // task-sk-1", ""},
		{"env.md", "about .env files", ""},
		{"f.txt", "AKIAshort", ""},
		{"environment.txt", "hello", ""},
	}
	root := t.TempDir()
	for _, c := range cases {
		p := filepath.Join(root, c.file)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(c.body), 0o644)
	}
	got := map[string]string{}
	for _, f := range ScanTree(root) {
		got[f.Path] = f.Rule
	}
	for _, c := range cases {
		if got[c.file] != c.rule {
			t.Errorf("%s: rule %q, want %q", c.file, got[c.file], c.rule)
		}
	}
}

// A vault sealed before the schema freeze names its key as 16 hex under
// "key_id". It still opens, and the next write names the key as 32 hex under
// "cell_key_id".
func TestLegacyEnvelopeReadsOnceThenMigrates(t *testing.T) {
	v, home := mustOpen(t)
	plain := []byte(`{"V":1,"secrets":{"id1":{"name":"K","value":"v","scope":"p"}},"updated":1}`)
	legacy := legacyID(v.key)
	blob := sealAs(t, v.key, plain, legacy)
	if err := os.WriteFile(filepath.Join(home, "vault.enc"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := v.Get("id1"); err != nil || got.Value != "v" {
		t.Fatalf("legacy read: %+v %v", got, err)
	}
	if err := v.Put("id2", Entry{Name: "K2", Value: "w", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if len(env.CellKeyID) != 32 || env.KeyID != "" {
		t.Fatalf("envelope not migrated: %s", raw)
	}
	if !strings.HasPrefix(env.CellKeyID, legacy) {
		t.Fatalf("new id %s does not extend legacy id %s", env.CellKeyID, legacy)
	}
}

func TestEnvelopeRefusesForeignKeyID(t *testing.T) {
	v, home := mustOpen(t)
	if err := v.Put("id1", Entry{Name: "K", Value: "v", Scope: "p"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "vault.enc"))
	var env envelope
	_ = json.Unmarshal(raw, &env)
	env.CellKeyID = strings.Repeat("ab", 16)
	bad, _ := json.Marshal(env)
	if _, err := open(v.key, bad); err == nil {
		t.Fatal("opened an envelope naming another key")
	}
}

func legacyID(key []byte) string { return identity.CellKeyID(key)[:16] }

// sealAs seals plain the way the pre-freeze build did, naming the key by id.
func sealAs(t *testing.T, key, plain []byte, id string) []byte {
	t.Helper()
	aead, _ := chacha20poly1305.New(key)
	nonce := make([]byte, aead.NonceSize())
	blob, err := json.Marshal(map[string]any{"V": 1, "key_id": id, "nonce": hex.EncodeToString(nonce),
		"data": hex.EncodeToString(aead.Seal(nil, nonce, plain, []byte(id)))})
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// A machine with a vault.key from before identities keeps its vault: the old
// key becomes the identity's cell key and the vault still opens.
func TestVaultKeyBecomesTheIdentityCellKey(t *testing.T) {
	home := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	os.WriteFile(filepath.Join(home, "vault.key"), []byte(hex.EncodeToString(key)), 0o600)
	blob, err := seal(key, []byte(`{"V":1,"secrets":{"a":{"name":"K","value":"old","scope":"p"}},"updated":1}`))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, "vault.enc"), blob, 0o600)

	v, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := v.Get("a"); err != nil || e.Value != "old" {
		t.Fatalf("old vault did not open: %v %v", e, err)
	}
	id, _ := identity.Load(home)
	if id.CellKeyID() != identity.CellKeyID(key) {
		t.Fatal("identity cell key is not the old vault key")
	}
}
