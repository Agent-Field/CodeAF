package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// With no relay set the surface gets the notice desk and nothing else: every
// other seam stays nil, and a nil seam is an absent verb, never a broken one.
func TestWireSyncOffAddsOnlyTheDesk(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(syncsetup.URLVar, "")
	var o tui3.Options
	wireSync(&o)
	if o.Machines != nil || o.Takeover != nil || o.Branches.Discard != nil || o.Branches.Merge != nil {
		t.Fatalf("sync off wired %+v", o)
	}
	if o.Notices == nil {
		t.Fatal("the notice desk is missing")
	}
}

// With a relay and an identity the surface can list, continue and discard, and
// it offers no merge.
func TestWireSyncOnWiresTheTakeSide(t *testing.T) {
	h := t.TempDir()
	t.Setenv(home.EnvVar, h)
	t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")
	if _, err := identity.Ensure(h); err != nil {
		t.Fatal(err)
	}
	var o tui3.Options
	wireSync(&o)
	if o.Machines == nil || o.Takeover == nil || o.Branches.Discard == nil {
		t.Fatalf("sync on wired %+v", o)
	}
	if o.Branches.Merge != nil {
		t.Fatal("a merge is offered and there is none")
	}
}

// A chat this machine already has is taken back into the folder it is in; one
// it has never seen gets a folder in the bucket of chats with no project.
func TestTakeRootForFindsTheFolderAChatIsIn(t *testing.T) {
	h := t.TempDir()
	t.Setenv(home.EnvVar, h)
	t.Setenv("HOME", t.TempDir())
	bucket := filepath.Join(h, "v3", "projects", "-work-project")
	c, err := cell.CreateIn(bucket, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	if got := takeRootFor(c.ID); got != c.Root {
		t.Fatalf("takeRootFor(%s) = %s, want the folder it is in, %s", c.ID, got, c.Root)
	}
	const stranger = "01J0000000000000000000000Z"
	if got := takeRootFor(stranger); filepath.Base(got) != stranger || filepath.Dir(filepath.Dir(got)) != filepath.Join(h, "v3", "projects") {
		t.Fatalf("a new chat is kept at %s, want a bucket under v3/projects", got)
	}
}
