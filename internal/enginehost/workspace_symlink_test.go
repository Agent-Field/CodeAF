package enginehost

import (
	"os"
	"path/filepath"
	"testing"
)

// A FOLDER AND A SYMLINK TO IT ARE ONE WORKSPACE. The host directory is named by
// a hash of the workspace path, so if the link spelling hashes differently, the
// same folder gets two engines and `engine --status` on the link misses the one
// that is running.
func TestASymlinkToAWorkspaceHoldsTheSameEngine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "l")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	direct, err := where(dir)
	if err != nil {
		t.Fatal(err)
	}
	through, err := where(link)
	if err != nil {
		t.Fatal(err)
	}
	if direct != through {
		t.Fatalf("a folder and a symlink to it hold two engines: %s and %s", direct, through)
	}
}
