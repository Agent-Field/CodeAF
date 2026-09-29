package vaultsync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The values below are obvious fakes; no test prints a credentials file.
const (
	credsOne = `{"mail":{"key":"FAKE-one"}}`
	credsTwo = `{"mail":{"key":"FAKE-two"}}`
	credsSix = `{"mail":{"key":"FAKE-six"}}`
)

func (m *machine) credsPath() string { return filepath.Join(m.home, CredentialsFile) }

// saveCreds writes the file as a person's save would, at a chosen time.
func (m *machine) saveCreds(t *testing.T, body string, at time.Time) {
	t.Helper()
	must(t, os.WriteFile(m.credsPath(), []byte(body), 0o600))
	must(t, os.Chtimes(m.credsPath(), at, at))
}

func (m *machine) creds(t *testing.T) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(m.credsPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	must(t, err)
	return string(raw), true
}

func (m *machine) wantCreds(t *testing.T, want string) {
	t.Helper()
	if got, ok := m.creds(t); !ok || got != want {
		t.Fatalf("credentials.json is present=%v, matches the wanted copy=%v", ok, got == want)
	}
}

func TestCredentialsNoFile(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	for _, m := range []*machine{a, b} {
		if _, ok := m.creds(t); ok {
			t.Fatal("a credentials.json appeared from nothing")
		}
	}
}

func TestCredentialsAddedOnAAppearsOnB(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	a.saveCreds(t, credsOne, time.Now())
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	b.wantCreds(t, credsOne)
	if info, err := os.Stat(b.credsPath()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the restored file must be private, stat %v mode %v", err, info)
	}
}

func TestCredentialsRemovedOnAIsRemovedOnB(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	a.saveCreds(t, credsOne, time.Now())
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	must(t, os.Remove(a.credsPath()))
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	if _, ok := b.creds(t); ok {
		t.Fatal("the removal did not reach B")
	}
}

// A key set on B after A's last look is not clobbered by A's older copy, even
// though A pushes last: each copy is stamped by when it was saved.
func TestCredentialsConcurrentEditsNewestSaveWins(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	now := time.Now()
	a.saveCreds(t, credsOne, now.Add(-time.Hour))
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	a.saveCreds(t, credsTwo, now.Add(-30*time.Minute))
	b.saveCreds(t, credsSix, now.Add(-10*time.Minute))
	must(t, b.Push(ctx))
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	a.wantCreds(t, credsSix)
	b.wantCreds(t, credsSix)
}

func TestCredentialsCorruptFileDoesNotDestroyTheOtherCopy(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	a.saveCreds(t, credsOne, time.Now().Add(-time.Hour))
	must(t, a.Push(ctx))
	must(t, b.Pull(ctx))
	b.saveCreds(t, `{"mail": damaged`, time.Now())
	must(t, b.Push(ctx))
	// A still holds the good copy and the vault still names it.
	c := r.machine()
	must(t, c.Pull(ctx))
	c.wantCreds(t, credsOne)
	// B's next pull heals B, and keeps what it found beside the file.
	must(t, b.Pull(ctx))
	b.wantCreds(t, credsOne)
	if raw, err := os.ReadFile(b.credsPath() + ".damaged"); err != nil || !strings.Contains(string(raw), "damaged") {
		t.Fatalf("the damaged file was not kept: %v", err)
	}
}

func TestCredentialsNeverReachAProjectEnv(t *testing.T) {
	r := newRig(t)
	a := r.machine()
	a.saveCreds(t, credsOne, time.Now())
	must(t, a.Push(ctx))
	c := newCell(t)
	if _, err := a.Apply(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Root, ".env")); err == nil {
		t.Fatal("credentials.json leaked into a workspace .env")
	}
}
