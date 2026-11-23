package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The suite's throwaway home never holds Go's module cache, so a toolchain or
// module that Go fetches during a test lands in the developer's own cache once
// instead of in a temporary folder on every run.
func TestTheSessionSuiteKeepsGoModulesOutOfItsThrowawayHome(t *testing.T) {
	cache, home := os.Getenv("GOMODCACHE"), os.Getenv("HOME")
	if cache == "" {
		t.Fatal("GOMODCACHE is unset, so Go's module cache follows the throwaway HOME")
	}
	if rel, err := filepath.Rel(home, cache); err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("Go's module cache %s sits inside the throwaway home %s", cache, home)
	}
}

// The cleanup removes a home holding read-only files in read-only folders, the
// shape Go's module cache leaves behind.
func TestTheThrowawayHomeIsRemovedEvenWhenItHoldsReadOnlyTrees(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home")
	locked := filepath.Join(root, "go", "pkg", "mod", "toolchain")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "go"), []byte("toolchain"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{locked, filepath.Dir(locked)} {
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	removeTestHome(root)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("the throwaway home survived its cleanup: %v", err)
	}
}
