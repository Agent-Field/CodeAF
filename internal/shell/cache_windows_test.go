//go:build windows

package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// N1 binds a relative override to its current directory, never the cached
// choice from a former directory. No executable needs to run for this proof.
func TestWindowsRelativeOverrideCacheFollowsDirectory(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
	changeDir := func(dir string) {
		t.Helper()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
	}
	candidate := filepath.Join(a, "bash.exe")
	if err := os.WriteFile(candidate, []byte("resolver fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_BASH", "bash.exe")
	changeDir(a)
	if got, err := Bash(); err != nil || got != candidate {
		t.Fatalf("relative bash in A: %q, %v", got, err)
	}
	changeDir(b)
	if got, err := Bash(); got != "" || err == nil || err.Error() != fmt.Sprintf(MissingBash, "bash.exe") {
		t.Fatalf("relative bash in B: %q, %v", got, err)
	}
	changeDir(a)
	if got, err := Bash(); err != nil || got != candidate {
		t.Fatalf("relative bash back in A: %q, %v", got, err)
	}
	// Absolute choices retain their cached resolution across directory changes.
	t.Setenv("CODEAF_BASH", candidate)
	if got, err := Bash(); err != nil || got != candidate {
		t.Fatalf("absolute bash: %q, %v", got, err)
	}
	if err := os.Remove(candidate); err != nil {
		t.Fatal(err)
	}
	changeDir(b)
	if got, err := Bash(); err != nil || got != candidate {
		t.Fatalf("absolute cache depended on cwd: %q, %v", got, err)
	}
}
