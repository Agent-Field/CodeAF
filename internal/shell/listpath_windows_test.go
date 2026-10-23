//go:build windows

package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// N2 and N3 reach the real ListPath with Windows' own absolutiser, including the
// current drive's separate directory for C:rel and its root for \tools\bin.
func TestWindowsListPathUsesNativeAbsoluteRules(t *testing.T) {
	cwd := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	drive := filepath.VolumeName(cwd)
	for _, native := range []string{drive, drive + `skills\bin`, `\tools\bin`, `rel\x`} {
		absolute, err := filepath.Abs(native)
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(absolute) || len(absolute) < 3 || absolute[1] != ':' {
			t.Fatalf("test requires a drive-based temporary directory: %q", absolute)
		}
		want := "/" + strings.ToLower(absolute[:1]) + filepath.ToSlash(absolute[2:])
		if got := ListPath(native); got != want {
			t.Errorf("%q => %q, want %q from native absolute %q", native, got, want, absolute)
		}
	}
}
