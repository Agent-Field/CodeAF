package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
)

// A desktop window's new chat in a workspace with no conversation yet used to
// leave TWO folders: the launch resolved "this workspace's latest", which
// minted one because there was none, and the new chat was minted beside it.
// The first stayed behind as an untitled meta.json with no transcript, and the
// History and Places lists read it as a conversation (reproduced 2026-10-09 by
// one POST /sessions against an empty home: two folders).
func TestANewChatHelloMintsExactlyOneFolder(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	if opts := engineLaunchOptions(remote.Hello{Workspace: workspace, New: true}, workspace, ""); !opts.Fresh || opts.Session != "" {
		t.Fatalf("a new-chat hello did not ask for a fresh conversation: %+v", opts)
	}
	if opts := engineLaunchOptions(remote.Hello{Workspace: workspace}, workspace, ""); opts.Fresh {
		t.Fatal("a plain hello asked for a fresh conversation")
	}
	found, err := v3FreshSession(workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	folders, _ := filepath.Glob(filepath.Join(found.Bucket, "*"))
	if len(folders) != 1 || folders[0] != found.Place.Dir {
		t.Fatalf("folders after one fresh launch: %v", folders)
	}
}

// And it touches nobody else's folder: an empty one another window has just
// minted, and the conversations people have spoken in, all stay.
func TestAFreshLaunchReapsNothing(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	workspace := t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	spoken := writeV3Session(t, bucket, "00000000000000b1", "hello", time.Now().Add(-time.Hour))
	empty := writeV3Session(t, bucket, "00000000000000b2", "", time.Time{})
	found, err := v3FreshSession(workspace, workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{spoken, empty, found.Place.Dir} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
	}
	if found.Place.Dir == spoken || found.Place.Dir == empty || found.Resumed {
		t.Fatalf("a fresh launch reused %s", found.Place.Dir)
	}
}
