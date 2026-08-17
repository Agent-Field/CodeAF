package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ownedPlace is one owned session folder under a fake HOME, which is how these
// tests keep a git init off the machine they run on: nothing below reads or
// writes anything outside the temp directory.
func ownedPlace(t *testing.T) session.Place {
	t.Helper()
	fake := t.TempDir()
	t.Setenv("HOME", fake)
	t.Setenv(home.EnvVar, filepath.Join(fake, ".aforge"))
	dir := home.Join("v3", "projects", "owned", "0123456789abcdef")
	place := session.Place{Dir: dir, Owned: true}
	place.Workspace = place.Work()
	return place
}

// gitHere answers whether this machine has a git worth testing against. A
// machine without one is not a failing build — it is the very case
// [prepareOwnedWorkspace] is written to survive, and the test below covers it
// deliberately instead.
func gitHere(t *testing.T) bool {
	t.Helper()
	_, err := exec.LookPath("git")
	return err == nil
}

// AN OWNED WORKSPACE IS A REPOSITORY WITH A HEAD. The commit is what task
// worktrees branch from, so a directory that was init-ed and never committed
// would look isolated and behave exactly like a directory that was not.
func TestAnOwnedWorkspaceOpensAsARepositoryWithACommit(t *testing.T) {
	if !gitHere(t) {
		t.Skip("no git on this machine")
	}
	place := ownedPlace(t)
	if err := prepareOwnedWorkspace(place); err != nil {
		t.Fatalf("prepareOwnedWorkspace: %v", err)
	}

	info, err := os.Stat(place.Work())
	if err != nil || !info.IsDir() {
		t.Fatalf("work/ = %v, %v; want a directory", info, err)
	}
	head, err := gitIn(place.Work(), "rev-parse", "--verify", "HEAD")
	if err != nil {
		t.Fatalf("the owned workspace has no HEAD to branch from: %v", err)
	}
	if strings.TrimSpace(head) == "" {
		t.Fatal("HEAD resolved to nothing")
	}
	// The commit says what happened and is signed by the harness rather than by
	// whoever happens to be sitting at the machine: a session opening must not
	// write the person's name into a history they did not make.
	subject, err := gitIn(place.Work(), "log", "-1", "--format=%s|%an|%ae")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if got, want := strings.TrimSpace(subject), "session opened|aforge|aforge@localhost"; got != want {
		t.Fatalf("first commit = %q, want %q", got, want)
	}
}

// It is SILENT and it is IDEMPOTENT: a resumed owned session finds its
// repository already there and adds nothing to a history the person has since
// made their own.
func TestReopeningAnOwnedWorkspaceAddsNoCommit(t *testing.T) {
	if !gitHere(t) {
		t.Skip("no git on this machine")
	}
	place := ownedPlace(t)
	if err := prepareOwnedWorkspace(place); err != nil {
		t.Fatalf("prepareOwnedWorkspace: %v", err)
	}
	if err := prepareOwnedWorkspace(place); err != nil {
		t.Fatalf("second prepareOwnedWorkspace: %v", err)
	}
	count, err := gitIn(place.Work(), "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("git rev-list: %v", err)
	}
	if got := strings.TrimSpace(count); got != "1" {
		t.Fatalf("commits after two opens = %s, want 1", got)
	}
}

// A BORROWED SESSION HAS NO work/ AT ALL, so there is nothing here to make and
// nothing to say about it. The person's repository is not touched.
func TestABorrowedSessionMakesNoWorkspace(t *testing.T) {
	fake := t.TempDir()
	t.Setenv("HOME", fake)
	t.Setenv(home.EnvVar, filepath.Join(fake, ".aforge"))
	place := session.Place{Dir: home.Join("v3", "projects", "repo", "0123456789abcdef")}
	if err := prepareOwnedWorkspace(place); err != nil {
		t.Fatalf("prepareOwnedWorkspace: %v", err)
	}
	if _, err := os.Stat(place.Dir); !os.IsNotExist(err) {
		t.Fatalf("a borrowed session made a folder on the way in: %v", err)
	}
}

// NO GIT IS NOT A FAILED LAUNCH. The session runs without isolation — which is
// what this mode had before the repository existed — rather than refusing to
// open, because pretending to isolate is worse than not isolating and refusing
// to talk is worse than both.
func TestAWorkspaceWithoutGitStillOpens(t *testing.T) {
	place := ownedPlace(t)
	// An empty PATH is a machine with no git on it, which is the one thing the
	// non-fatal branch has to survive.
	t.Setenv("PATH", "")
	if err := prepareOwnedWorkspace(place); err != nil {
		t.Fatalf("a missing git failed the launch: %v", err)
	}
	if info, err := os.Stat(place.Work()); err != nil || !info.IsDir() {
		t.Fatalf("work/ = %v, %v; want the workspace to exist anyway", info, err)
	}
	if _, err := os.Stat(filepath.Join(place.Work(), ".git")); !os.IsNotExist(err) {
		t.Fatalf("a repository appeared without a git to make it: %v", err)
	}
}

// The index every deliverable is cited in is ONE path, resolved through
// internal/home so AFORGE_HOME moves it with the rest of the state root.
func TestTheArtifactsIndexLivesUnderTheStateRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	if got, want := artifactsIndexPath(), filepath.Join(root, "v3", "artifacts.jsonl"); got != want {
		t.Fatalf("artifacts index = %q, want %q", got, want)
	}
}
