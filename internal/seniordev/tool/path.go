//go:build !windows

// Path confinement: every tool path resolves inside the workspace unless the
// registry allows external directories, in which case it asks first.
package tool

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/project"
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

// resolveWritePath is resolvePath for a path a tool is about to write: under
// ConfineWrites a path outside the workspace is refused, with a sentence the
// model can act on, before anything is asked or touched.
//
// IT FOLLOWS SYMLINKS. A link inside the workspace that points out of it is a
// write outside it, so the deepest part of the path that exists is resolved on
// both sides before they are compared (and macOS's /var and /private/var are
// the same place by the same rule).
//
// WHY IT EXISTS. codeaf keeps only what a run changes in the folder it is
// handed, on the branch it cut there. A run told by its brief to "make a
// checkout" cloned a repository into the person's own projects folder and
// edited it there with these tools: the task ended saying it had changed
// nothing, and the edits sat in a folder of the person's that no task owned.
// Reads stay open, because a task's statement can live outside its folder;
// the shell cannot be fenced this way, and the prompt says so.
func (r *Registry) resolveWritePath(path string) (string, error) {
	resolved, err := r.resolvePath(path)
	if err != nil || !r.confineWrites {
		return resolved, err
	}
	if !withinReal(r.workDir, resolved) {
		return "", fmt.Errorf("write refused: %s is outside this run's workspace (%s). "+
			"Only changes inside the workspace are handed back, so make this change there", path, r.workDir)
	}
	return resolved, nil
}

// withinReal reports whether target is root or under it once both have had
// their symlinks resolved as far as they exist.
func withinReal(root, target string) bool {
	relative, err := filepath.Rel(realPrefix(root), realPrefix(target))
	return err == nil && !outsidePath(relative)
}

// realPrefix resolves the symlinks of the deepest existing ancestor of path and
// puts the parts that do not exist yet back on the end.
func realPrefix(path string) string {
	path = filepath.Clean(path)
	var rest []string
	for current := path; ; {
		if real, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				real = filepath.Join(real, rest[i])
			}
			return real
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		rest = append(rest, filepath.Base(current))
		current = parent
	}
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
