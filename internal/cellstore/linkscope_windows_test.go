package cellstore

import (
	"os"
	"path/filepath"
	"testing"
)

// TestU4HardlinkHintsRequestTheFullC9Scope proves U4/C9: a native hardlink
// requests the same full candidate screen as the engine's Unix fallback,
// while an ordinary known file keeps the delta path.
func TestU4HardlinkHintsRequestTheFullC9Scope(t *testing.T) {
	tree := t.TempDir()
	first := filepath.Join(tree, "first")
	if err := os.WriteFile(first, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if needsLinkScope(tree, []string{"first"}) {
		t.Fatal("ordinary file lost its delta scope")
	}
	if err := os.Link(first, filepath.Join(tree, "linked")); err != nil {
		t.Fatal(err)
	}
	if !needsLinkScope(tree, []string{"linked"}) {
		t.Fatal("native hardlink did not request a full screen")
	}
}
