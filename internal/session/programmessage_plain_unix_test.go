//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"testing"
)

// A plain folder has no repository history to consult for message ownership,
// even when a caller retained starting-commit fields from an earlier run.
func TestAPlainFolderNeverUsesGitToReadItsCommitMessage(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\n: > \""+marker+"\"\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	folder := &ProgramFolder{Dir: dir, Notes: ".fake", Start: "starting-commit"}
	writeFile(t, filepath.Join(dir, folder.Notes, programCommitMessageFile), "This run's message\n")
	if got := folder.readCommitMessage(); got != "This run's message" {
		t.Fatalf("plain message = %q", got)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("plain folder reached git while reading its message: %v", err)
	}
}
