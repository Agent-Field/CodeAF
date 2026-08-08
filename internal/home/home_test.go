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

// A blank override is not an override: an exported variable set to the empty
// string is how a shell says "unset" more often than it says "use the root".
func TestBlankOverrideIsIgnored(t *testing.T) {
	t.Setenv("HOME", "/tmp/pretend-home")
	t.Setenv(EnvVar, "   ")
	if got, want := Dir(), filepath.Join("/tmp/pretend-home", ".aforge"); got != want {
		t.Fatalf("state root: got %q, want %q", got, want)
	}
}
