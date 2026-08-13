package home

import (
	"path/filepath"
	"testing"
)

func TestDirDefaultsUnderTheUserHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/pretend-home")
	t.Setenv(EnvVar, "")
	if got, want := Dir(), filepath.Join("/tmp/pretend-home", ".aforge"); got != want {
		t.Fatalf("state root: got %q, want %q", got, want)
	}
}

func TestDirFollowsTheOverride(t *testing.T) {
	t.Setenv("HOME", "/tmp/pretend-home")
	t.Setenv(EnvVar, "/tmp/disposable/state")
	if got, want := Dir(), "/tmp/disposable/state"; got != want {
		t.Fatalf("state root: got %q, want %q", got, want)
	}
	if got, want := Join("graph.db"), "/tmp/disposable/state/graph.db"; got != want {
		t.Fatalf("joined: got %q, want %q", got, want)
	}
}

// The probe ran four conversations from four databases in /tmp, and all four
// wrote into one /tmp/workspace: every run's deliverables landed among the
// others' with nothing on disk saying which brain produced which file. Two
// stores in one directory get two workspaces, always.
func TestTwoStoresInOneDirectoryNeverShareAWorkspace(t *testing.T) {
	first, second := StoreDir("/tmp/probe-1.db", "workspace"), StoreDir("/tmp/probe-2.db", "workspace")
	if first == second {
		t.Fatalf("two stores in one directory share %q", first)
	}
	for _, want := range []string{"/tmp/probe-1-workspace", "/tmp/probe-2-workspace"} {
		if first != want && second != want {
			t.Fatalf("workspaces are not named for their stores: %q and %q", first, second)
		}
	}
	// It sits beside the store, so the kinds of a single store stay together and
	// a store's own directory is still one place a person can look.
	if got, want := StoreDir("/tmp/probe-1.db", "scratch"), "/tmp/probe-1-scratch"; got != want {
		t.Fatalf("scratch: got %q, want %q", got, want)
	}
	if got, want := StoreDir(Join("graph.db"), "workspace"), Join("graph-workspace"); got != want {
		t.Fatalf("the default store: got %q, want %q", got, want)
	}
}

// A blank override is not an override: an exported variable set to the empty
// string is how a shell says "unset" more often than it says "use the root".
func TestBlankOverrideIsIgnored(t *testing.T) {
	t.Setenv("HOME", "/tmp/pretend-home")
	t.Setenv(EnvVar, "   ")
	if got, want := Dir(), filepath.Join("/tmp/pretend-home", ".aforge"); got != want {
		t.Fatalf("state root: got %q, want %q", got, want)
	}
}
