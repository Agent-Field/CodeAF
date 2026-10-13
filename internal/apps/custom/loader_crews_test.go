package custom

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestCustomDefinitionLoader(t *testing.T) {
	root := t.TempDir()
	files, _ := spec.GoldenCRM()
	files["custom/summary.tsx"] = []byte(goodSource)
	for name, source := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "custom", "ignored.js"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	bundle, err := spec.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if string(bundle.Files["custom/summary.tsx"]) != goodSource || bundle.Files["custom/ignored.js"] != nil {
		t.Fatalf("loader paths: %v", bundle.Files.Paths())
	}
}
