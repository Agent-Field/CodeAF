package taskstorage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/taskstorage"
)

func TestNativeArchiveRetiresAndReopensWithoutWritableSharing(t *testing.T) {
	binary := env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY")
	if binary == "" {
		t.Skip("prototype archive engine not configured")
	}
	root := t.TempDir()
	trees := filepath.Join(root, "trees")
	if e := os.MkdirAll(trees, 0700); e != nil {
		t.Fatal(e)
	}
	m := taskstorage.Manager{Root: filepath.Join(root, "storage"), Trees: trees, Vault: taskstorage.CLIBackend{Binary: binary, Store: filepath.Join(root, "storage", "vault")}, Busy: func(string) bool { return false }}
	ctx := context.Background()
	payload := "exact private task content\n"
	paths := []string{filepath.Join(trees, "a"), filepath.Join(trees, "b")}
	for _, path := range paths {
		if e := os.MkdirAll(filepath.Join(path, ".git"), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(path, "work.txt"), []byte(payload), 0440); e != nil {
			t.Fatal(e)
		}
		if e := os.Link(filepath.Join(path, "work.txt"), filepath.Join(path, "alias")); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink("missing", filepath.Join(path, "link")); e != nil {
			t.Fatal(e)
		}
		if e := m.Park(ctx, path); e != nil {
			t.Fatal(e)
		}
	}
	for _, path := range paths {
		if e := m.Ensure(ctx, path); e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(filepath.Join(path, "work.txt"))
		if e != nil || string(b) != payload {
			t.Fatalf("restored %q %v", b, e)
		}
		a, e := os.Stat(filepath.Join(path, "work.txt"))
		if e != nil {
			t.Fatal(e)
		}
		binfo, e := os.Stat(filepath.Join(path, "alias"))
		if e != nil || !os.SameFile(a, binfo) {
			t.Fatal("hardlink relationship lost")
		}
	}
	if e := os.Chmod(filepath.Join(paths[0], "work.txt"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(paths[0], "work.txt"), []byte("independent edit"), 0600); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(paths[1], "work.txt"))
	if e != nil || string(b) != payload {
		t.Fatal("editing one restore changed its sibling")
	}
	if e := m.Park(ctx, paths[1]); e != nil {
		t.Fatal(e)
	}
	store := filepath.Join(root, "storage", "vault")
	if e := os.Rename(store, store+".offline"); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(ctx, paths[1]); e == nil {
		t.Fatal("unavailable vault accepted")
	}
	if _, e := os.Stat(paths[1]); !os.IsNotExist(e) {
		t.Fatal("unavailable vault produced a partial or fresh workspace")
	}
	if e := os.RemoveAll(store); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(store+".offline", store); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(ctx, paths[1]); e != nil {
		t.Fatal(e)
	}
}
