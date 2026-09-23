//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestFileSystemWritesReadsAndDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	filesystem := NewFileSystem()
	nested := filepath.Join(root, "a", "b.json")
	if err := filesystem.WriteJSON(nested, map[string]any{"x": 1}); err == nil {
		t.Fatal("WriteJSON unexpectedly created parents")
	}
	if err := filesystem.WriteStringWithDirs(nested, `{"x":1}`, 0o600); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := filesystem.ReadJSON(nested, &value); err != nil {
		t.Fatal(err)
	}
	if value["x"].(float64) != 1 {
		t.Fatalf("read JSON: %#v", value)
	}
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	entries, err := filesystem.ReadDirectoryEntries(filepath.Join(root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != (DirEntry{Name: "b.json", Type: "file"}) {
		t.Fatalf("entries: %#v", entries)
	}
}

func TestFileSystemUpAndGlob(t *testing.T) {
	root := t.TempDir()
	filesystem := NewFileSystem()
	for _, rel := range []string{"package.json", "a/config.json", "a/b/file.go", "a/b/.hidden.go", ".root-hidden"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	start := filepath.Join(root, "a", "b")
	found, err := filesystem.Up(UpOptions{Targets: []string{"package.json", "config.json"}, Start: start})
	if err != nil {
		t.Fatal(err)
	}
	wantFound := []string{filepath.Join(root, "a", "config.json"), filepath.Join(root, "package.json")}
	if !reflect.DeepEqual(found, wantFound) {
		t.Fatalf("up = %v, want %v", found, wantFound)
	}

	matches, err := filesystem.Glob("**/*.go", GlobOptions{Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	if !reflect.DeepEqual(matches, []string{filepath.Join("a", "b", "file.go")}) {
		t.Fatalf("glob without dot: %v", matches)
	}
	matches, err = filesystem.Glob("**/*.go", GlobOptions{Cwd: root, Dot: true, Absolute: true})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	want := []string{filepath.Join(root, "a", "b", ".hidden.go"), filepath.Join(root, "a", "b", "file.go")}
	sort.Strings(want)
	if !reflect.DeepEqual(matches, want) {
		t.Fatalf("glob dot: %v, want %v", matches, want)
	}
}

func TestWindowsPathTransform(t *testing.T) {
	cases := map[string]string{
		"/c/x":          "C:/x",
		"/c":            "C:/",
		"/c:/x":         "C:/x",
		"/cygdrive/d/x": "D:/x",
		"/mnt/e/x":      "E:/x",
		"/code":         "/code",
	}
	for input, want := range cases {
		if got := windowsPath(input); got != want {
			t.Errorf("windowsPath(%q) = %q, want %q", input, got, want)
		}
	}
}
