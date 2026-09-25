//go:build !windows

// Eager per-write git checkpoint
package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// skipEagerCommit is read on every write and set from two places: the
// environment at startup, and the run once it knows which workspace recorder
// it is using. Atomic because the write happens before the run starts while
// the reads happen on tool goroutines.
var skipEagerCommit atomic.Bool

func init() { skipEagerCommit.Store(os.Getenv("SENIOR_DEV_EAGER_COMMIT") == "0") }

// DisableEagerCommit turns off the per-write checkpoint for the rest of the
// process. A run whose recorder keeps its own snapshots does not want commits
// in the workspace -- under --in-place that workspace may be a repository the
// run has no business writing history into. Call it before the run starts.
func DisableEagerCommit() { skipEagerCommit.Store(true) }

type EagerCommitOptions struct {
	Cwd      string
	FilePath string
	Label    string
}

func EagerCommit(ctx context.Context, options EagerCommitOptions) {
	if skipEagerCommit.Load() {
		return
	}
	defer func() { _ = recover() }()
	// The branch comes from codeaf's folder preparation, not the current HEAD:
	// a shell command or editor can move HEAD between two file tools.
	expected := strings.TrimSpace(os.Getenv("SENIOR_DEV_EXPECTED_BRANCH"))
	if !strings.HasPrefix(expected, "task/") {
		return
	}
	inRepo, _ := RunProcess(ctx, []string{"git", "rev-parse", "--is-inside-work-tree"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: options.Cwd}, NoThrow: true,
	})
	if inRepo.Code != 0 {
		return
	}
	rootResult, _ := RunProcess(ctx, []string{"git", "rev-parse", "--show-toplevel"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: options.Cwd}, NoThrow: true,
	})
	root := options.Cwd
	if rootResult.Code == 0 {
		root = strings.TrimSpace(string(rootResult.Stdout))
	}
	head, _ := RunProcess(ctx, []string{"git", "symbolic-ref", "--quiet", "--short", "HEAD"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: root}, NoThrow: true,
	})
	if head.Code != 0 || strings.TrimSpace(string(head.Stdout)) != expected {
		return
	}
	relative := repositoryRelative(root, options.Cwd, options.FilePath)
	if GeneratedRunPath(relative) || IgnoredAtStart(relative) {
		return
	}
	add, _ := RunProcess(ctx, []string{"git", "add", "--", relative}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: root}, NoThrow: true,
	})
	if add.Code != 0 {
		return
	}
	diff, _ := RunProcess(ctx, []string{"git", "diff", "--cached", "--quiet", "--", relative}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: root}, NoThrow: true,
	})
	if diff.Code == 0 {
		return
	}
	// A checkout may have moved while git was staging this path. Read HEAD
	// again immediately before the commit, leaving the edit uncommitted there.
	head, _ = RunProcess(ctx, []string{"git", "symbolic-ref", "--quiet", "--short", "HEAD"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: root}, NoThrow: true,
	})
	if head.Code != 0 || strings.TrimSpace(string(head.Stdout)) != expected {
		return
	}
	message := "wip(" + options.Label + "): " + relative
	_, _ = RunProcess(ctx, GitArgv(
		"commit", "-m", message, "--no-verify", "--only", "--", relative,
	), RunOptions{ProcessOptions: ProcessOptions{Cwd: root}, NoThrow: true})
}

// IgnoredAtStart reads the parent's frozen ignore list. It is a small file in
// the run record, since ignored directories can contain thousands of files.
func IgnoredAtStart(path string) bool {
	paths, err := InitialIgnoredPaths()
	if err != nil {
		// An unreadable safety list must never permit an eager commit.
		return true
	}
	return PathIgnoredAtStart(path, paths)
}

// InitialIgnoredPaths reads the one list codeaf captured before the run.
func InitialIgnoredPaths() ([]string, error) {
	list := os.Getenv("SENIOR_DEV_IGNORED_AT_START")
	if list == "" {
		return nil, nil
	}
	body, err := os.ReadFile(list)
	if err != nil {
		return nil, fmt.Errorf("read start-time ignore list: %w", err)
	}
	return strings.Split(string(body), "\x00"), nil
}

// PathIgnoredAtStart matches a file or a child of an ignored directory.
func PathIgnoredAtStart(path string, paths []string) bool {
	for _, ignored := range paths {
		if ignored != "" && (path == ignored || strings.HasPrefix(path, strings.TrimSuffix(ignored, "/")+"/")) {
			return true
		}
	}
	return false
}

// repositoryRelative names a written file inside the repository whose top
// level git reported as root.
//
// GIT REPORTS ITS TOP LEVEL WITH EVERY SYMLINK RESOLVED, and the path a tool
// hands in need not be. On macOS every temporary folder is /var/folders/…,
// which is a link to /private/var/folders/…, so a file under the one measured
// against a root under the other walked out of the repository
// ("../../../var/folders/…"), `git add` refused it, and every per-file commit
// in such a workspace stopped without a word while the run went on believing
// it was checkpointing. Both sides are resolved before they are compared.
func repositoryRelative(root, cwd, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	relative, err := filepath.Rel(resolveExisting(root), resolveExisting(path))
	if err != nil || relative == "" {
		return path
	}
	return relative
}

// resolveExisting resolves the symlinks in path, or in its nearest ancestor
// that exists when the path itself does not (a file just deleted still has a
// folder, and the folder is what carries the link).
func resolveExisting(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolveExisting(parent), filepath.Base(path))
}
