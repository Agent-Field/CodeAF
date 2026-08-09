// Git workflow-artifact exclusion — port of src/util/git-exclude.ts:1-90
// (swe-pro 3b25a1a).
package util

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

var ExcludedPaths = []string{
	".codeaf/",
	".plandb/",
	".plandb.db",
	".plandb.db-shm",
	".plandb.db-wal",
}

const excludeSentinel = "# codeaf: workflow artifacts (managed by ensureCodeafExcluded)"

func EnsureCodeafExcluded(ctx context.Context, workspace string) (bool, error) {
	result, err := RunProcess(ctx, []string{"git", "rev-parse", "--git-dir"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: workspace},
		NoThrow:        true,
	})
	if err != nil || result.Code != 0 {
		return false, nil
	}
	gitDirText := jscompat.Trim(string(result.Stdout))
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
		line = jscompat.Trim(line)
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
