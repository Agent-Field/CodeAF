package cellstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// SPEC-3 item 5: reading just the first turn preserves valid large first records.
func TestFixRound5FirstTurnHasNoNewSizeLimit(t *testing.T) {
	c, err := cell.Create(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	first := Turn{ID: "first", Device: "original", ExcludedPaths: []string{strings.Repeat("long-path", 1<<14)}}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Root, filepath.FromSlash(TurnsPath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tail := range []string{"", "\nnot another turn"} {
		if err := os.WriteFile(path, append(append([]byte{}, raw...), tail...), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := FirstTurn(c)
		if err != nil || got == nil || got.ID != first.ID || got.Device != first.Device || len(got.ExcludedPaths) != 1 || got.ExcludedPaths[0] != first.ExcludedPaths[0] {
			t.Fatalf("large first turn was not read whole: %v", err)
		}
	}
}
