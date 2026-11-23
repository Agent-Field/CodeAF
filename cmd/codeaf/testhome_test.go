package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// testHome reports late writers after TempDir has attempted its removal, so a
// future cleanup failure names the files that raced the owner's shutdown. It
// reports only for a failed test, and it still sees a late writer only when it
// is called before any other TempDir in the test, because testing shares their
// removal.
//
// IT SPEAKS ONLY FOR A TEST THAT FAILED. A test that called TempDir before
// testHome has its shared removal registered first, so it runs after this
// walk, and the walk would list every file the test legitimately left for that
// removal. A late writer fails the removal itself, which marks the test failed
// when the walk runs after it, so gating on Failed loses no late writer the walk
// could have named, and keeps a passing test's output silent.
func testHome(t *testing.T) string {
	t.Helper()
	var home string
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
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
