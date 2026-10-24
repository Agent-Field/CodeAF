package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestC11WindowsOmitsStaleEngines proves C11's packaging contract after a
// developer has already staged engines for other platforms in this checkout.
func TestC11WindowsOmitsStaleEngines(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"furrow-linux-amd64.gz", "furrow-darwin-arm64.gz", "README.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearStaged(dir, ""); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "README.md" {
		t.Fatalf("Windows packaging retained: %v", files)
	}
}
