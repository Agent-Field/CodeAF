//go:build linux

package sessionhost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistryAtomicAndSurvivesRestart(t *testing.T) {
	cfg := testConfig(t)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	r, err := loadRegistry(cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	row := sessionRow{ID: chatA, Person: "alice", UID: cfg.UIDBase, Created: now, LastActive: now.Add(time.Minute)}
	if err := r.put(row); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.Data, "registry.json")
	first, err := os.Stat(path)
	if err != nil || first.Mode().Perm() != 0o600 {
		t.Fatalf("registry permissions: %v, %v", first, err)
	}
	row.LastActive = now.Add(2 * time.Minute)
	if err := r.put(row); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(path)
	if err != nil || os.SameFile(first, second) {
		t.Fatalf("registry was not atomically replaced: %v, %v", second, err)
	}
	restarted, err := loadRegistry(cfg, now)
	if err != nil || restarted.sessions[chatA] != row {
		t.Fatalf("registry did not retain the session: %v, %v", restarted, err)
	}
}

func TestUIDQuarantine24h(t *testing.T) {
	cfg := testConfig(t)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	r, err := loadRegistry(cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	row := sessionRow{ID: chatA, Person: "alice", UID: cfg.UIDBase}
	if err := r.put(row); err != nil {
		t.Fatal(err)
	}
	if err := r.remove(row.ID, now); err != nil {
		t.Fatal(err)
	}
	r, err = loadRegistry(cfg, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if uid := r.allocate(cfg, nil, now.Add(24*time.Hour-time.Nanosecond)); uid != cfg.UIDBase+1 {
		t.Fatalf("allocated %d before quarantine ended", uid)
	}
	if uid := r.allocate(cfg, nil, now.Add(24*time.Hour)); uid != cfg.UIDBase {
		t.Fatalf("allocated %d after quarantine ended", uid)
	}
	if _, exists := r.quarantine[cfg.UIDBase]; exists {
		t.Fatal("expired entry was not dropped")
	}
}

func TestUIDNeverReusedWhileProcessAlive(t *testing.T) {
	cfg := testConfig(t)
	cfg.UIDCount = 1
	now := time.Now()
	r, err := loadRegistry(cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	if uid := r.allocate(cfg, processTable{cfg.UIDBase: {1234}}, now); uid != 0 {
		t.Fatalf("allocated %d with a process still alive", uid)
	}
	if uid := r.allocate(cfg, nil, now); uid != cfg.UIDBase {
		t.Fatalf("allocated %d after the process left", uid)
	}
}
