package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A PROJECT IS NAMED BY THE FOLDER, NOT A SPELLING OF IT. The surface-side
// regression for issue #1761: the bucket key a launch reads and writes sessions
// under used to carry whatever spelling the terminal walked in with, so one
// folder reached through a link was a second project — a second history —
// beside the same folder named by its resolved path.
func TestAProjectIsNamedByTheFolderNotTheSpelling(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "linkedrepo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// A non-git directory reached through the link keys the same bucket as the
	// folder it reaches.
	aliasProject, _ := v3Workspace(link, "")
	if aliasProject != resolved {
		t.Fatalf("the alias spelling became the project: got %q, want %q", aliasProject, resolved)
	}
	bucketFromAlias, err := v3ProjectDir(aliasProject)
	if err != nil {
		t.Fatalf("v3ProjectDir(alias): %v", err)
	}
	bucketFromTarget, err := v3ProjectDir(resolved)
	if err != nil {
		t.Fatalf("v3ProjectDir(target): %v", err)
	}
	if bucketFromAlias != bucketFromTarget {
		t.Fatalf("one folder was given two buckets: alias=%q target=%q", bucketFromAlias, bucketFromTarget)
	}

	// Naming the folder explicitly keys one project either way.
	namedAlias, borrowed := v3Workspace("", link)
	if namedAlias != resolved || borrowed {
		t.Fatalf("an explicit workspace keys %q (borrowed=%v), want %q borrowed", namedAlias, borrowed, resolved)
	}

	// And a path that does not resolve keeps its spelling.
	imagined := filepath.Join(t.TempDir(), "nowhere-at-all")
	kept, _ := v3Workspace(imagined, "")
	if kept != imagined {
		t.Fatalf("a path that is not on disk was rewritten: got %q, want %q", kept, imagined)
	}
}
