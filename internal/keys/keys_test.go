package keys

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	for _, name := range []string{"vault.enc", "vault.key"} {
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
	if len(entries) != 2 {
		t.Fatalf("want vault.enc + vault.key only, got %v", entries)
	}
}

func TestTamperAndWrongKeyRejected(t *testing.T) {
	v, home := mustOpen(t)
	v.Put("id", Entry{Name: "K", Value: "v", Scope: "p"})
	os.Remove(filepath.Join(home, "vault.key"))
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
