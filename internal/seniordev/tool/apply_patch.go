//go:build !windows

// The apply_patch tool: a multi-file patch envelope (add, update, move,
// delete) validated up front and applied after one permission check that
// covers every file it touches.
package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	patchpkg "github.com/Agent-Field/codeaf/internal/seniordev/patch"
)

type applyPatchChange struct {
	filePath   string
	oldContent string
	newContent string
	kind       string
	movePath   string
	diff       string
	additions  int
	deletions  int
	bom        bool
}

type applyPatchFileMetadata struct {
	FilePath     string `json:"filePath"`
	RelativePath string `json:"relativePath"`
	Type         string `json:"type"`
	Patch        string `json:"patch"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	MovePath     string `json:"movePath,omitempty"`
}

type applyPatchMetadata struct {
	Diff        string                   `json:"diff"`
	Files       []applyPatchFileMetadata `json:"files"`
	Diagnostics map[string]any           `json:"diagnostics"`
}

func (r *Registry) executeApplyPatch(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input applyPatchInput
	if err := decodeInput(call.Input, &input, "patchText"); err != nil {
		return steploop.ToolResult{}, err
	}
	if input.PatchText == "" {
		return steploop.ToolResult{}, errors.New("patchText is required")
	}
	parsed, err := patchpkg.ParsePatch(input.PatchText)
	if err != nil {
		return steploop.ToolResult{}, fmt.Errorf("apply_patch verification failed: Error: %s", err)
	}
	if len(parsed.Hunks) == 0 {
		normalized := strings.ReplaceAll(input.PatchText, "\r\n", "\n")
		normalized = strings.ReplaceAll(normalized, "\r", "\n")
		normalized = strings.TrimSpace(normalized)
		if normalized == "*** Begin Patch\n*** End Patch" {
			return steploop.ToolResult{}, errors.New("patch rejected: empty patch")
		}
		return steploop.ToolResult{}, errors.New("apply_patch verification failed: no hunks found")
	}
	formatter, err := r.formatterService()
	if err != nil {
		return steploop.ToolResult{}, err
	}

	changes := make([]applyPatchChange, 0, len(parsed.Hunks))
	for _, hunk := range parsed.Hunks {
		if err := ctx.Err(); err != nil {
			return steploop.ToolResult{}, err
		}
		filePath, err := r.resolvePath(hunk.Path)
		if err != nil {
			return steploop.ToolResult{}, err
		}
		if err := r.askExternalDirectory(ctx, call, filePath, "file"); err != nil {
			return steploop.ToolResult{}, err
		}
		switch hunk.Type {
		case "add":
			newContent := hunk.Contents
			if newContent != "" && !strings.HasSuffix(newContent, "\n") {
				newContent += "\n"
			}
			bom, newContent := splitBOM(newContent)
			additions, deletions := lineChangeCounts("", newContent)
			change := applyPatchChange{
				filePath:   filePath,
				oldContent: "",
				newContent: newContent,
				kind:       "add",
				diff:       proposedFileDiff(filePath, "", newContent),
				additions:  additions,
				deletions:  deletions,
				bom:        bom,
			}
			changes = append(changes, change)
		case "update":
			info, statErr := os.Stat(filePath)
			if statErr != nil || info.IsDir() {
				return steploop.ToolResult{}, fmt.Errorf(
					"apply_patch verification failed: Failed to read file to update: %s",
					filePath,
				)
			}
			source, readErr := os.ReadFile(filePath)
			if readErr != nil {
				return steploop.ToolResult{}, fmt.Errorf(
					"apply_patch verification failed: Failed to read file to update: %s",
					filePath,
				)
			}
			sourceBOM, oldContent := splitBOM(strings.ToValidUTF8(string(source), "\uFFFD"))
			update, deriveErr := patchpkg.DeriveNewContentsFromChunks(filePath, hunk.Chunks)
			if deriveErr != nil {
				return steploop.ToolResult{}, fmt.Errorf("apply_patch verification failed: Error: %s", deriveErr)
			}
			movePath := ""
			if hunk.MovePath != "" {
				movePath, err = r.resolvePath(hunk.MovePath)
				if err != nil {
					return steploop.ToolResult{}, err
				}
				if err := r.askExternalDirectory(ctx, call, movePath, "file"); err != nil {
					return steploop.ToolResult{}, err
				}
			}
			additions, deletions := lineChangeCounts(oldContent, update.Content)
			kind := "update"
			if hunk.MovePath != "" {
				kind = "move"
			}
			change := applyPatchChange{
				filePath:   filePath,
				oldContent: oldContent,
				newContent: update.Content,
				kind:       kind,
				movePath:   movePath,
				diff:       proposedFileDiff(filePath, oldContent, update.Content),
				additions:  additions,
				deletions:  deletions,
				bom:        sourceBOM || update.BOM,
			}
			changes = append(changes, change)
		case "delete":
			source, readErr := os.ReadFile(filePath)
			if readErr != nil {
				return steploop.ToolResult{}, fmt.Errorf("apply_patch verification failed: %s", readErr)
			}
			bom, oldContent := splitBOM(strings.ToValidUTF8(string(source), "\uFFFD"))
			change := applyPatchChange{
				filePath:   filePath,
				oldContent: oldContent,
				newContent: "",
				kind:       "delete",
				diff:       proposedFileDiff(filePath, oldContent, ""),
				additions:  0,
				deletions:  len(strings.Split(oldContent, "\n")),
				bom:        bom,
			}
			changes = append(changes, change)
		}
	}

	permissionFiles := make([]applyPatchFileMetadata, 0, len(changes))
	mutationPaths := make([]string, 0, len(changes)*2)
	permissionPaths := make([]string, 0, len(changes))
	proposedTotalDiff := ""
	for _, change := range changes {
		proposedTotalDiff += change.diff + "\n"
		mutationPaths = append(mutationPaths, change.filePath)
		if change.movePath != "" {
			mutationPaths = append(mutationPaths, change.movePath)
		}
		permissionPath, relErr := filepath.Rel(r.worktree(), change.filePath)
		if relErr != nil {
			permissionPath = change.filePath
		}
		permissionPaths = append(permissionPaths, filepath.ToSlash(permissionPath))
		target := change.filePath
		if change.movePath != "" {
			target = change.movePath
		}
		relative, err := filepath.Rel(r.workDir, target)
		if err != nil {
			relative = target
		}
		permissionFiles = append(permissionFiles, applyPatchFileMetadata{
			FilePath:     change.filePath,
			RelativePath: filepath.ToSlash(relative),
			Type:         change.kind,
			Patch:        change.diff,
			Additions:    change.additions,
			Deletions:    change.deletions,
			MovePath:     change.movePath,
		})
	}
	metadata := map[string]any{
		"filepath": strings.Join(permissionPaths, ", "),
		"diff":     proposedTotalDiff,
		"files":    permissionFiles,
	}
	if err := r.ask(ctx, call, "edit", permissionPaths, metadata); err != nil {
		return steploop.ToolResult{}, err
	}

	for index := range changes {
		change := &changes[index]
		if err := ctx.Err(); err != nil {
			return steploop.ToolResult{}, err
		}
		switch change.kind {
		case "add", "update":
			if err := os.MkdirAll(filepath.Dir(change.filePath), 0o755); err != nil {
				return steploop.ToolResult{}, err
			}
			if err := os.WriteFile(change.filePath, []byte(joinBOM(change.newContent, change.bom)), 0o644); err != nil {
				return steploop.ToolResult{}, err
			}
			change.newContent, err = formatMutationFile(ctx, formatter, change.filePath, change.bom)
			if err != nil {
				return steploop.ToolResult{}, err
			}
		case "move":
			if err := os.MkdirAll(filepath.Dir(change.movePath), 0o755); err != nil {
				return steploop.ToolResult{}, err
			}
			if err := os.WriteFile(change.movePath, []byte(joinBOM(change.newContent, change.bom)), 0o644); err != nil {
				return steploop.ToolResult{}, err
			}
			if err := os.Remove(change.filePath); err != nil {
				return steploop.ToolResult{}, err
			}
			change.newContent, err = formatMutationFile(ctx, formatter, change.movePath, change.bom)
			if err != nil {
				return steploop.ToolResult{}, err
			}
		case "delete":
			if err := os.Remove(change.filePath); err != nil {
				return steploop.ToolResult{}, err
			}
		}
	}

	totalDiff := ""
	files := make([]applyPatchFileMetadata, 0, len(changes))
	for index := range changes {
		change := &changes[index]
		target := change.filePath
		if change.movePath != "" {
			target = change.movePath
		}
		change.diff = TrimDiff(patchpkg.GenerateTwoFilesPatch(target, change.oldContent, change.newContent))
		change.additions, change.deletions = lineChangeCounts(change.oldContent, change.newContent)
		totalDiff += change.diff + "\n"
		relative, relErr := filepath.Rel(r.workDir, target)
		if relErr != nil {
			relative = target
		}
		files = append(files, applyPatchFileMetadata{
			FilePath:     change.filePath,
			RelativePath: filepath.ToSlash(relative),
			Type:         change.kind,
			Patch:        change.diff,
			Additions:    change.additions,
			Deletions:    change.deletions,
			MovePath:     change.movePath,
		})
	}

	summary := make([]string, 0, len(changes))
	for _, change := range changes {
		target := change.filePath
		prefix := "M "
		if change.kind == "add" {
			prefix = "A "
		}
		if change.kind == "delete" {
			prefix = "D "
		}
		if change.movePath != "" {
			target = change.movePath
		}
		relative, err := filepath.Rel(r.workDir, target)
		if err != nil {
			relative = target
		}
		summary = append(summary, prefix+filepath.ToSlash(relative))
	}
	output := "Success. Updated the following files:\n" + strings.Join(summary, "\n")
	return steploop.ToolResult{
		Title:  output,
		Output: output,
		Metadata: rawMetadata(applyPatchMetadata{
			Diff:        totalDiff,
			Files:       files,
			Diagnostics: map[string]any{},
		}),
	}, nil
}
