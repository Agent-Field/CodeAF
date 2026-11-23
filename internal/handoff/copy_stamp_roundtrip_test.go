package handoff

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestTakeBackPreservesTheLocalCopyBaseline(t *testing.T) {
	_, a, _ := backFromB(t)
	path := filepath.Join(a.root(chatID), cell.CopyReadyPath)
	stamp := []byte(`{"original":"/fixture/original","bytes":100,"head":"baseline","turns":1,"known":true}`)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stamp, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.taker().Take(t.Context(), chatID); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, stamp) {
		t.Fatalf("take-back lost baseline: %q (%v)", got, err)
	}
}
