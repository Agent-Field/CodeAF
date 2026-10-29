// Package testutil keeps fixtures independent of the host's path aliases.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalizeTempRoots makes temporary fixtures agree with canonical workspace paths.
// Empty roots retain Go's defaults, and physical roots retain their original spelling.
// This belongs before suite setup because testing caches temporary parents per test.
func CanonicalizeTempRoots() error {
	for _, name := range []string{"TMPDIR", "GOTMPDIR"} {
		raw := os.Getenv(name)
		if raw == "" {
			continue
		}
		absolute, err := filepath.Abs(raw)
		if err != nil {
			return fmt.Errorf("cannot resolve %s: %w", name, err)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
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
