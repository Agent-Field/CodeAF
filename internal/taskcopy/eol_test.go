package taskcopy

import (
	"os"
	"path/filepath"
	"testing"
)

// TestK4TaskCopyLineEndings proves K3/K4: a clean committed LF or CRLF file and
// a mixed-ending file retain their actual bytes despite opposite checkout
// settings. A copy with ordinary clean files still needs only its record.
func TestK4TaskCopyLineEndings(t *testing.T) {
	a := newMachine(t)
	run(t, a.project, "git", "config", "core.autocrlf", "false")
	tree := a.task("eol")
	files := map[string]string{"lf.txt": "one\ntwo\n", "crlf.txt": "one\r\ntwo\r\n", "mixed.txt": "one\ntwo\r\n"}
	for path, data := range files {
		write(t, filepath.Join(tree, path), data)
	}
	run(t, tree, "git", "add", ".")
	run(t, tree, "git", "commit", "-q", "-m", "line endings")
	a.seal()
	b := newMachineNoRepo(t)
	run(t, filepath.Dir(b.project), "git", "clone", "-q", "--no-checkout", a.project, b.project)
	run(t, b.project, "git", "config", "core.autocrlf", "true")
	run(t, b.project, "git", "config", "core.eol", "crlf")
	if err := os.CopyFS(carriedRoot(b.cell), os.DirFS(carriedRoot(a.cell))); err != nil {
		t.Fatal(err)
	}
	if _, err := restorer.Restore(b.cell, b.project); err != nil {
		t.Fatal(err)
	}
	for path, want := range files {
		if got, ok := b.read("eol", path); !ok || got != want {
			t.Errorf("K4 %s = %q want %q", path, got, want)
		}
	}
}
