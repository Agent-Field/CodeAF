package cellstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
)

// Contract H4 keeps repeated admission in different scratch folders off the real timeline.
func TestRepeatedAdmissionNeverTouchesTheChatTimeline(t *testing.T) {
	celltest.RequireEngine(t)
	engine := realEngine(t)
	source := newCell(t)
	if err := os.WriteFile(filepath.Join(source.Root, "work.txt"), []byte("saved work"), 0600); err != nil {
		t.Fatal(err)
	}
	sealed, err := engine.Seal(t.Context(), source, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Await(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	bindings, err := filepath.Glob(filepath.Join(engine.StoreDir(source), "store-v1", "workspaces", "*", "root.path"))
	if err != nil || len(bindings) != 1 {
		t.Fatalf("source bindings=%v: %v", bindings, err)
	}
	paths := []string{bindings[0], filepath.Join(filepath.Dir(bindings[0]), "paths.head"), filepath.Join(engine.LocalDir(source), "engine-tree")}
	type savedFile struct {
		body string
		info os.FileInfo
	}
	before := make(map[string]savedFile)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = savedFile{string(raw), info}
	}
	for range 3 {
		stage := cell.Cell{ID: source.ID, Root: t.TempDir()}
		if err := engine.StageAdmission(t.Context(), stage, sealed.Turn.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(stage.Root, "work.txt")); !os.IsNotExist(err) {
			t.Fatalf("admission restored working files: %v", err)
		}
		if err := engine.StageWorkspace(t.Context(), stage, sealed.Turn.ID); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(filepath.Join(stage.Root, "work.txt")); err != nil || string(raw) != "saved work" {
			t.Fatalf("workspace admission=%q: %v", raw, err)
		}
	}
	for path, saved := range before {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != saved.body {
			t.Fatalf("admission changed binding contents: %s: %v", path, err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(saved.info.ModTime()) {
			t.Fatalf("admission touched timeline metadata: %s: %v", path, err)
		}
	}
}
