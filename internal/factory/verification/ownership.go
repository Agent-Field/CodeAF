package verification

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadIn reconciles old as well as new records against the caller's actual
// job boundary. A check outside that checkout is diagnostic, even if an old
// observer accidentally inherited it from a negative-test fixture. Original
// records are never rewritten or converted into successful checks.
func ReadIn(transcript, dir string) ([]Check, error) {
	checks, err := Read(transcript)
	if err != nil || dir == "" || len(checks) == 0 {
		return checks, err
	}
	owner, err := OwnerAt(dir)
	if err != nil {
		return checks, fmt.Errorf("verification owner unavailable: %w", err)
	}
	for i := range checks {
		c := &checks[i]
		if c.Diagnostic {
			continue
		}
		// Missing or relative provenance cannot prove an outside-worktree scope.
		// In particular, malformed committed records remain blocking.
		if c.Cwd == "" || !filepath.IsAbs(c.Cwd) {
			if c.Exit != nil && c.Problem == "" {
				c.Problem = "recorded check ownership is unknown"
			}
			continue
		}
		cwd, e := canonicalRecordedPath(c.Cwd)
		if e != nil {
			if c.Exit != nil && c.Problem == "" {
				c.Problem = "recorded check ownership cannot be resolved"
			}
			continue
		}
		rel, e := filepath.Rel(owner, cwd)
		outside := e == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))
		if c.OwnerRoot != "" {
			recorded, e := canonicalRecordedPath(c.OwnerRoot)
			outside = outside || (e == nil && recorded != owner)
		}
		if outside {
			c.Diagnostic = true
			c.DiagnosticReason = "recorded check belongs outside this job checkout"
		}
	}
	return checks, nil
}

// Resolve the existing prefix, including /var and /tmp aliases on macOS, even
// after a fixture has been deleted. No path-name heuristic or temp-directory
// exemption is used: a job whose own checkout lives in /tmp is still owned.
func canonicalRecordedPath(path string) (string, error) {
	path = filepath.Clean(path)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}
