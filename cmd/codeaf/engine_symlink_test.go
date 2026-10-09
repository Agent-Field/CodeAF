package main

import (
	"os"
	"path/filepath"
	"testing"
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
