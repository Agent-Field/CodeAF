package factory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repoWithCommit is a temp repository on main with one commit.
func repoWithCommit(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	dir := t.TempDir()
	sh(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "add", "README")
	sh(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func habitWriter(sentence string) func(string) error {
	return func(d string) error { return BankRecipeHabit(d, sentence) }
}

// fakeGh puts a gh on PATH that records its args and prints a pull request URL.
func fakeGh(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "args.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + log + "'\necho https://github.com/acme/web/pull/42\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func noGh(t *testing.T) {
	t.Helper()
	// A PATH with git but no gh: a dir of one symlink to git.
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestRecipeChangeNotAGitRepositoryWritesTheFile(t *testing.T) {
	dir := t.TempDir()
	note, err := BankRecipeChange(context.Background(), dir, "ship on green", "", "- ship on green", habitWriter("ship on green"))
	if err != nil || note != RecipeNotePlain {
		t.Fatalf("note = %q, err = %v", note, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, RecipeFile)); !strings.Contains(string(data), "- ship on green") {
		t.Fatalf("file:\n%s", data)
	}
}

func TestRecipeChangeNoRemoteCommitsOnABranchAndLeavesTheCheckoutAlone(t *testing.T) {
	noGh(t)
	dir := repoWithCommit(t)
	// A dirty tree: an edit to a tracked file and an untracked file.
	os.WriteFile(filepath.Join(dir, "README"), []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("mine\n"), 0o644)
	before := sh(t, dir, "status", "--porcelain")

	note, err := BankRecipeChange(context.Background(), dir, "Security review on auth", "auth is risky", "- Security review on auth", habitWriter("Security review on auth"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "written · committed on factory/recipe-security · no remote to push to"; note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	if after := sh(t, dir, "status", "--porcelain"); after != before {
		t.Fatalf("working tree changed:\n%s\nvs\n%s", before, after)
	}
	if got := sh(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("checkout moved to %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, RecipeFile)); err == nil {
		t.Fatal("the checkout's own file was written")
	}
	if msg := sh(t, dir, "log", "-1", "--format=%B", "factory/recipe-security"); msg != "recipe: Security review on auth" {
		t.Fatalf("message = %q", msg)
	}
	if show := sh(t, dir, "show", "factory/recipe-security:"+RecipeFile); !strings.Contains(show, "- Security review on auth") {
		t.Fatalf("branch file:\n%s", show)
	}
	if wts := sh(t, dir, "worktree", "list", "--porcelain"); strings.Count(wts, "worktree ") != 1 {
		t.Fatalf("worktree left behind:\n%s", wts)
	}
	// A second line with the same first word takes a suffix.
	note, err = BankRecipeChange(context.Background(), dir, "Security: nothing in prod on friday", "", "- x", habitWriter("Security: nothing in prod on friday"))
	if err != nil || !strings.Contains(note, "factory/recipe-security-2") {
		t.Fatalf("second note = %q, err = %v", note, err)
	}
}

func TestRecipeChangeWithARemoteButNoGhPushesTheBranch(t *testing.T) {
	noGh(t)
	dir := repoWithCommit(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	sh(t, dir, "init", "-q", "--bare", "-b", "main", bare)
	sh(t, dir, "remote", "add", "origin", bare)
	sh(t, dir, "push", "-q", "origin", "main")
	note, err := BankRecipeChange(context.Background(), dir, "Security review", "", "- Security review", habitWriter("Security review"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "written · branch factory/recipe-security pushed · open the pull request when you want"; note != want {
		t.Fatalf("note = %q", note)
	}
	if out := sh(t, bare, "branch", "--list", "factory/recipe-security"); !strings.Contains(out, "factory/recipe-security") {
		t.Fatalf("remote lacks the branch: %q", out)
	}
}

func TestRecipeChangeWithGhOpensAPullRequest(t *testing.T) {
	dir := repoWithCommit(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	sh(t, dir, "init", "-q", "--bare", "-b", "main", bare)
	sh(t, dir, "remote", "add", "origin", bare)
	sh(t, dir, "push", "-q", "origin", "main")
	log := fakeGh(t)
	note, err := BankRecipeChange(context.Background(), dir, "Security review on auth", "auth is risky", "- Security review on auth", habitWriter("Security review on auth"))
	if err != nil {
		t.Fatal(err)
	}
	if note != fmt.Sprintf(RecipeNotePR, 42) || note != "written · pull request #42 opened for the team" {
		t.Fatalf("note = %q", note)
	}
	args, _ := os.ReadFile(log)
	for _, want := range []string{"pr\ncreate", "--title\nSecurity review on auth", "--head\nfactory/recipe-security", "--base\nmain", "auth is risky", "- Security review on auth"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("gh args lack %q:\n%s", want, args)
		}
	}
}

func TestRecipeChangeALineAlreadyThereChangesNothing(t *testing.T) {
	noGh(t)
	dir := repoWithCommit(t)
	if err := BankRecipeHabit(dir, "ship on green"); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "add", RecipeFile)
	sh(t, dir, "commit", "-q", "-m", "recipe")
	note, err := BankRecipeChange(context.Background(), dir, "ship on green", "", "- ship on green", habitWriter("ship on green"))
	if err != nil || note != RecipeNoteHad {
		t.Fatalf("note = %q, err = %v", note, err)
	}
	if out := sh(t, dir, "branch", "--list", "factory/recipe-*"); out != "" {
		t.Fatalf("a branch was left: %q", out)
	}
}
