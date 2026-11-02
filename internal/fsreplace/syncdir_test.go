package fsreplace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestA4SyncDirectoryKeepsErrorsAndSupportsNativeStorage(t *testing.T) {
	dir := t.TempDir()
	if err := SyncDir(dir); err != nil {
		t.Fatalf("A4: sync existing directory: %v", err)
	}
	if err := SyncDir(filepath.Join(dir, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("A4: missing directory error lost: %v", err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SyncDir(file); err == nil {
		t.Fatal("A4: a file was accepted as a directory")
	}
}
