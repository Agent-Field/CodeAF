//go:build !windows

package tool

import "path/filepath"

// worktree is the directory tool paths are reported relative to. It is the
// registry's workspace unless a project instance in context names a different
// worktree, which is the case only when an embedder runs the registry against
// a checkout other than the one it was constructed for.
func (r *Registry) worktree() string {
	if r.instance != nil && r.instance.Worktree != "" {
		return filepath.Clean(r.instance.Worktree)
	}
	return r.workDir
}
