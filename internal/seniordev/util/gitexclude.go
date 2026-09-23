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

func EnsureSeniorDevExcluded(ctx context.Context, workspace string) (bool, error) {
	result, err := RunProcess(ctx, []string{"git", "rev-parse", "--git-dir"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: workspace},
		NoThrow:        true,
	})
	if err != nil || result.Code != 0 {
		return false, nil
	}
	gitDirText := strings.TrimSpace(string(result.Stdout))
	if gitDirText == "" {
		return false, nil
	}
	gitDir := gitDirText
	if !filepath.IsAbs(gitDir) {
		gitDir, _ = filepath.Abs(filepath.Join(workspace, gitDir))
	}
	infoDir := filepath.Join(gitDir, "info")
	_ = os.MkdirAll(infoDir, 0o777)
	excludePath := filepath.Join(infoDir, "exclude")
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
