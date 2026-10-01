package handoff

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The tests of the hard-link seed run on every platform, so they pin the tier
// they test: a platform that clones would otherwise skip it.
func init() { cloneTree = refuseClone }

func refuseClone(string, string) error { return syscall.EXDEV }

// writeTree makes the files of a small tree and answers its root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func emptyStage(t *testing.T) string {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	return stage
}

// copyTree stands in for a filesystem clone: it makes dst, which must not
// exist, hold a copy of src, and it counts its calls.
func copyTree(calls *int) func(src, dst string) error {
	return func(src, dst string) error {
		*calls++
		return os.CopyFS(dst, os.DirFS(src))
	}
}

// TestSeedClonesTheWholeTreeInOneCallAndDropsTheIdentity: where the filesystem
// clones, one call seeds the staging folder, no per-file link is made, and the
// engine's identity folder, which a clone carries along, is not left in it.
func TestSeedClonesTheWholeTreeInOneCallAndDropsTheIdentity(t *testing.T) {
	src := writeTree(t, map[string]string{"a.txt": "a", "d/b.txt": "b", identityDir + "/workspace": "id"})
	dst, calls := emptyStage(t), 0
	defer func(old func(string, string) error) { cloneTree = old }(cloneTree)
	cloneTree = copyTree(&calls)
	defer func(old func(string, string) error) { linkFile = old }(linkFile)
	linkFile = func(string, string) error { t.Fatal("a file was linked after the clone worked"); return nil }

	seedFrom(src, dst)

	if calls != 1 {
		t.Fatalf("clone called %d times, want 1", calls)
	}
	if got := tree(t, dst); got["a.txt"] != "a" || got["d/b.txt"] != "b" {
		t.Fatalf("staging = %v, want the held tree", got)
	}
	if exists(filepath.Join(dst, identityDir)) {
		t.Fatal("the engine's identity folder was cloned into the staging folder")
	}
}

// TestSeedFallsBackToLinksWhenTheCloneIsRefused: a clone across volumes fails,
// the staging folder is again the empty folder it was, and the hard-link seed
// fills it, so the take is whole on a filesystem that cannot clone.
func TestSeedFallsBackToLinksWhenTheCloneIsRefused(t *testing.T) {
	src := writeTree(t, map[string]string{"a.txt": "a", "d/b.txt": "b"})
	dst := emptyStage(t)
	defer func(old func(string, string) error) { cloneTree = old }(cloneTree)
	cloneTree = func(_, to string) error {
		_ = os.Mkdir(to, 0o700) // a refusal that left a half-made folder behind
		return &os.LinkError{Op: "clonefile", Err: syscall.EXDEV}
	}

	seedFrom(src, dst)

	if !os.SameFile(stat(t, filepath.Join(src, "d/b.txt")), stat(t, filepath.Join(dst, "d/b.txt"))) {
		t.Fatal("the fallback did not hard-link the files")
	}
	if got := stat(t, dst).Mode().Perm(); got != stat(t, src).Mode().Perm() {
		t.Fatalf("staging folder mode %v, want the held tree's", got)
	}
}

// TestSeedLeavesAnEmptyFolderWhenNothingCanSeedIt: a clone that is refused and
// a link that is refused leave the folder empty and usable, never absent.
func TestSeedLeavesAnEmptyFolderWhenNothingCanSeedIt(t *testing.T) {
	src := writeTree(t, map[string]string{"a.txt": "a"})
	dst := emptyStage(t)
	defer func(old func(string, string) error) { linkFile = old }(linkFile)
	linkFile = func(string, string) error { return errors.New("refused") }

	seedFrom(src, dst)

	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging = %v, %v; want an empty folder", entries, err)
	}
}
