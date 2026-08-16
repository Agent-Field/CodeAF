package util

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func gitTestRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitTestRun(t, dir, "init", "-q")
	gitTestRun(t, dir, "config", "user.email", "test@example.com")
	gitTestRun(t, dir, "config", "user.name", "Test")
	return dir
}

func TestEnsureCodeafExcludedIdempotent(t *testing.T) {
	dir := initGitRepo(t)
	ok, err := EnsureCodeafExcluded(context.Background(), dir)
	if err != nil || !ok {
		t.Fatalf("ensure = %v, %v", ok, err)
	}
	ok, err = EnsureCodeafExcluded(context.Background(), dir)
	if err != nil || !ok {
		t.Fatalf("second ensure = %v, %v", ok, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, excludeSentinel) != 1 {
		t.Fatalf("exclude:\n%s", text)
	}
	for _, path := range ExcludedPaths {
		if strings.Count(text, path+"\n") != 1 {
			t.Fatalf("%q count in:\n%s", path, text)
		}
	}
}

// D7, held from the inside: the exclusions must land in the file git actually
// reads, and in a LINKED WORKTREE that is not the one `--git-dir` names.
//
// A worktree has a private gitdir of its own, and git consults the COMMON
// directory's info/exclude for exclusions. Written to the private path this
// function did its whole job and reported success into a file nothing ever
// opens — silently, on the layout aforge runs every isolated coding leaf in.
// The assertion is `git status`, not the file: what matters is whether git
// ignores the engine's state, and only git can answer that.
func TestEnsureCodeafExcludedReachesTheFileGitReadsInAWorktree(t *testing.T) {
	root := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(root, "work.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", "-A")
	gitTestRun(t, root, "commit", "-q", "--no-verify", "-m", "the work as it stands")

	linked := filepath.Join(t.TempDir(), "view")
	gitTestRun(t, root, "worktree", "add", "-q", "-b", "leaf", linked)

	ok, err := EnsureCodeafExcluded(context.Background(), linked)
	if err != nil || !ok {
		t.Fatalf("ensure = %v, %v", ok, err)
	}
	if err := os.MkdirAll(filepath.Join(linked, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".codeaf", "contract.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".plandb.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status := gitTestRun(t, linked, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Fatalf("git still sees the engine's own state in a worktree:\n%s", status)
	}
}

func TestSuppressCaseCollisions(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "A.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, dir, "add", "A.txt", "a.txt")
	result := SuppressCaseCollisions(context.Background(), dir)
	if !reflect.DeepEqual(result.Collided, []string{"A.txt", "a.txt"}) ||
		!reflect.DeepEqual(result.Suppressed, result.Collided) {
		t.Fatalf("collision: %+v", result)
	}
	list := gitTestRun(t, dir, "ls-files", "-v")
	if !strings.Contains(list, "S A.txt") || !strings.Contains(list, "S a.txt") {
		t.Fatalf("skip-worktree flags:\n%s", list)
	}
}

func TestFingerprintDiffAndEagerCommit(t *testing.T) {
	dir := initGitRepo(t)
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, dir, "add", "file.txt")
	gitTestRun(t, dir, "commit", "-qm", "initial")
	if err := os.WriteFile(file, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := FingerprintDiff(context.Background(), dir, "HEAD")
	second := FingerprintDiff(context.Background(), dir, "HEAD")
	if !first.HasChanges || first.DiffLines == 0 || !IsSameFingerprint(first, second) {
		t.Fatalf("fingerprints: %+v %+v", first, second)
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	afterBudget := FingerprintDiff(expired, dir, "HEAD")
	if afterBudget.SHA256 != first.SHA256 || afterBudget.DiffLines != first.DiffLines {
		t.Fatalf("expired fingerprint = %+v, want SHA256/DiffLines from %+v", afterBudget, first)
	}
	empty := FingerprintDiff(context.Background(), dir, "missing-ref")
	if empty.HasChanges || empty.SHA256 != hashText("") {
		t.Fatalf("bad ref: %+v", empty)
	}

	previous := skipEagerCommit
	skipEagerCommit = false
	defer func() { skipEagerCommit = previous }()
	EagerCommit(context.Background(), EagerCommitOptions{Cwd: dir, FilePath: file, Label: "write"})
	subject := strings.TrimSpace(gitTestRun(t, dir, "log", "-1", "--pretty=%s"))
	if subject != "wip(write): file.txt" {
		t.Fatalf("subject = %q", subject)
	}
}
