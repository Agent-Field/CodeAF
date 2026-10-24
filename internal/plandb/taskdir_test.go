package plandb

import (
	"os"
	"path/filepath"
	"testing"
)

// A cell keeps a task's folder inside .cell/ with the chat's other truth; a
// folder that is not a cell keeps it beside the store.
func TestTaskDirFollowsTheFolderLayout(t *testing.T) {
	dir := t.TempDir()
	if got, want := TaskDir(dir, "3"), filepath.Join(dir, "tasks", "3"); got != want {
		t.Errorf("legacy folder: TaskDir = %q, want %q", got, want)
	}
	if err := os.Mkdir(filepath.Join(dir, ".cell"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := TaskDir(dir, "3"), filepath.Join(dir, ".cell", "tasks", "3"); got != want {
		t.Errorf("cell: TaskDir = %q, want %q", got, want)
	}
}
