package main

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewResolvedTemporaryLaunchKeepsItsCitation(t *testing.T) {
	// A temp root whose physical path is outside /tmp emulates /var ->
	// /private/var on macOS; a target still under /tmp would hide this bug.
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	target, err := os.MkdirTemp(cache, "codeaf-temp-upgrade-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(target)
	alias := filepath.Join(t.TempDir(), "temps")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	launch := filepath.Join(alias, "launch")
	if err := os.Mkdir(launch, 0700); err != nil {
		t.Fatal(err)
	}
	project, owned := v3Workspace(launch, "")
	if !owned {
		t.Fatal("fixture must open an owned conversation")
	}
	stamp := v3StampLaunchDir(launch, project)
	t.Logf("raw launch=%s, project=%s, stamp=%q", launch, project, stamp)
	if stamp != launch {
		t.Fatal("normalizing the temporary workspace discarded its launch citation")
	}
	// A session whose stored launch is the workspace spelling must remain
	// recognizable to the sweeper after that spelling becomes canonical.
	root := t.TempDir()
	dir := filepath.Join(root, "old-bucket", "00000000000000f2")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := session.SaveMeta(dir, session.Meta{ID: "00000000000000f2", Owned: true, Workspace: filepath.Join(dir, "work"), LaunchDir: project, LastUserAt: now.Add(-30 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile((session.Place{Dir: dir}).Transcript(), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	session.SweepPlaces(root, now, nil)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("resolved temp citation no longer marks litter: %v", err)
	}

}
