// Package testutil keeps fixtures independent of the host's path aliases.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// CanonicalizeTempRoots makes temporary fixtures agree with canonical workspace paths.
// macOS hands every process a TMPDIR under /var/folders, which is a link to
// /private/var/folders, so a fixture's t.TempDir() spelling differs from the path
// the product records once it resolves links. Empty roots retain Go's defaults,
// and physical roots retain their original spelling. Other systems keep their
// environment untouched, because their temporary folder is not an alias.
// This belongs before suite setup because testing caches temporary parents per test.
func CanonicalizeTempRoots() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
		raw := os.Getenv(name)
		if raw == "" {
			continue
		}
		// Links are followed before any lexical cleaning, so a ".." after a link
		// climbs out of the link's target exactly as the kernel would.
		resolved, err := filepath.EvalSymlinks(raw)
		if err != nil {
			return fmt.Errorf("cannot resolve %s: %w", name, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return fmt.Errorf("cannot resolve %s: %w", name, err)
		}
		if resolved != filepath.Clean(raw) {
			if err := os.Setenv(name, resolved); err != nil {
				return fmt.Errorf("cannot set %s: %w", name, err)
			}
		}
	}
	return nil
}
