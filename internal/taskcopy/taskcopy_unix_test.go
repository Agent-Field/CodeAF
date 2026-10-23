//go:build !windows

// K6 keeps Unix process, lock and mode assertions on the systems that provide them.
package taskcopy

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCheckedOutFilesComeBackAtTheirModesUnderAnotherUmask(t *testing.T) {
	old := syscall.Umask(0o002)
	defer syscall.Umask(old)
	a := newMachine(t)
	tree := a.task("umask")
	write(t, filepath.Join(tree, "edited.txt"), "edit\n")
	if err := os.WriteFile(filepath.Join(tree, ".env"), []byte("K=1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	want := modesOfTree(tree)
	if want["README.md"] != 0o664 || want[".env"] != 0o640 {
		t.Fatalf("the copy's modes are %v", want)
	}
	a.seal()
	b := newMachineNoRepo(t)
	a.moveTo(b)
	syscall.Umask(0o077)
	if _, err := restorer.Restore(b.cell, b.project); err != nil {
		t.Fatal(err)
	}
	got := modesOfTree(filepath.Join(b.cell.Root, cell.TreesDir, "umask"))
	for rel, mode := range want {
		// The plain cutter of this test does not copy an ignored file; the real
		// road does, and its file is put right by the same step.
		if _, there := got[rel]; there && got[rel] != mode {
			t.Errorf("%s came back as %v, want %v", rel, got[rel], mode)
		}
	}
	if got["README.md"] != want["README.md"] || got["edited.txt"] != want["edited.txt"] {
		t.Errorf("the checked-out and the carried file came back as %v and %v", got["README.md"], got["edited.txt"])
	}
}
