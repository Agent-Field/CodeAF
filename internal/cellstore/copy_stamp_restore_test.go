package cellstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func TestMaterializePreservesTheLocalCopyBaseline(t *testing.T) {
	c := newCell(t)
	path := filepath.Join(c.Root, cell.CopyReadyPath)
	stamp := []byte(`{"original":"/fixture/original","bytes":100,"turns":1,"known":true}`)
	if err := os.WriteFile(path, stamp, 0600); err != nil {
		t.Fatal(err)
	}
	e := syncEngineOver(transportFunc(func(context.Context, Target, Op) ([]byte, error) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		return []byte(`{"snapshot":"arrived"}`), nil
	}))
	if err := e.Materialize(t.Context(), c, "arrived"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(stamp) {
		t.Fatalf("restore lost baseline: %q (%v)", got, err)
	}
}
