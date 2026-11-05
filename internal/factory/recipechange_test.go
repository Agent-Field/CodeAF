package factory_test

import (
	"context"
	"fmt"
	. "github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repoWithCommit is a temp repository on main with one commit.
func repoWithCommit(t *testing.T) string {
	t.Helper()
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
	return func(d string) error { return (pipeline.Files{}).BankRule(d, sentence) }
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
	if data, _ := os.ReadFile(filepath.Join(dir, PipelineFile)); !strings.Contains(string(data), "- ship on green") {
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
	if _, err := os.Stat(filepath.Join(dir, PipelineFile)); err == nil {
		t.Fatal("the checkout's own file was written")
	}
	if msg := sh(t, dir, "log", "-1", "--format=%B", "factory/recipe-security"); msg != "recipe: Security review on auth" {
		t.Fatalf("message = %q", msg)
	}
	if show := sh(t, dir, "show", "factory/recipe-security:"+PipelineFile); !strings.Contains(show, "- Security review on auth") {
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
	if err := (pipeline.Files{}).BankRule(dir, "ship on green"); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "add", PipelineFile)
	sh(t, dir, "commit", "-q", "-m", "recipe")
	note, err := BankRecipeChange(context.Background(), dir, "ship on green", "", "- ship on green", habitWriter("ship on green"))
	if err != nil || note != RecipeNoteHad {
		t.Fatalf("note = %q, err = %v", note, err)
	}
	if out := sh(t, dir, "branch", "--list", "factory/recipe-*"); out != "" {
		t.Fatalf("a branch was left: %q", out)
	}
}

func TestRecipeChangeCommitsWithCodeafIdentityWithoutGitIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		// Setenv registers restoration; unset removes even an explicitly empty identity.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	dir := repoWithCommit(t)
	note, err := BankRecipeChange(context.Background(), dir, "ship on green", "", "- ship on green", habitWriter("ship on green"))
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(RecipeNoteLocal, "factory/recipe-ship"); note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	got := sh(t, dir, "log", "-1", "--format=%an <%ae>%n%cn <%ce>", "factory/recipe-ship")
	const want = "codeaf <agentfield-bot@users.noreply.github.com>\ncodeaf <agentfield-bot@users.noreply.github.com>"
	if got != want {
		t.Fatalf("commit identity = %q, want %q", got, want)
	}
}
