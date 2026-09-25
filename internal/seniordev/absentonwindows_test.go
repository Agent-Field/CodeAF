//go:build !windows

package seniordev

import (
	"go/build"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// SENIOR-DEV'S ENGINE IS ABSENT ON WINDOWS. Its process groups, file locks and
// bash shell have no Windows form, so its files stay out of that build. The
// one exception is util/runshape.go: codeaf's cross-platform program folder
// reads those shared run facts even when the engine is absent.
func TestOnlySharedRunFactsOfSeniorDevReachAWindowsBuild(t *testing.T) {
	windows := build.Default
	windows.GOOS, windows.GOARCH, windows.CgoEnabled = "windows", "amd64", false
	checked := 0
	sharedFacts := filepath.Join("util", "runshape.go")
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
		if path == sharedFacts {
			if !included {
				t.Errorf("%s must remain available to codeaf's Windows program folder", path)
			}
			return nil
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
