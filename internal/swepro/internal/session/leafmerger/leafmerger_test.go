package leafmerger

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type gitFixture struct {
	repo     string
	worktree string
}

func runGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, cwd, err, out)
	}
	return string(out)
}

func newGitFixture(t *testing.T) gitFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "leaf")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "test")
	runGit(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "shared.txt")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "worktree", "add", "-b", "plandb/task-1", worktree)
	return gitFixture{repo: repo, worktree: worktree}
}

func defaultInput(f gitFixture) Input {
	return Input{
		Workspace: f.repo, Worktree: f.worktree, MergeAt: f.repo,
		TaskID: "task-1", TaskTitle: "land the leaf",
		TargetBranch: "main", StructuralMergeSet: true,
	}
}

func TestLeafMergerCleanRealGit(t *testing.T) {
	f := newGitFixture(t)
	if err := os.WriteFile(filepath.Join(f.worktree, "leaf.txt"), []byte("leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := New(defaultInput(f), nil).Run()
	if err != nil {
		t.Fatal(err)
	}
	if outcome != (Outcome{
		OK: true, Summary: "merged plandb/task-1 into main via --no-ff",
	}) {
		t.Fatalf("outcome = %#v", outcome)
	}
	if content, err := os.ReadFile(filepath.Join(f.repo, "leaf.txt")); err != nil || string(content) != "leaf\n" {
		t.Fatalf("merged leaf.txt content=%q err=%v", content, err)
	}
	if got := strings.TrimSpace(runGit(t, f.repo, "rev-list", "--parents", "-n", "1", "HEAD")); len(strings.Fields(got)) != 3 {
		t.Fatalf("HEAD is not a two-parent merge commit: %s", got)
	}
	if _, err := os.Stat(f.worktree); !os.IsNotExist(err) {
		t.Fatalf("leaf worktree still exists: %v", err)
	}
	branches := runGit(t, f.repo, "branch", "--list", "plandb/task-1")
	if strings.TrimSpace(branches) != "" {
		t.Fatalf("leaf branch still exists: %q", branches)
	}
}

func TestLeafMergerCommitsCarrySWEAFAttribution(t *testing.T) {
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "")
	f := newGitFixture(t)
	if err := os.WriteFile(filepath.Join(f.worktree, "leaf.txt"), []byte("leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := New(defaultInput(f), nil).Run()
	if err != nil || !outcome.OK {
		t.Fatalf("outcome = %#v err = %v", outcome, err)
	}
	for ref, label := range map[string]string{"HEAD^2": "leaf commit", "HEAD": "merge commit"} {
		body := runGit(t, f.repo, "log", "--format=%B", "-n", "1", ref)
		if !strings.Contains(body, "Co-Authored-By: SWE AF <noreply@agentfield.ai>") ||
			!strings.Contains(body, "https://agentfield.ai") {
			t.Errorf("%s lacks SWE AF attribution:\n%s", label, body)
		}
	}
}

func TestLeafMergerRebaseConflictResolvedWithRealGit(t *testing.T) {
	f := newGitFixture(t)
	if err := os.WriteFile(filepath.Join(f.repo, "shared.txt"), []byte("target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, f.repo, "add", "shared.txt")
	runGit(t, f.repo, "commit", "-m", "target changes")
	if err := os.WriteFile(filepath.Join(f.worktree, "shared.txt"), []byte("leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := defaultInput(f)
	var calls []ConflictArgs
	input.ResolveConflicts = func(args ConflictArgs) (ConflictResult, error) {
		calls = append(calls, args)
		if err := os.WriteFile(filepath.Join(args.Worktree, "shared.txt"), []byte("target + leaf\n"), 0o644); err != nil {
			return ConflictResult{}, err
		}
		return ConflictResult{OK: true, Summary: "combined both edits"}, nil
	}
	outcome, err := New(input, nil).Run()
	if err != nil {
		t.Fatal(err)
	}
	wantSummary := "merged plandb/task-1 into main via --no-ff " +
		"(LLM resolver fixed 1 rebase conflict(s): combined both edits)"
	if !outcome.OK || outcome.Summary != wantSummary {
		t.Fatalf("outcome = %#v", outcome)
	}
	if len(calls) != 1 ||
		!reflect.DeepEqual(calls[0].ConflictedFiles, []string{"shared.txt"}) ||
		calls[0].Worktree != f.worktree {
		t.Fatalf("resolver calls = %#v", calls)
	}
	content, err := os.ReadFile(filepath.Join(f.repo, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "target + leaf\n" {
		t.Fatalf("merged content = %q", content)
	}
}

func TestLeafMergerConflictWritesByteExactReportAndLeavesWorktree(t *testing.T) {
	f := newGitFixture(t)
	if err := os.WriteFile(filepath.Join(f.repo, "shared.txt"), []byte("target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, f.repo, "add", "shared.txt")
	runGit(t, f.repo, "commit", "-m", "target changes")
	if err := os.WriteFile(filepath.Join(f.worktree, "shared.txt"), []byte("leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := New(defaultInput(f), nil).Run()
	if err != nil {
		t.Fatal(err)
	}
	if outcome.OK ||
		outcome.Summary != "rebase onto main produced conflicts; worktree left at "+f.worktree {
		t.Fatalf("outcome = %#v", outcome)
	}
	wantPath := filepath.Join(f.repo, ".plandb", "conflicts", "task-1.md")
	if outcome.ConflictPath != wantPath {
		t.Fatalf("conflict path = %q, want %q", outcome.ConflictPath, wantPath)
	}
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Merge conflict: task-1\n\nStage: `rebase`",
		"Target branch: `main`",
		"Worktree: `.plandb/wt-task-1`",
		"git -C " + f.repo + " merge --no-ff plandb/task-1",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("report missing %q:\n%s", want, body)
		}
	}
	if _, err := os.Stat(f.worktree); err != nil {
		t.Fatalf("worktree was not left intact: %v", err)
	}
	if got := runGit(t, f.worktree, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Fatalf("rebase abort did not restore clean leaf: %q", got)
	}
}

type scriptStep struct {
	argv   []string
	cwd    string
	env    map[string]string
	result Result
	err    error
}

type scriptRunner struct {
	t     *testing.T
	steps []scriptStep
	next  int
}

func (s *scriptRunner) Run(argv []string, opts RunOptions) (Result, error) {
	s.t.Helper()
	if s.next >= len(s.steps) {
		s.t.Fatalf("unexpected command: %#v cwd=%q env=%#v", argv, opts.CWD, opts.Env)
	}
	step := s.steps[s.next]
	s.next++
	if !reflect.DeepEqual(argv, step.argv) ||
		opts.CWD != step.cwd ||
		!reflect.DeepEqual(opts.Env, step.env) {
		s.t.Fatalf(
			"step %d:\n got argv=%#v cwd=%q env=%#v\nwant argv=%#v cwd=%q env=%#v",
			s.next, argv, opts.CWD, opts.Env, step.argv, step.cwd, step.env,
		)
	}
	return step.result, step.err
}

func TestStageCommitFailureAndPhantomBranches(t *testing.T) {
	t.Setenv("AGENTFIELD_COMMIT_ATTRIBUTION", "0")
	input := Input{
		Workspace: "/repo", Worktree: "/leaf", MergeAt: "/repo",
		TaskID: "7", TaskTitle: strings.Repeat("x", 90), TargetBranch: "main",
	}
	prefix := []scriptStep{
		{argv: []string{"git", "update-index", "--refresh", "--again"}, cwd: "/leaf"},
		{argv: []string{"git", "add", "-A"}, cwd: "/leaf"},
		{
			argv: []string{"git", "status", "--porcelain"}, cwd: "/leaf",
			result: Result{Stdout: []byte(" M x\n")},
		},
	}
	commitArgv := gitWithCommitter(
		"commit", "-m", "leaf 7: "+strings.Repeat("x", 77)+"...",
	)

	t.Run("real failure combines stderr then stdout and slices", func(t *testing.T) {
		long := strings.Repeat("z", 400)
		runner := &scriptRunner{t: t, steps: append(prefix, scriptStep{
			argv: commitArgv, cwd: "/leaf",
			result: Result{Code: 1, Stdout: []byte("stdout"), Stderr: []byte(long)},
		})}
		outcome, err := New(input, runner).Run()
		if err != nil {
			t.Fatal(err)
		}
		want := "failed to commit leaf work: " + strings.Repeat("z", 300)
		if outcome != (Outcome{OK: false, Summary: want}) {
			t.Fatalf("outcome = %#v", outcome)
		}
	})

	t.Run("phantom resets then empty branch cleans", func(t *testing.T) {
		steps := append(append([]scriptStep{}, prefix...),
			scriptStep{
				argv: commitArgv, cwd: "/leaf",
				result: Result{Code: 1, Stdout: []byte("nothing to commit")},
			},
			scriptStep{argv: []string{"git", "reset", "--hard", "HEAD"}, cwd: "/leaf"},
			scriptStep{
				argv: []string{"git", "diff", "--quiet", "main...HEAD"}, cwd: "/leaf",
			},
			scriptStep{
				argv: []string{"git", "worktree", "remove", "--force", "/leaf"}, cwd: "/repo",
			},
			scriptStep{argv: []string{"git", "branch", "-D", "plandb/7"}, cwd: "/repo"},
		)
		runner := &scriptRunner{t: t, steps: steps}
		outcome, err := New(input, runner).Run()
		if err != nil {
			t.Fatal(err)
		}
		if outcome != (Outcome{
			OK: true, Summary: "no changes vs target; worktree cleaned up",
		}) {
			t.Fatalf("outcome = %#v", outcome)
		}
		if runner.next != len(runner.steps) {
			t.Fatalf("used %d/%d steps", runner.next, len(runner.steps))
		}
	})
}

func TestRunnerRejectionPropagates(t *testing.T) {
	want := fmt.Errorf("runner rejected")
	runner := RunnerFunc(func([]string, RunOptions) (Result, error) {
		return Result{}, want
	})
	_, got := New(Input{}, runner).Run()
	if got != want {
		t.Fatalf("error = %v, want identity %v", got, want)
	}
}
