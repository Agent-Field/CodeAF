//go:build !windows

package util

import "testing"

// A PATH IGNORED AT START IS THE FILE ITSELF OR ANYTHING UNDER IT AS A FOLDER,
// and never a sibling that merely shares its prefix: `build` covers
// `build/out.o`, not `builder.go`.
func TestPathIgnoredAtStartMatchesAFileOrAFoldersChildrenOnly(t *testing.T) {
	ignored := []string{"", "secret.env", "build", "cache/"}
	for path, want := range map[string]bool{
		"secret.env":       true,
		"secret.env.local": false,
		"build":            true,
		"build/out.o":      true,
		"builder.go":       false,
		"cache/a/b.bin":    true,
		"cache":            false,
		"cached.txt":       false,
		"src/build/out.o":  false,
	} {
		if got := PathIgnoredAtStart(path, ignored); got != want {
			t.Errorf("PathIgnoredAtStart(%q) = %v, want %v", path, got, want)
		}
	}
	if PathIgnoredAtStart("anything", nil) {
		t.Error("an empty list ignored a path")
	}
}
