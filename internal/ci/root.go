package ci

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repositoryRoot is two directories up from this file, which is where go.mod
// and .github are; it is located from the source rather than the working
// directory so the test answers the same from any package or IDE. It lives in
// a non-test file because more than one _test.go in this package needs it and
// a helper deleted with its first consumer took the others down with it.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller gave no file")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s (%v); this file moved and the root did not follow", root, err)
	}
	return root
}
