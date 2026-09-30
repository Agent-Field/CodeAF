//go:build unix

package taskcopy

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// A file git writes again when the copy is cut comes back at the mode it left
// with, even though nothing about it changed as far as git can tell and the
// machine that cuts it has another umask. Git keeps only the executable bit, so
// the record has to hold the mode of every file in the copy, not just the
// changed ones, and apply it after the cut.
func TestCutAgainFilesKeepTheirModesUnderAnotherUmask(t *testing.T) {
	a := newMachine(t)
	tree := a.task("umask")
	want := map[string]os.FileMode{"README.md": 0o664, "keep.txt": 0o640}
	for rel, mode := range want {
		if err := os.Chmod(filepath.Join(tree, rel), mode); err != nil {
			t.Fatal(err)
		}
	}
	a.seal()
	b := newMachineNoRepo(t)
	a.moveTo(b)

	// The source's files are 0664 and 0640; this machine cuts with a umask that
	// would make every file it recreates 0600.
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	if _, err := restorer.Restore(b.cell, b.project); err != nil {
		t.Fatal(err)
	}
	for rel, mode := range want {
		info, err := os.Stat(filepath.Join(b.cell.Root, cell.TreesDir, "umask", rel))
		if err != nil || info.Mode().Perm() != mode {
			t.Errorf("%s came back as %v (%v), want %v", rel, info, err, mode)
		}
	}
}
