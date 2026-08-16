// Git workflow-artifact exclusion — port of src/util/git-exclude.ts:1-90
// (swe-pro 3b25a1a).
package util

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/enginestate"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// aforge-embed: D7 — the list is declared once, at
// internal/swepro/enginestate, and read from both sides of the embedding.
// Upstream (and the TS source) spell these five strings here. They are the
// boundary between the engine's machinery and somebody's repository, and the
// harness draws that boundary too — in what it excludes, in what it refuses to
// stage onto a landing commit, and in what it keeps out of a delivered file
// list. Three copies of one list is three lists, and one of aforge's had
// already drifted.
var ExcludedPaths = enginestate.ExcludePatterns()

const excludeSentinel = "# codeaf: workflow artifacts (managed by ensureCodeafExcluded)"

func EnsureCodeafExcluded(ctx context.Context, workspace string) (bool, error) {
	// aforge-embed: D7 — `--git-path info/exclude` where the TS source says
	// `--git-dir` + "/info/exclude". The two agree in an ordinary clone and
	// disagree in a linked worktree, where `--git-dir` answers with the
	// worktree's PRIVATE gitdir and the file git actually reads for exclusions
	// is the one in the COMMON directory. Written to the private path this
	// function did its whole job — created the directory, wrote the five
	// patterns, returned (true, nil) — into a file git never opens, and the
	// engine's state was staged by the next `add -A` with nothing anywhere
	// saying so. aforge runs every isolated coding leaf in a linked worktree,
	// so the layout the bug needs is the layout it always has.
	result, err := RunProcess(ctx, []string{"git", "rev-parse", "--git-path", "info/exclude"}, RunOptions{
		ProcessOptions: ProcessOptions{Cwd: workspace},
		NoThrow:        true,
	})
	if err != nil || result.Code != 0 {
		return false, nil
	}
	excludeText := jscompat.Trim(string(result.Stdout))
	if excludeText == "" {
		return false, nil
	}
	excludePath := excludeText
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
