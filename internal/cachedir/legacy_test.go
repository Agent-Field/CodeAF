package cachedir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestCacheAccountingAndExplicitCleanIncludeLegacyProjectCache(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	caches := []string{filepath.Join(cachedir.Root(), "workspaces", "new", "cache", "compiler"), home.Join("workspace-caches", "legacy", "cache", "compiler")}
	for _, path := range caches {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("cache bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	protected := []string{"v3/chats/example/transcript.jsonl", "task-storage/vault/archive.sqlite", "credentials.json", "projects/output.txt"}
	for _, rel := range protected {
		path := home.Join(rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep exact bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got := cachedir.Size(); got != 22 {
		t.Fatalf("accounted cache=%d; want22 including retained legacy cache", got)
	}
	freed, err := cachedir.Clean()
	if err != nil || freed != 22 {
		t.Fatalf("explicit cache clean=(%d,%v)", freed, err)
	}
	for _, path := range caches {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cache remains: %v", err)
		}
	}
	for _, rel := range protected {
		raw, err := os.ReadFile(home.Join(rel))
		if err != nil || string(raw) != "keep exact bytes" {
			t.Fatalf("cache clean touched protected %s: %v", rel, err)
		}
	}
}

func TestLegacyRuntimeCacheCleanPreservesTempAndIgnoresLinkedRuntime(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	runtime := home.Join("workspace-runtime")
	leaf := filepath.Join(runtime, strings.Repeat("1", 64))
	cache := filepath.Join(leaf, "cache")
	for _, dir := range []string{cache, filepath.Join(leaf, "temp")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cache, "compiler"), []byte("derived"), 0600); err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(leaf, "temp", "keep")
	if err := os.WriteFile(temp, []byte("active scratch"), 0600); err != nil {
		t.Fatal(err)
	}
	locations := cachedir.Locations()
	if len(locations) != 2 || locations[1] != cache {
		t.Fatalf("cleanup paths do not name exact legacy cache leaf: %v", locations)
	}
	if got := cachedir.Size(); got != 7 {
		t.Fatalf("temporary bytes counted as cache: %d", got)
	}
	if _, err := cachedir.Clean(); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(temp); err != nil || string(raw) != "active scratch" {
		t.Fatal("cache clean reached runtime temporary files")
	}
	outside := t.TempDir()
	outsideCache := filepath.Join(outside, strings.Repeat("2", 64), "cache")
	if err := os.MkdirAll(outsideCache, 0700); err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(outsideCache, "keep")
	if err := os.WriteFile(protected, []byte("not ours"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(runtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, runtime); err != nil {
		t.Skip(err)
	}
	if got := cachedir.Size(); got != 0 {
		t.Fatalf("followed substituted runtime directory: %d", got)
	}
	if _, err := cachedir.Clean(); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(protected); err != nil || string(raw) != "not ours" {
		t.Fatal("cache clean followed runtime directory link")
	}
}
