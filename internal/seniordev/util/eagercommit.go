//go:build !windows

// Eager per-write git checkpoint
package util

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/seniordev/attribution"
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
	relative, _ := filepath.Rel(root, options.FilePath)
	if relative == "" {
		relative = options.FilePath
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
	message := attribution.AppendCommitTrailer("wip(" + options.Label + "): " + relative)
	_, _ = RunProcess(ctx, attribution.GitArgv(
		"commit", "-m", message, "--no-verify", "--only", "--", relative,
	), RunOptions{ProcessOptions: ProcessOptions{Cwd: root}, NoThrow: true})
}
