package codeaf

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fingerprintRepo is a one-commit repository, which is the state every codeaf
// run starts from.
func fingerprintRepo(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := gitRun(workspace, "init", "-b", "main"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "seed"); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func TestResumeTreeFingerprintIsStableAcrossAPointlessResume(t *testing.T) {
	// The measured cobra#2257 shape: a whole pipeline re-entry that reads,
	// audits and re-verifies without writing a byte. Two readings of the same
	// tree must be equal, or the supervisor can never recognise a fixed point.
	workspace := fingerprintRepo(t)
	ctx := context.Background()
	first, ok := resumeTreeFingerprint(ctx, workspace)
	if !ok || first == "" {
		t.Fatalf("fingerprint = %q, ok = %v", first, ok)
	}
	second, ok := resumeTreeFingerprint(ctx, workspace)
	if !ok || second != first {
		t.Fatalf("second = %q ok=%v, want %q", second, ok, first)
	}
}

func TestResumeTreeFingerprintMovesWithTheDeliverable(t *testing.T) {
	// Every way a resume can advance the work has to read as a change:
	// a new file, an edit to an already-modified file (which leaves the
	// porcelain status line identical), and a commit.
	workspace := fingerprintRepo(t)
	ctx := context.Background()
	seen := map[string]bool{}
	record := func(label string) {
		print, ok := resumeTreeFingerprint(ctx, workspace)
		if !ok {
			t.Fatalf("%s: fingerprint unavailable", label)
		}
		if seen[print] {
			t.Fatalf("%s: fingerprint repeated a previous state", label)
		}
		seen[print] = true
	}
	record("clean")

	if err := os.WriteFile(filepath.Join(workspace, "fix.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "fix.go"); err != nil {
		t.Fatal(err)
	}
	record("new file staged")

	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("seed\nedit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record("tracked file modified")

	// The status line for README.md is unchanged from the previous step; only
	// the bytes moved. `git status --porcelain` alone would call this a stall.
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("seed\nsecond edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record("same status line, different bytes")

	if err := gitRun(workspace, "commit", "-am", "fix"); err != nil {
		t.Fatal(err)
	}
	record("committed")
}

func TestResumeTreeFingerprintDeclinesOutsideAGitRepository(t *testing.T) {
	// No repository, no answer — and the supervisor reads "no answer" as
	// "keep the ported stall policy", never as "nothing changed".
	if _, ok := resumeTreeFingerprint(context.Background(), t.TempDir()); ok {
		t.Fatal("fingerprint reported an answer without a repository")
	}
}
