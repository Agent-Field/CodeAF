// Eager per-write git checkpoint — port of src/util/eager-commit.ts:1-76
// (swe-pro 3b25a1a).
package util

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Agent-Field/swe-pro-go/internal/attribution"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

var skipEagerCommit = os.Getenv("CODEAF_EAGER_COMMIT") == "0"

type EagerCommitOptions struct {
	Cwd      string
	FilePath string
	Label    string
}

func EagerCommit(ctx context.Context, options EagerCommitOptions) {
	if skipEagerCommit {
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
		root = jscompat.Trim(string(rootResult.Stdout))
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
