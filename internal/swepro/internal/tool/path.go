// This file ports path confinement from swe-pro/src/tool/registry.ts at commit 3b25a1a.
package tool

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
)

func (r *Registry) resolvePath(path string) (string, error) {
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.workDir, candidate)
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	absolute = filepath.Clean(absolute)
	if r.instance != nil {
		absolute = project.RedirectIntoDirectory(absolute, *r.instance)
	}

	relative, err := filepath.Rel(r.workDir, absolute)
	if !r.allowExternal && (err != nil || outsidePath(relative)) {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}
	return absolute, nil
}

func outsidePath(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative)
}

func (r *Registry) askExternalDirectory(
	ctx context.Context,
	call steploop.ToolCall,
	target string,
	kind string,
) error {
	if !r.allowExternal || target == "" {
		return nil
	}
	inside := func(root string) bool {
		if root == "" {
			return false
		}
		relative, err := filepath.Rel(root, target)
		return err == nil && !outsidePath(relative)
	}
	worktree := r.worktree()
	if inside(r.workDir) || (worktree != string(filepath.Separator) && inside(worktree)) {
		return nil
	}
	directory := filepath.Dir(target)
	if kind == "directory" {
		directory = target
	}
	glob := filepath.ToSlash(filepath.Join(directory, "*"))
	return r.askWithAlways(ctx, call, "external_directory", []string{glob}, []string{glob}, map[string]any{
		"filepath":  target,
		"parentDir": directory,
	})
}
