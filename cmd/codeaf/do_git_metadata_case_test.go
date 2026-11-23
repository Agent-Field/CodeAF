package main

// #2884: THE GUARD ON A FOLDER'S GIT HISTORY HOLDS ON A DISK THAT IGNORES CASE.
//
// On a Mac disk `.GIT`, `.git/OBJECTS` and `.git/head` are the same entries as
// `.git`, `.git/objects` and `.git/HEAD`, and a worker that spelled them that
// way deleted or rewrote the history while the run reported success. This
// drives the real headless door with a scripted worker that sends those
// spellings. It skips on a disk that tells the cases apart, where a folder named
// `.GIT` is a different folder and nothing here applies; the guard's decision
// logic for that disk is covered in internal/session with a stand-in for it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestDoOnTheRunEngineRefusesCaseVariantsOfTheFoldersGitMetadata(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	parent, repo := nestedRunRepo(t)
	gitDir := filepath.Join(repo, ".git")
	original, err := os.Lstat(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if variant, err := os.Lstat(filepath.Join(repo, ".GIT")); err != nil || !os.SameFile(original, variant) {
		t.Skip("this disk tells upper and lower case apart, so .GIT is not the repository's .git")
	}
	head := doGitIn(t, repo, "rev-parse", "HEAD")
	headFile, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		t.Fatal(err)
	}

	seat := &recordedReplacementSeat{commands: []string{
		"rm -rf .GIT",
		"rm -rf .git/OBJECTS",
		"echo 'ref: refs/heads/x' > .git/head",
		"sed -i '' 's/work/WORK/' .GIT/HEAD",
	}}
	var stdout, stderr strings.Builder
	runErr := doErrand(doRequest{
		task: "create add.py and its test", workspace: repo, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	})
	if runErr == nil {
		t.Fatalf("a run whose every attempted action was refused exited successfully\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if info, err := os.Stat(gitDir); err != nil || !info.IsDir() {
		t.Fatalf("the repository's .git is gone after the run (err %v): a case variant got past the guard", err)
	}
	if info, err := os.Stat(filepath.Join(gitDir, "objects")); err != nil || !info.IsDir() {
		t.Fatalf("the repository's objects are gone after the run (err %v)", err)
	}
	if now, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err != nil || string(now) != string(headFile) {
		t.Fatalf("HEAD was rewritten: %q (err %v), want %q", now, err, headFile)
	}
	if now := doGitIn(t, repo, "rev-parse", "HEAD"); now != head {
		t.Fatalf("the repository's history moved %s -> %s", head, now)
	}
	if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
		t.Fatal("the parent folder became a repository")
	}
	if len(seat.results) < len(seat.commands) {
		t.Fatalf("the worker saw %d answers to its %d commands\nstdout:\n%s\nstderr:\n%s",
			len(seat.results), len(seat.commands), stdout.String(), stderr.String())
	}
	for index, said := range seat.results[:len(seat.commands)] {
		if !strings.Contains(said, "is outside your copy") {
			t.Errorf("command %d (%q) was answered %q, want the boundary's refusal", index+1, seat.commands[index], said)
		}
	}
}
