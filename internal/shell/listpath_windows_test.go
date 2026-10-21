//go:build windows

package shell

import (
	"path/filepath"
	"strings"
	"testing"
)

// F4 reaches the real ListPath with Windows' own absolutiser, including the
// current drive's separate directory for C:rel and its root for \tools\bin.
func TestWindowsListPathUsesNativeAbsoluteRules(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
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
