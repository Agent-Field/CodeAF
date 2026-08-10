// This file ports swe-pro/src/tool/write.ts:21-118 at commit 3b25a1a.
// Event-bus and LSP diagnostics remain host-service seams; the registry supplies
// formatting, permission, PlanDB, filesystem, and checkpoint behavior.
package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	patchpkg "github.com/Agent-Field/aforge-v2/internal/swepro/internal/patch"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/util"
)

type writeMetadata struct {
	Diagnostics map[string]any `json:"diagnostics"`
	Diff        string         `json:"diff"`
	FilePath    string         `json:"filepath"`
	Exists      bool           `json:"exists"`
}

func (r *Registry) executeWrite(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input writeInput
	if err := decodeInput(call.Input, &input, "content", "filePath"); err != nil {
		return steploop.ToolResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return steploop.ToolResult{}, err
	}

	resolved, err := r.resolvePath(input.FilePath)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if err := r.askExternalDirectory(ctx, call, resolved, "file"); err != nil {
		return steploop.ToolResult{}, err
	}
	formatter, err := r.formatterService()
	if err != nil {
		return steploop.ToolResult{}, err
	}
	source, readErr := os.ReadFile(resolved)
	exists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return steploop.ToolResult{}, readErr
	}
	sourceBOM, contentOld := splitBOM(strings.ToValidUTF8(string(source), "\uFFFD"))
	nextBOM, content := splitBOM(input.Content)
	desiredBOM := sourceBOM || nextBOM
	proposedDiff := proposedFileDiff(resolved, contentOld, content)
	binding := r.guardMutation(ctx, call, []string{resolved})
	if binding != nil {
		defer r.BeginGuardTask(binding.TaskID)()
	}
	pattern, relErr := filepath.Rel(r.worktree(), resolved)
	if relErr != nil {
		pattern = resolved
	}
	metadata := map[string]any{"filepath": resolved, "diff": proposedDiff}
	if binding != nil {
		metadata["planDBTaskId"] = binding.TaskID
	}
	if err := r.ask(ctx, call, "edit", []string{filepath.ToSlash(pattern)}, metadata); err != nil {
		return steploop.ToolResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return steploop.ToolResult{}, err
	}
	if err := os.WriteFile(resolved, []byte(joinBOM(content, desiredBOM)), 0o644); err != nil {
		return steploop.ToolResult{}, err
	}
	content, err = formatMutationFile(ctx, formatter, resolved, desiredBOM)
	if err != nil {
		return steploop.ToolResult{}, err
	}

	util.EagerCommit(ctx, util.EagerCommitOptions{Cwd: r.workDir, FilePath: resolved, Label: "write"})

	title, err := filepath.Rel(r.workDir, resolved)
	if err != nil {
		title = resolved
	}
	diff := TrimDiff(patchpkg.GenerateTwoFilesPatch(resolved, contentOld, content))
	return steploop.ToolResult{
		Title:  title,
		Output: "Wrote file successfully.",
		Metadata: rawMetadata(writeMetadata{
			Diagnostics: map[string]any{},
			Diff:        diff,
			FilePath:    resolved,
			Exists:      exists,
		}),
	}, nil
}

func splitBOM(value string) (bool, string) {
	if strings.HasPrefix(value, "\ufeff") {
		return true, strings.TrimPrefix(value, "\ufeff")
	}
	return false, value
}

func joinBOM(value string, bom bool) string {
	_, stripped := splitBOM(value)
	if bom {
		return "\ufeff" + stripped
	}
	return stripped
}
