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

// Contract 1: callers using os.Root must retain their opened-directory boundary.
func TestPublishContract1SyncOpenedDirectory(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := SyncDirFile(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.Stat(); err != nil {
		t.Fatalf("directory sync closed the caller's handle: %v", err)
	}
	if err := root.WriteFile("file", []byte("state"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := root.Open("file")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := SyncDirFile(file); err == nil {
		t.Fatal("a state file was accepted as a directory")
	}
}
