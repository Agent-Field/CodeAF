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

// resolveMutationPath is the write-side choke point shared by edit, write,
// and apply_patch. It resolves the path exactly like resolvePath and then,
// for the worker (the coder agent), refuses any target inside the harness's
// own .codeaf/ tree. That directory is engine state — the registered contract,
// artifacts, audit verdicts, plan records — not part of the task, and a coder
// leaf editing it (the FEATURE cell regressed this way) corrupts the run.
//
// The guard is agent-scoped, not blanket: the harness's own agents legitimately
// write .codeaf/ through these same tools — the auditor writes its verdict to
// .codeaf/auditor-verdict.json, the architect to .codeaf/plan/architecture.md.
// Only the coder (the generalist worker) is refused; reads keep using
// resolvePath and stay allowed, since the root-cut flow instructs reading
// .codeaf/contract.json. call.Agent is populated from the leaf's message agent
// (processor.go), so a coder leaf's tool call carries Agent == "coder".
//
// aforge-embed: D9 — see internal/swepro/EMBEDDING.md.
func (r *Registry) resolveMutationPath(call steploop.ToolCall, path string) (string, error) {
	resolved, err := r.resolvePath(path)
	if err != nil {
		return "", err
	}
	if err := r.rejectHarnessWrite(call, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

// rejectHarnessWrite refuses a coder's attempt to mutate the workspace's
// top-level .codeaf/ machinery directory. Only that root .codeaf is harness
// state; a nested .codeaf elsewhere in the tree is the user's own and is left
// alone. The check is anchored on r.workDir, the same root resolvePath uses,
// so a path the agent can reach is judged against the directory it lives in.
func (r *Registry) rejectHarnessWrite(call steploop.ToolCall, resolved string) error {
	if call.Agent != "coder" {
		return nil
	}
	rel, err := filepath.Rel(r.workDir, resolved)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	if rel == ".codeaf" || strings.HasPrefix(rel, ".codeaf/") {
		return fmt.Errorf("harness machinery; not part of the task: %s", filepath.ToSlash(resolved))
	}
	return nil
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
