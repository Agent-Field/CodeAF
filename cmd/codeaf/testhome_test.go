package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// testHome reports late writers after TempDir has attempted its removal, so a
// future cleanup failure names the files that raced the owner's shutdown.
// Call it before other TempDir calls because testing shares their removal.
func testHome(t *testing.T) string {
	t.Helper()
	var home string
	t.Cleanup(func() {
		_ = filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				t.Logf("late home walk: %v", err)
				return nil
			}
			relative, err := filepath.Rel(home, path)
			if err == nil {
				t.Logf("remaining under test home: %s", relative)
			}
			return nil
		})
	})
	home = t.TempDir()
	return home
}
