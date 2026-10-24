package cellstore

import (
	"path/filepath"
	"strings"
)

// planRelative compares Windows aliases before deriving a logical candidate.
// Rust canonical paths carry the extended-length prefix; Go's tree paths may
// omit it. Both spellings must screen the same physical bytes (C9/U4).
func planRelative(root, physical string) (string, error) {
	return filepath.Rel(planPath(root), planPath(physical))
}

func planPath(path string) string {
	if len(path) >= 8 && strings.EqualFold(path[:8], `\\?\UNC\`) {
		return `\\` + path[8:]
	}
	return strings.TrimPrefix(path, `\\?\`)
}
