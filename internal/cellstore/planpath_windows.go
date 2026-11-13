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

// composedPath keeps receipt destinations usable by the engine's native
// replacement calls. A short .cell directory can have children beyond MAX_PATH.
// Unlike the workspace, furrow does not canonicalize this composed path itself.
func composedPath(dir string) string {
	dir = canonicalTree(dir)
	if strings.HasPrefix(dir, `\\?\`) {
		return dir
	}
	if strings.HasPrefix(dir, `\\`) {
		return `\\?\UNC\` + dir[2:]
	}
	return `\\?\` + dir
}
