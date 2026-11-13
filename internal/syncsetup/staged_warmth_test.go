package syncsetup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

func TestAStagedWarmChatKeepsItsHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"head":"local-head"}`), 0600); err != nil {
		t.Fatal(err)
	}
	l := landing{Engine: cellstore.SyncEngine{}, rootFor: func(string) string { return root }}
	if !l.Holds(cell.Cell{ID: "warm", Root: t.TempDir()}) {
		t.Fatal("staging hid the chat's existing history")
	}
}
