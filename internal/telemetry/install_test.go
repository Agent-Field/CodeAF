package telemetry

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// TestInstallIDConcurrent pins the create race: sixteen callers starting
// together against a fresh directory must all answer the one id the first
// creator stored. The old read-then-write let every process mint its own id
// and silently use one that was never the stored id — a phantom install.
// It only means something under -race, where the interleavings are shaken out.
func TestInstallIDConcurrent(t *testing.T) {
	testHome(t)
	const callers = 16
	hashes := make([]string, callers)
	var group sync.WaitGroup
	group.Add(callers)
	for slot := 0; slot < callers; slot++ {
		go func(slot int) {
			defer group.Done()
			hashes[slot] = InstallIDHash()
		}(slot)
	}
	group.Wait()
	distinct := map[string]bool{}
	for _, hash := range hashes {
		distinct[hash] = true
	}
	if len(distinct) != 1 {
		t.Fatalf("%d concurrent callers produced %d distinct ids, want 1: %v", callers, len(distinct), hashes)
	}
	body, err := os.ReadFile(telemetryFile("install_id"))
	if err != nil {
		t.Fatalf("reading the stored id: %v", err)
	}
	if got := hashHex(strings.TrimSpace(string(body))); got != hashes[0] {
		t.Errorf("the stored id hashes to %q but every caller answered %q", got, hashes[0])
	}
	if info, err := os.Stat(telemetryFile("install_id")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("install_id mode: got %v, err %v, want 0600", info, err)
	}
}

// TestWriteInstallMethodPreservesExisting pins the no-op: install.json is the
// installer's file, carrying channel and installed_at beside the method, and
// a later run must never flatten it back to a method-only record.
func TestWriteInstallMethodPreservesExisting(t *testing.T) {
	testHome(t)
	installer := `{"install_method":"script","channel":"stable","installed_at":"2026-01-14T09:30:00Z"}`
	path := telemetryFile("install.json")
	if err := os.MkdirAll(telemetryDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(installer), 0o600); err != nil {
		t.Fatal(err)
	}
	WriteInstallMethod("source")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != installer {
		t.Errorf("WriteInstallMethod rewrote the installer's install.json:\n got %s\nwant %s", string(body), installer)
	}
	if got := installMethod(); got != "script" {
		t.Errorf("installMethod() = %q after the preserved record, want script", got)
	}
	// A file that does not parse is not the installer's record; it is replaced.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	WriteInstallMethod("source")
	if got := installMethod(); got != "source" {
		t.Errorf("after replacing a corrupt record: installMethod() = %q, want source", got)
	}
}
