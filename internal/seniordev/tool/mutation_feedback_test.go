//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	formatpkg "github.com/Agent-Field/codeaf/internal/seniordev/format"
)

func TestEditDiffReflectsPostFormatContent(t *testing.T) {
	// edit reports a unified diff of the actual post-format change.
	workDir := t.TempDir()
	path := filepath.Join(workDir, "file.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := New(workDir)
	extensions := []string{".txt"}
	command := []string{"test-formatter", "$FILE"}
	registry.formatters.services[workDir+"\x00"+workDir] = formatpkg.NewService(
		formatpkg.Context{Directory: workDir, Worktree: workDir},
		formatpkg.Configuration{Enabled: true, Overrides: []formatpkg.FormatterOverride{{
			Key: "test", Extensions: &extensions, Command: &command,
		}}},
		formatpkg.Dependencies{},
		func(_ context.Context, command []string, _ string, _ map[string]string) (int, error) {
			return 0, os.WriteFile(command[1], []byte("formatted\n"), 0o644)
		},
	)
	result, err := execute(t, registry, "edit", map[string]any{
		"filePath": "file.txt", "oldString": "before", "newString": "raw",
	})
	if err != nil {
		t.Fatal(err)
	}
	var metadata editMetadata
	if err := json.Unmarshal(result.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Diff == "" || metadata.FileDiff.Patch != metadata.Diff {
		t.Fatalf("metadata = %#v", metadata)
	}
	if !strings.Contains(metadata.Diff, "+formatted") || strings.Contains(metadata.Diff, "+raw") {
		t.Fatalf("diff does not reflect formatter output: %q", metadata.Diff)
	}
	assertTestFile(t, workDir, "file.txt", "formatted\n")
}

func TestApplyPatchReportsPerFileAndAggregateDiffs(t *testing.T) {
	// apply_patch reports unified diffs for every actual change.
	workDir := t.TempDir()
	writeTestFile(t, workDir, "update.txt", "old\n")
	result, err := execute(t, New(workDir), "apply_patch", map[string]any{"patchText": `*** Begin Patch
*** Add File: added.txt
+new
*** Update File: update.txt
@@
-old
+updated
*** End Patch`})
	if err != nil {
		t.Fatal(err)
	}
	var metadata applyPatchMetadata
	if err := json.Unmarshal(result.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Diff == "" || len(metadata.Files) != 2 {
		t.Fatalf("metadata = %#v", metadata)
	}
	for _, file := range metadata.Files {
		if file.Patch == "" || !strings.Contains(metadata.Diff, file.Patch) {
			t.Fatalf("file diff missing from aggregate: %#v", file)
		}
	}
}
