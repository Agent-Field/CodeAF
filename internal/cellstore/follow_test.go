package cellstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// W4 makes Windows's process-directory limit observable on every platform.
func TestFollowLongWorkspaceW4(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("a", 120), strings.Repeat("b", 120))
	to := cell.Cell{ID: "chat", Root: root}
	from := cell.Cell{ID: to.ID, Root: root + ".taking"}
	tree := canonicalTree(filepath.Join(to.Root, "work"))
	if len(tree) < 260 {
		t.Fatalf("W4 fixture needs a long workspace, got %d characters", len(tree))
	}
	calls := 0
	e := Engine{Binary: "engine", DataRoot: t.TempDir(), WorkspaceOf: func(c cell.Cell) string {
		return filepath.Join(c.Root, "work")
	}}
	e.Run = func(_ context.Context, dir string, _ []string, argv ...string) ([]byte, error) {
		calls++
		if len(dir) >= 260 {
			return nil, fmt.Errorf("Windows process directory is too long: %s", dir)
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("W4 process directory %q: %v", dir, err)
		}
		at := slices.Index(argv, "--repo")
		if at < 0 || at+1 == len(argv) || argv[at+1] != tree {
			t.Fatalf("W4 workspace argument: %q, want %q", argv, tree)
		}
		return []byte(`{"changes":[]}`), nil
	}
	if err := e.Follow(t.Context(), from, to); err != nil {
		t.Fatal(err)
	}
	if dirty, err := e.Dirty(t.Context(), to); err != nil || dirty {
		t.Fatalf("W4 dirty=%v: %v", dirty, err)
	}
	if calls != 2 {
		t.Fatalf("W4 engine calls = %d, want rebind and dirty plan", calls)
	}
}
