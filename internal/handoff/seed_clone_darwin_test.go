//go:build darwin

package handoff

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSeedCloneOnAPFSGivesFilesOfItsOwnThatKeepTheirTimes: the real clone
// makes the staging tree equal to the held one, with a file of its own for
// every file, so a write into the held tree does not reach the staging tree.
func TestSeedCloneOnAPFSGivesFilesOfItsOwnThatKeepTheirTimes(t *testing.T) {
	src := writeTree(t, map[string]string{"a.txt": "a", "d/b.txt": "b", identityDir + "/workspace": "id"})
	dst := emptyStage(t)
	defer func(old func(string, string) error) { cloneTree = old }(cloneTree)
	cloneTree = cloneTreeOnPlatform

	seedFrom(src, dst)

	if got := tree(t, dst); got["a.txt"] != "a" || got["d/b.txt"] != "b" {
		t.Fatalf("staging = %v, want the held tree", got)
	}
	if os.SameFile(stat(t, filepath.Join(src, "a.txt")), stat(t, filepath.Join(dst, "a.txt"))) {
		t.Fatal("a cloned file is the same file as its source")
	}
	if !stat(t, filepath.Join(dst, "d/b.txt")).ModTime().Equal(stat(t, filepath.Join(src, "d/b.txt")).ModTime()) {
		t.Fatal("a cloned file lost its time")
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, dst)["a.txt"]; got != "a" {
		t.Fatalf("a write into the held tree reached the staging tree: %q", got)
	}
	if exists(filepath.Join(dst, identityDir)) {
		t.Fatal("identity folder cloned")
	}
}
