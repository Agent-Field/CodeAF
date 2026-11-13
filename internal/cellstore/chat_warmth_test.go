package cellstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestChatWarmthBelongsToItsOwnHead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	data := t.TempDir()
	c := cell.Cell{ID: "new-chat", Root: t.TempDir()}
	e := SyncEngine{Ledger: "used", Target: func(cell.Cell) Target { return Target{DataDir: data} }}
	if err := os.WriteFile(filepath.Join(data, "published.used"), []byte("earlier chat's objects"), 0600); err != nil {
		t.Fatal(err)
	}
	if e.Holds(c) {
		t.Fatal("an unrelated chat's ledger made this chat warm")
	}
	if err := noteAdopted(c, "held-head"); err != nil {
		t.Fatal(err)
	}
	if !e.Holds(c) {
		t.Fatal("this chat's adopted history was forgotten")
	}
}
