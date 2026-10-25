package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestAppRestDiskBoundsEntryCountOldestFirst(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	for i := range 257 {
		appRestWrite(fmt.Sprintf("agent-%03d", i), "stamp", []appwire.Task{{ID: "1"}})
	}
	files, err := os.ReadDir(filepath.Dir(appRestPath("agent-000")))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) > 256 {
		t.Errorf("disk cache kept %d entries, want at most 256", len(files))
	}
	if _, err := os.Stat(appRestPath("agent-000")); !os.IsNotExist(err) {
		t.Error("oldest entry was not evicted")
	}
	if _, ok := appRestRead("agent-256", "stamp"); !ok {
		t.Error("newest entry was evicted")
	}
}

func restDiskFixture(t *testing.T, agent string, size int64, at time.Time) {
	t.Helper()
	dir := filepath.Join(session.PlacesRoot(), "project", agent)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := appRestPath(agent)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestAppRestDiskBoundsAggregateBytesOldestFirst(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	old := time.Unix(1700000000, 0)
	for i, agent := range []string{"oldest", "middle", "newest"} {
		// Sparse legacy files exercise the aggregate bound without large allocations.
		restDiskFixture(t, agent, 24<<20, old.Add(time.Duration(i)*time.Second))
	}
	restDiskFixture(t, "overflow", 24<<20, old.Add(3*time.Second))
	appRestWrite("current", "stamp", nil)
	files, err := os.ReadDir(filepath.Dir(appRestPath("current")))
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	if total > 96<<20 {
		t.Errorf("disk cache retained %d bytes, want at most 96 MiB", total)
	}
	if _, err := os.Stat(appRestPath("oldest")); !os.IsNotExist(err) {
		t.Error("oldest file was not evicted for byte bound")
	}
	if _, err := os.Stat(appRestPath("newest")); err != nil {
		t.Error("newer file was evicted before oldest")
	}
}

func TestAppRestDiskSweepPrunesDeletedAgents(t *testing.T) {
	b, _ := restBackend(t, &restConversation{})
	now := time.Now().Add(time.Hour)
	b.now = func() time.Time { return now }
	for _, agent := range []string{"live", "gone", "deleted"} {
		restDiskFixture(t, agent, 1, now)
	}
	gone := filepath.Join(session.PlacesRoot(), "project", "gone")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(session.PlacesRoot(), "project", "deleted", teams.ConversationDeletedFile)
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	b.sweepOnce()
	for _, agent := range []string{"gone", "deleted"} {
		if _, err := os.Stat(appRestPath(agent)); !os.IsNotExist(err) {
			t.Errorf("deleted agent %s kept disk cache", agent)
		}
	}
	if _, err := os.Stat(appRestPath("live")); err != nil {
		t.Fatal("live agent cache was pruned")
	}
	// New external entries are checked on a later sweep, without another request.
	restDiskFixture(t, "later", 1, now)
	if err := os.Remove(filepath.Join(session.PlacesRoot(), "project", "later")); err != nil {
		t.Fatal(err)
	}
	// An ordinary write and a sweep inside the interval must reuse the inventory.
	appRestWrite("live", "stamp", nil)
	b.sweepOnce()
	if _, err := os.Stat(appRestPath("later")); err != nil {
		t.Fatal("ordinary request rescanned the cache directory")
	}
	now = now.Add(time.Minute)
	b.sweepOnce()
	if _, err := os.Stat(appRestPath("later")); !os.IsNotExist(err) {
		t.Fatal("periodic sweep did not prune external entry")
	}
}
