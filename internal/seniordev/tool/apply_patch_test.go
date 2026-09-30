//go:build !windows

package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPatchAddUpdateMoveDelete(t *testing.T) {
	workDir := t.TempDir()
	writeTestFile(t, workDir, "update.txt", "one\ntwo\n")
	writeTestFile(t, workDir, "move.txt", "\ufeffold\n")
	writeTestFile(t, workDir, "delete.txt", "gone\n")
	patchText := `*** Begin Patch
*** Add File: nested/added.txt
+added
*** Update File: update.txt
@@
-two
+second
*** Update File: move.txt
*** Move to: moved/new.txt
@@
-old
+new
*** Delete File: delete.txt
*** End Patch`
	result, err := execute(t, New(workDir), "apply_patch", map[string]any{"patchText": patchText})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Success. Updated the following files:\n" +
		"A nested/added.txt\n" +
		"M update.txt\n" +
		"M moved/new.txt\n" +
		"D delete.txt"
	if result.Output != want || result.Title != want {
		t.Fatalf("result output = %q", result.Output)
	}
	assertTestFile(t, workDir, "nested/added.txt", "added\n")
	assertTestFile(t, workDir, "update.txt", "one\nsecond\n")
	assertTestFile(t, workDir, "moved/new.txt", "\ufeffnew\n")
	for _, path := range []string{"move.txt", "delete.txt"} {
		if _, err := os.Stat(filepath.Join(workDir, path)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists, err=%v", path, err)
		}
	}
}

func TestApplyPatchVerificationErrors(t *testing.T) {
	workDir := t.TempDir()
	registry := New(workDir)
	cases := []struct {
		name      string
		patchText string
		want      string
	}{
		{"required", "", "patchText is required"},
		{
			"parse",
			"bad",
			"apply_patch verification failed: Error: Invalid patch format: missing Begin/End markers",
		},
		{
			"empty",
			"*** Begin Patch\r\n*** End Patch",
			"patch rejected: empty patch",
		},
		{
			"no hunks",
			"*** Begin Patch\njunk\n*** End Patch",
			"apply_patch verification failed: no hunks found",
		},
		{
			"missing update",
			"*** Begin Patch\n*** Update File: missing.txt\n@@\n-old\n+new\n*** End Patch",
			"apply_patch verification failed: Failed to read file to update: " + filepath.Join(workDir, "missing.txt"),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := execute(t, registry, "apply_patch", map[string]any{"patchText": test.patchText})
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestApplyPatchDescription(t *testing.T) {
	for _, fragment := range []string{
		"*** Begin Patch", "*** End Patch", "*** Add File:", "*** Update File:", "*** Delete File:",
	} {
		if !strings.Contains(applyPatchDescription, fragment) {
			t.Fatalf("embedded description lacks %q", fragment)
		}
	}
}
