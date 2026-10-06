package reasoners

import (
	"context"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/praf/appx"

	"github.com/Agent-Field/codeaf/internal/praf/harnessx"
	"github.com/Agent-Field/codeaf/internal/praf/prompts"
)

// DeepenFindings ports deepen_findings (registered but DEAD on the live path):
// the literal ground-truth verification pass over the changed code.
//
// Output keys (§B.2): findings. No non-empty patches short-circuits to an
// empty list without a harness call; parse failure degrades to an empty list.
func DeepenFindings(ctx context.Context, deps Deps, in DeepenInput) (map[string]any, error) {
	patches := filterPairs(in.DiffPatches)
	if len(patches) == 0 {
		return map[string]any{"findings": []any{}}, nil
	}

	// The builder embeds a file reference when the rendered patches exceed
	// 9000 characters and a repo path exists; the write is the reasoner's job.
	patchesText := renderPatches(patches)
	if utf8.RuneCountInString(patchesText) > 9000 && in.RepoPath != "" {
		if _, err := writeContextFile(patchesText, "deepen_diff.md", in.RepoPath); err != nil {
			return nil, err
		}
	}

	prompt := prompts.DeepenFindingsPrompt([]prompts.StrPair(in.DiffPatches), in.ExistingTitles, in.RepoPath, in.PrContext)
	parsed, _, err := harnessx.Run[deepenResult](ctx, deps.Harness, prompt, appx.HarnessOptions{Cwd: in.RepoPath})
	if err != nil {
		return nil, err
	}
	result := *parsed
	for i := range result.Findings {
		result.Findings[i].Tags = orEmptyStrs(result.Findings[i].Tags)
	}
	findings, err := dumpSlice(result.Findings)
	if err != nil {
		return nil, err
	}
	return map[string]any{"findings": findings}, nil
}
