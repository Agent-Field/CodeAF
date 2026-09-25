//go:build !windows

package seniordev

import (
	"go/build"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// SENIOR-DEV IS ABSENT ON WINDOWS, NOT BROKEN THERE. Its engine has never had
// a Windows form of its process groups, file locks and bash shell, so no file
// of it may reach a Windows build: the build's list is empty there
// (internal/delegate/builtin/carried_windows.go), and this holds every Go file
// under this tree, tests included, to a constraint that keeps it out. A file
// that forgot one would put half an engine into a Windows build, where it
// either fails to compile or compiles into something that fails every time.
func TestNoFileOfSeniorDevReachesAWindowsBuild(t *testing.T) {
	windows := build.Default
	windows.GOOS, windows.GOARCH, windows.CgoEnabled = "windows", "amd64", false
	checked := 0
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		checked++
		included, err := windows.MatchFile(filepath.Dir(path), entry.Name())
		if err != nil {
			return err
		}
		if included {
			t.Errorf("%s would be compiled into a Windows build; give it //go:build !windows", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 200 {
		t.Fatalf("only %d files were checked; the walk has stopped seeing the tree", checked)
	}
}
