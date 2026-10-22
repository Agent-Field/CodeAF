package enginehost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

func TestReviewAttachDoesNotSpawnBesideStartingLegacyHost(t *testing.T) {
	shortHome(t)
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	dir := home.Join("v3", "hosts", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// The workspace record is already present when a legacy host restarts in
	// an old directory. Its life lock is taken, but Listen has not run yet.
	if err := os.WriteFile(filepath.Join(dir, placeName), []byte(alias), 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := takeLock(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLock(lock)
	spawned := false
	conn, err := Attach(alias, func() error {
		spawned = true
		return errors.New("review fixture: canonical process would now start")
	})
	if conn != nil {
		conn.Close()
	}
	t.Logf("Attach returned %v; spawned=%v while legacy lock held", err, spawned)
	if spawned {
		t.Fatal("upgrade spawned a new host while a legacy engine still held its lifetime lock")
	}
}

// Startup probing must see the same no-listener legacy lock as Attach and Run.
func TestReviewStartingProbeSeesLegacyLifetimeLock(t *testing.T) {
	shortHome(t)
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	dir := home.Join("v3", "hosts", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, placeName), []byte(alias), 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := takeLock(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLock(lock)
	if !hostLockHeld(alias) {
		t.Fatal("startup probe missed the held legacy lifetime lock")
	}
	if conn, err := DialStartingHost(alias); err == nil {
		conn.Close()
		t.Fatal("a held lock with no listener answered")
	}
}

// The new host holds old keys throughout its lifetime, closing the race in
// which a successful legacy preflight was followed by acquisition elsewhere.
func TestReviewCanonicalHostHoldsProvenLegacyLock(t *testing.T) {
	shortHome(t)
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(alias)))
	dir := home.Join("v3", "hosts", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, placeName), []byte(alias), 0600); err != nil {
		t.Fatal(err)
	}
	liveHost(t, target)
	lock, err := takeLock(filepath.Join(dir, lockName))
	if err == nil {
		releaseLock(lock)
		t.Fatal("a running canonical host left a proven old lifetime lock free")
	}
}
