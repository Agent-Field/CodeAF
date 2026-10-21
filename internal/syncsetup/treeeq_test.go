package syncsetup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// assertSameTree compares every file outside the cell's own directory.
func assertSameTree(t *testing.T, a, b string) {
	t.Helper()
	seen := 0
	err := filepath.WalkDir(a, func(path string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(a, path)
		if err != nil || d.IsDir() || rel == cell.StateDir || filepath.Dir(rel) == cell.StateDir {
			return err
		}
		seen++
		want, _ := os.ReadFile(path)
		got, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: imported %q (%v), want %q", rel, got, err, want)
		}
		wi, _ := os.Stat(path)
		gi, _ := os.Stat(filepath.Join(b, rel))
		if gi != nil && gi.Mode() != wi.Mode() {
			t.Errorf("%s: mode %v, want %v", rel, gi.Mode(), wi.Mode())
		}
		return nil
	})
	if err != nil || seen == 0 {
		t.Fatalf("compared %d files, err %v", seen, err)
	}
}
