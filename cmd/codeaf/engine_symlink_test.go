package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// TestEngineWorkspaceResolvesSymlink is the regression for issue #1761: a
// folder reached through a symlink must key to the same workspace as the
// folder it points at, so one folder cannot become two projects and two
// engines. Before the fix engineWorkspace stopped at filepath.Clean, so the
// alias and the target returned different paths and enginehost.where hashed
// each spelling to its own host directory.
func TestEngineWorkspaceResolvesSymlink(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "realrepo")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tmp, "linkedrepo")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	fromTarget, err := engineWorkspace(real)
	if err != nil {
		t.Fatalf("engineWorkspace(target): %v", err)
	}
	fromLink, err := engineWorkspace(link)
	if err != nil {
		t.Fatalf("engineWorkspace(link): %v", err)
	}

	resolvedLink, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks(link): %v", err)
	}

	if fromLink != resolvedLink {
		t.Fatalf("link spelling not resolved: got %q, want %q", fromLink, resolvedLink)
	}
	if fromLink != fromTarget {
		t.Fatalf("two spellings keyed differently: link=%q target=%q", fromLink, fromTarget)
	}
}

// TestEngineWorkspaceKeepsNonSymlinkPathUnchanged guards that an ordinary
// folder still resolves to itself, so the fix only moves symlinked spellings.
func TestEngineWorkspaceKeepsNonSymlinkPathUnchanged(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "plainrepo")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks(plain): %v", err)
	}
	got, err := engineWorkspace(real)
	if err != nil {
		t.Fatalf("engineWorkspace(plain): %v", err)
	}
	if got != resolvedReal {
		t.Fatalf("plain path changed: got %q, want %q", got, resolvedReal)
	}
}

// TestADialThroughASymlinkMeetsTheHostAtTheFolder is the launch-side regression
// for the review of #1798: the local surface used to hash the spelling the
// person's terminal carried (os.Getwd keeps a symlinked alias), while the
// daemon resolved its --workspace before it listened — so the surface dialed
// one host directory and the daemon was born in another, and the launch fell
// back to a conversation in the terminal. Host lookup, dial and spawn now all
// funnel through the one resolved identity (enginehost.where), so a host
// standing at the folder answers a dial that arrives through any spelling of
// it.
func TestADialThroughASymlinkMeetsTheHostAtTheFolder(t *testing.T) {
	shortStateRoot(t)
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// A daemon was handed --workspace by a door that resolves (engineWorkspace),
	// so a host stands where the FOLDER hashes. Stand one there, speaking this
	// build's wire so the stale-host question before the dial is answered.
	standIn(t, target, remote.HostSelf{Version: remote.Version, Build: buildinfo.Identity()}, false)

	// The surface reached the folder through the link and dials with the
	// spelling it carries. The dial must still meet the host — and the spawn
	// callback must never fire, because something is already answering.
	linkDial := &localLink{workspace: link}
	conn, err := linkDial.dial()
	if err != nil {
		t.Fatalf("a dial through the symlink missed the host at the folder: %v", err)
	}
	_ = conn.Close()

	// And the target's own spelling reaches the same place, since one folder is
	// one host.
	targetDial := &localLink{workspace: target}
	conn, err = targetDial.dial()
	if err != nil {
		t.Fatalf("a dial from the target spelling missed its own host: %v", err)
	}
	_ = conn.Close()
}

// TestTheDefaultWorkspaceIsTheSameFolderAsTheNamedHome is the default-home
// half of the #1798 review: engineWorkspace("") used to return home without
// the resolution a TYPED home goes through, so a home behind a symlink keyed
// two hosts depending on whether --workspace was omitted or spelled out.
func TestTheDefaultWorkspaceIsTheSameFolderAsTheNamedHome(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "homelink")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// os.UserHomeDir reads HOME on unix and USERPROFILE on Windows; name both
	// so the default reaches the link on every machine this runs on.
	t.Setenv("HOME", link)
	t.Setenv("USERPROFILE", link)

	omitted, err := engineWorkspace("")
	if err != nil {
		t.Fatalf("engineWorkspace(\"\"): %v", err)
	}
	named, err := engineWorkspace(link)
	if err != nil {
		t.Fatalf("engineWorkspace(home): %v", err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks(home): %v", err)
	}
	if omitted != resolved {
		t.Fatalf("the omitted flag kept the raw home spelling: got %q, want %q", omitted, resolved)
	}
	if omitted != named {
		t.Fatalf("omitting --workspace and naming the same home keyed two workspaces: %q vs %q", omitted, named)
	}
}
