package enginehost

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// legacyHostDir mints the host directory the way a build BEFORE the resolved
// workspace key (#1761) named it: sha256 of the spelling it was handed. An
// upgrade fixture needs the old name, because that is what the installed base
// left on disk.
func legacyHostDir(t *testing.T, workspace string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(filepath.Clean(workspace)))
	dir := home.Join("v3", "hosts", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("make a legacy host directory: %v", err)
	}
	return dir
}

// A host drops its listener before it finishes flushing journals. The old
// directory's lock must keep replacement waiting throughout that interval.
func TestLegacyRetirementWaitsForTheLegacyLock(t *testing.T) {
	shortHome(t)
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dir := legacyHostDir(t, link)
	if err := os.WriteFile(filepath.Join(dir, placeName), []byte(link), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, socketName))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := takeLock(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = releaseLock(lock) }()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{target, link} {
		if err := waitForGone(workspace, 0); err == nil {
			t.Errorf("retirement through %s completed while the legacy host still held its lock", workspace)
		}
	}
	if err := releaseLock(lock); err != nil {
		t.Fatal(err)
	}
	lock = nil
	for _, workspace := range []string{target, link} {
		if err := waitForGone(workspace, 0); err != nil {
			t.Errorf("retirement through %s did not finish after the legacy lock was released: %v", workspace, err)
		}
	}
}

// THE HOST AN OLDER BUILD WROTE FOR THIS FOLDER IS STILL THIS FOLDER'S HOST.
// The upgrade regression for the resolved workspace key: a host directory named
// by the raw spelling an older build was handed — a link, the way a terminal
// carries it — stays reachable by lookup, so Dial meets it, and with it Stop,
// the status door and the takeover rule keep reaching an engine the upgrade did
// not start. Before the legacy lookup, Held listed the engine and Dial missed
// its live socket, which is an upgrade that strands a running engine.
func TestADialMeetsAHostAnOlderBuildWroteForTheSameFolder(t *testing.T) {
	shortHome(t)
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// The old build was told the link's spelling and hashed that, so its host
	// directory carries the old key and cites the spelling it was given.
	dir := legacyHostDir(t, link)
	if err := os.WriteFile(filepath.Join(dir, placeName), []byte(link+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, socketName))
	if err != nil {
		t.Fatalf("stand a live socket in the legacy directory: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	// The new build asks by the FOLDER — the resolved spelling. Held already
	// answers for the legacy directory; Dial must now reach it too, and the
	// plain target spelling with it, since one folder is one host.
	if held, err := Held(); err != nil || len(held) != 1 || !strings.HasSuffix(held[0], "linkedrepo") {
		t.Fatalf("Held() = %v (%v), want the legacy directory's own citation", held, err)
	}
	conn, err := Dial(target)
	if err != nil {
		t.Fatalf("a dial by the resolved folder missed the host an older build wrote: %v", err)
	}
	_ = conn.Close()
	conn, err = Dial(link)
	if err != nil {
		t.Fatalf("a dial by the legacy spelling missed its own host: %v", err)
	}
	_ = conn.Close()

	// And a folder that never had a host is still answered with nothing.
	if _, err := Dial(filepath.Join(t.TempDir(), "nowhere")); err == nil {
		t.Fatal("a workspace with no host anywhere dialed to something")
	}
}
