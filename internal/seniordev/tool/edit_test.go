//go:build !windows

package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditLineEndingAndBOM(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, "file.txt")
	if err := os.WriteFile(path, []byte("\ufeffone\r\ntwo\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := execute(t, New(workDir), "edit", map[string]any{
		"filePath":  "file.txt",
		"oldString": "one\ntwo",
		"newString": "first\nsecond",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Title != "file.txt" || result.Output != "Edit applied successfully." {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\ufefffirst\r\nsecond\r\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestEditEmptyOldStringCreatesOrOverwrites(t *testing.T) {
	workDir := t.TempDir()
	registry := New(workDir)
	result, err := execute(t, registry, "edit", map[string]any{
		"filePath":  "nested/new.txt",
		"oldString": "",
		"newString": "\ufeffcreated",
	})
	if err != nil {
		t.Fatalf("Execute create: %v", err)
	}
	if result.Output != "Edit applied successfully." {
		t.Fatalf("Output = %q", result.Output)
	}
	data, err := os.ReadFile(filepath.Join(workDir, "nested", "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\ufeffcreated" {
		t.Fatalf("content = %q", data)
	}
}

func TestEditShellErrors(t *testing.T) {
	workDir := t.TempDir()
	registry := New(workDir)
	_, err := execute(t, registry, "edit", map[string]any{
		"filePath":  "",
		"oldString": "a",
		"newString": "b",
	})
	if err == nil || err.Error() != "filePath is required" {
		t.Fatalf("empty path error = %v", err)
	}
	_, err = execute(t, registry, "edit", map[string]any{
		"filePath":  "missing.txt",
		"oldString": "a",
		"newString": "b",
	})
	if err == nil || err.Error() != "File "+filepath.Join(workDir, "missing.txt")+" not found" {
		t.Fatalf("missing error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(workDir, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = execute(t, registry, "edit", map[string]any{
		"filePath":  "dir",
		"oldString": "a",
		"newString": "b",
	})
	if err == nil || err.Error() != "Path is a directory, not a file: "+filepath.Join(workDir, "dir") {
		t.Fatalf("directory error = %v", err)
	}
}
