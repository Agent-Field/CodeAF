//go:build !windows

// Git workflow-artifact exclusion
package util

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ExcludedPaths = []string{
	".senior-dev/",
}

const excludeSentinel = "# senior-dev: workflow artifacts (managed by senior-dev)"

// EnsureSeniorDevExcluded adds senior-dev's own folder to the exclude file git
// reads for workspace, once.
//
// THE FILE IS THE ONE GIT READS, which is not always <git-dir>/info/exclude.
// In a linked worktree the git dir is .git/worktrees/<name>, and git ignores
// an info/ folder there in favour of the common dir's. An exclude written
// beside the git dir changed nothing: `.senior-dev/` stayed untracked, and a
// commit of the tree's own status would have taken senior-dev's database,
// spec and tool logs with it. `rev-parse --git-path` names the file git
// actually consults, which for a linked worktree is the repository's shared
// one.
func EnsureSeniorDevExcluded(ctx context.Context, workspace string) (bool, error) {
	result, err := RunProcess(ctx, []string{"git", "rev-parse", "--git-path", "info/exclude"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: workspace},
		NoThrow:        true,
	})
	if err != nil || result.Code != 0 {
		return false, nil
	}
	excludePath := strings.TrimSpace(string(result.Stdout))
	if excludePath == "" {
		return false, nil
	}
	if !filepath.IsAbs(excludePath) {
		excludePath, _ = filepath.Abs(filepath.Join(workspace, excludePath))
	}
	_ = os.MkdirAll(filepath.Dir(excludePath), 0o777)
	currentBytes, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		currentBytes = nil
	}
	current := string(currentBytes)
	existing := map[string]bool{}
	for _, line := range strings.Split(current, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			existing[line] = true
		}
	}
	missing := []string{}
	for _, path := range ExcludedPaths {
		if !existing[path] {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return true, nil
	}
	addition := ""
	if current != "" && !strings.HasSuffix(current, "\n") {
		addition = "\n"
	}
	addition += excludeSentinel + "\n" + strings.Join(missing, "\n") + "\n"
	if err := os.WriteFile(excludePath, []byte(current+addition), 0o666); err != nil {
		return false, err
	}
	return true, nil
}
