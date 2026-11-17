package celltest_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEngineCleanupUsesEngineDir(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	direct := regexp.MustCompile(`\.Cleanup\s*\(\s*cellstore\.StopSpawned\s*\)`)
	for _, tree := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if direct.Match(raw) {
				t.Errorf("%s: use celltest.EngineDir instead of registering StopSpawned directly", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
