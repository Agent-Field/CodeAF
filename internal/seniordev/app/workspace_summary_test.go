//go:build !windows

package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The finishing measurement counts new work as well as tracked edits, while
// the program's private notes and generated files stay out of its change.
func TestSummaryMeasuresTheTreeTheRunWouldSubmit(t *testing.T) {
	workspace, base := guardWorkspace(t)
	recorder := newGitRecorder(workspace, func(string) {})
	for path, body := range map[string]string{
		"README.md": "base\nedit\n", "new.txt": "one\ntwo\nthree\nfour\nfive\n", ".senior-dev/note": "private\n",
	} {
		if err := writeFile(filepath.Join(workspace, path), body); err != nil {
			t.Fatal(err)
		}
	}
	data, status := recorder.Summary(context.Background(), base)
	if status != "completed" || data["files"] != 2 || data["additions"] != int64(6) || data["deletions"] != int64(0) || data["binary_files"] != 0 {
		t.Fatalf("summary lost untracked work or counted notes: %s, %#v", status, data)
	}
	tree, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	patch, err := recorder.git("diff", "--binary", "--no-renames", base, tree, "--")
	if err != nil {
		t.Fatal(err)
	}
	if data["patch_bytes"] != int64(len(patch)+1) {
		t.Fatalf("patch bytes = %v, want %d", data["patch_bytes"], len(patch)+1)
	}
	// This is an observational count of git's listing, including the notes.
	if data["untracked_files"] != 2 {
		t.Fatalf("untracked count changed meaning: %#v", data)
	}
}

func TestSummaryCountsANewBinaryAndUsesWorkingDiffIfSnapshotFails(t *testing.T) {
	workspace, base := guardWorkspace(t)
	recorder := newGitRecorder(workspace, func(string) {})
	if err := writeFile(filepath.Join(workspace, "image.bin"), "\x00binary\n"); err != nil {
		t.Fatal(err)
	}
	data, status := recorder.Summary(context.Background(), base)
	if status != "completed" || data["files"] != 1 || data["binary_files"] != 1 || data["additions"] != int64(0) || data["patch_bytes"].(int64) <= 0 {
		t.Fatalf("new binary is missing from summary: %s, %#v", status, data)
	}
	t.Setenv("SENIOR_DEV_IGNORED_AT_START", filepath.Join(t.TempDir(), "missing"))
	if err := writeFile(filepath.Join(workspace, "README.md"), "base\nedit\n"); err != nil {
		t.Fatal(err)
	}
	data, status = recorder.Summary(context.Background(), base)
	if status != "completed" || data["files"] != 1 || data["additions"] != int64(1) || data["binary_files"] != 0 {
		t.Fatalf("snapshot failure lost the working-tree fallback: %s, %#v", status, data)
	}
	if head := gitOutput(context.Background(), workspace, "rev-parse", "HEAD"); strings.TrimSpace(head) != base {
		t.Fatal("measurement moved HEAD")
	}
}
