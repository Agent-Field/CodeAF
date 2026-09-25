package run_test

// Land reaches the session door from the run side: the working copy's own work
// is committed onto its branch, the answer is written on the root, and the
// repository the run's copy was cut from is not moved. The fixture is one real
// repository in a temp directory with a linked worktree as the run's copy —
// the session landing tests' own helpers are unexported, so the few commands
// they need are spelled here.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// runLandWorkspace builds the person's repository with one committed file, and
// a linked worktree of it as the run's own working copy. The worktree is what
// keeps the two apart: the run commits on its own branch, and the repository
// the person is standing in does not move.
func runLandWorkspace(t *testing.T) (repo, work string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo = t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "checkout", "-b", "work")
	writeRunFile(t, filepath.Join(repo, "first.txt"), "one\n")
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "first")
	work = filepath.Join(t.TempDir(), "run")
	runGit(t, repo, "worktree", "add", "-b", "run-work", work, "HEAD")
	return repo, work
}

// TestLandCommitsTheRunsWorkOntoItsBranch is the whole door end to end: a run
// whose worker writes a second file and edits the first, its landing answering
// the branch and the two paths, the repository's own HEAD untouched.
func TestLandCommitsTheRunsWorkOntoItsBranch(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo, work := runLandWorkspace(t)
	base := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "work"))
	store := runOpenStore(t)
	ctx := runContext(t)
	seat := newFakeSeat()
	seat.actions["root"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		writeRunFile(t, filepath.Join(work, "second.txt"), "two\n")
		writeRunFile(t, filepath.Join(work, "first.txt"), "one changed\n")
		return run.Report{Result: "wrote two files", Steps: 2}, nil
	}
	supervisor := run.NewSupervisor(store, work, 1, run.Limits{}, seat.workerFor)
	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want done", outcome)
	}

	landing, err := run.Land(ctx, store, work, base, store.RootID())
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	if landing.Branch != "run-work" {
		t.Fatalf("branch = %q, want the run's own branch", landing.Branch)
	}
	if want := []string{"first.txt", "second.txt"}; !reflect.DeepEqual(landing.Changed, want) {
		t.Fatalf("changed = %v, want %v", landing.Changed, want)
	}
	if landing.Refused != "" {
		t.Fatalf("refused = %q, want none on a landing", landing.Refused)
	}
	// THE PERSON'S REPOSITORY IS WHERE IT WAS: still on its own branch, still
	// one commit back, its file unchanged.
	if head := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "--abbrev-ref", "HEAD")); head != "work" {
		t.Fatalf("the repository's HEAD = %q, want it left untouched", head)
	}
	if now := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "work")); now != base {
		t.Fatalf("the repository's own branch moved: %s -> %s", base, now)
	}
	if got := readRunFile(t, filepath.Join(repo, "first.txt")); got != "one\n" {
		t.Fatalf("the repository's file = %q, want it untouched", got)
	}
	// AND THE WORK IS ON THE RUN'S BRANCH, in one commit carrying both files.
	landed := runGitOut(t, repo, "show", "--name-only", "--format=", "run-work")
	for _, want := range []string{"first.txt", "second.txt"} {
		if !strings.Contains(landed, want) {
			t.Fatalf("the run's branch does not carry %s:\n%s", want, landed)
		}
	}
	// AND THE ROOT CARRIES WHERE THE WORK WENT.
	if notes := store.Notes(store.RootID(), 0); len(notes) == 0 || notes[len(notes)-1].Body != "landed on run-work: 2 files" {
		t.Fatalf("the root's notes = %v, want the landing line", notes)
	}
}

// Contract 4a and 4b: the real landing must carry git-quoted names as their
// exact bytes while leaving the harness's own files outside the commit.
func TestLandKeepsEveryRealFilename(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "checkout", "-b", "work")
	writeRunFile(t, filepath.Join(repo, "x b", "plandb.db"), "one\n")
	runGit(t, repo, "add", "x b/plandb.db")
	runGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "seed")
	base := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "work"))
	work := filepath.Join(t.TempDir(), "run")
	runGit(t, repo, "worktree", "add", "-b", "run-work", work, "HEAD")
	store := runOpenStore(t)
	files := map[string]string{
		"x b/plandb.db":    "one\ntwo\n",
		" lead.txt":        "lead\n",
		"odd name é'q.txt": "accent\n",
		"has\"quote.txt":   "quote\n",
		"new\nline.txt":    "newline\n",
		"a -> b.txt":       "arrow\n",
	}
	seat := newFakeSeat()
	seat.actions["root"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		for path, content := range files {
			writeRunFile(t, filepath.Join(work, path), content)
		}
		writeRunFile(t, filepath.Join(work, "plandb.db"), "private\n")
		writeRunFile(t, filepath.Join(work, ".codeaf", "jobs", "x.log"), "private\n")
		return run.Report{Result: "wrote six files"}, nil
	}
	ctx := runContext(t)
	if got := run.NewSupervisor(store, work, 1, run.Limits{}, seat.workerFor).Run(ctx); got != run.OutcomeDone {
		t.Fatalf("run outcome = %q", got)
	}
	landing, err := run.Land(ctx, store, work, base, store.RootID())
	if err != nil || landing.Refused != "" || landing.Branch != "run-work" {
		t.Fatalf("landing = %+v, error = %v", landing, err)
	}
	want := make([]string, 0, len(files))
	for name := range files {
		want = append(want, name)
	}
	slices.Sort(want)
	if !reflect.DeepEqual(landing.Changed, want) {
		t.Fatalf("landing.Changed = %q, want %q", landing.Changed, want)
	}
	listed := strings.Split(runGitOut(t, repo, "ls-tree", "-r", "-z", "--name-only", landing.Branch), "\x00")
	for name, content := range files {
		if !slices.Contains(listed, name) {
			t.Errorf("landing tree omits %q: %q", name, listed)
		}
		if got := runGitOut(t, repo, "show", landing.Branch+":"+name); got != content {
			t.Errorf("landed %q = %q, want %q", name, got, content)
		}
	}
	for _, name := range []string{"plandb.db", ".codeaf/jobs/x.log"} {
		if slices.Contains(listed, name) {
			t.Errorf("landing tree includes harness file %q", name)
		}
	}
	if notes := store.Notes(store.RootID(), 0); len(notes) == 0 || notes[len(notes)-1].Body != "landed on run-work: 6 files" {
		t.Fatalf("root notes = %v, want six-file landing", notes)
	}
}

// TestLandRefusesARunThatWroteNothing is the other ending of the same door: a
// run that only read is a refusal with no branch, and the root says so.
func TestLandRefusesARunThatWroteNothing(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo, work := runLandWorkspace(t)
	base := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "work"))
	store := runOpenStore(t)
	ctx := runContext(t)
	before := runGitOut(t, repo, "branch", "--format=%(refname:short)")
	supervisor := run.NewSupervisor(store, work, 1, run.Limits{}, newFakeSeat().workerFor)
	if outcome := supervisor.Run(ctx); outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want done", outcome)
	}

	landing, err := run.Land(ctx, store, work, base, store.RootID())
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	if landing.Branch != "" || len(landing.Changed) != 0 {
		t.Fatalf("branch = %q changed = %v, want no landing", landing.Branch, landing.Changed)
	}
	if landing.Refused == "" {
		t.Fatal("refused is empty, want the sentence that says there was nothing to land")
	}
	// NO BRANCH WAS MADE FOR A LANDING THAT DID NOT HAPPEN.
	if after := runGitOut(t, repo, "branch", "--format=%(refname:short)"); after != before {
		t.Fatalf("branches moved on a refusal: %q -> %q", before, after)
	}
	if notes := store.Notes(store.RootID(), 0); len(notes) == 0 || notes[len(notes)-1].Body != landing.Refused {
		t.Fatalf("the root's notes = %v, want the refusal sentence", notes)
	}
}

// Contracts 1, 5 and 7: the real supervisor's worker commits on its branch;
// landing signs that commit, leaves its identity and hook alone, and counts its
// changed path even when the worker left no uncommitted work.
func TestLandSignsWorkerCommitAndNamesCommittedWork(t *testing.T) {
	for _, leftover := range []bool{false, true} {
		name := "committed only"
		if leftover {
			name = "committed and leftover"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			repo, work := runLandWorkspace(t)
			base := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "work"))
			hooks := strings.TrimSpace(runGitOut(t, work, "rev-parse", "--git-path", "hooks"))
			if !filepath.IsAbs(hooks) {
				hooks = filepath.Join(work, hooks)
			}
			marker := filepath.Join(t.TempDir(), "hook-ran")
			hookConfigBefore := runGitOut(t, work, "config", "--local", "--list")
			writeRunFile(t, filepath.Join(hooks, "pre-commit"), "#!/bin/sh\nprintf ran >> '"+marker+"'\n")
			if err := os.Chmod(filepath.Join(hooks, "pre-commit"), 0o755); err != nil {
				t.Fatal(err)
			}
			store := runOpenStore(t)
			seat := newFakeSeat()
			seat.actions["root"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
				writeRunFile(t, filepath.Join(work, "worker.txt"), "worker\n")
				runGit(t, work, "add", "worker.txt")
				runGit(t, work, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "commit", "-m", "worker")
				if leftover {
					writeRunFile(t, filepath.Join(work, "leftover.txt"), "leftover\n")
				}
				return run.Report{Result: "worker finished"}, nil
			}
			if got := run.NewSupervisor(store, work, 1, run.Limits{}, seat.workerFor).Run(runContext(t)); got != run.OutcomeDone {
				t.Fatalf("run outcome = %q", got)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("pre-commit hook did not run: %v", err)
			}
			landing, err := run.Land(runContext(t), store, work, base, store.RootID())
			if err != nil || landing.Refused != "" || landing.Branch != "run-work" {
				t.Fatalf("landing = %+v, %v", landing, err)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "ran" {
				t.Fatalf("landing ran the worker's hook again: %q, %v", data, err)
			}
			if hookConfigAfter := runGitOut(t, work, "config", "--local", "--list"); hookConfigAfter != hookConfigBefore {
				t.Fatalf("landing changed the repository's git config:\n%s\n--- before ---\n%s", hookConfigAfter, hookConfigBefore)
			}
			want := []string{"worker.txt"}
			if leftover {
				want = []string{"leftover.txt", "worker.txt"}
			}
			if !reflect.DeepEqual(landing.Changed, want) {
				t.Fatalf("changed = %v, want %v", landing.Changed, want)
			}
			workerRev := "run-work"
			if leftover {
				workerRev += "~1"
			}
			message := runGitOut(t, work, "log", "-1", "--format=%B", workerRev)
			if !strings.HasSuffix(message, "\n\nAssisted-by: CodeAF\nCo-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>\n") || strings.Count(message, "Assisted-by: CodeAF") != 1 {
				t.Fatalf("worker commit was not signed once: %q", message)
			}
			if who := strings.TrimSpace(runGitOut(t, work, "log", "-1", "--format=%an <%ae>|%cn <%ce>", workerRev)); who != "Worker <worker@example.test>|Worker <worker@example.test>" {
				t.Fatalf("worker identity changed: %q", who)
			}
			if leftover {
				message := runGitOut(t, work, "log", "-1", "--format=%B", "run-work")
				if strings.Count(message, "Assisted-by: CodeAF") != 1 || strings.Count(message, "Co-Authored-By: CodeAF") != 1 {
					t.Fatalf("landing commit was not signed once: %q", message)
				}
			}
			if now := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "work")); now != base {
				t.Fatalf("person's earlier commit moved: %s -> %s", base, now)
			}
			note := "landed on run-work: 1 file"
			if leftover {
				note = "landed on run-work: 2 files"
			}
			if notes := store.Notes(store.RootID(), 0); len(notes) == 0 || notes[len(notes)-1].Body != note {
				t.Fatalf("notes = %v, want %q", notes, note)
			}
		})
	}
}

// Contract 7: the run note names a file from committed work even if a later
// worker commit reverted its net tree change.
func TestLandNamesCommittedThenRevertedWork(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo, work := runLandWorkspace(t)
	base := strings.TrimSpace(runGitOut(t, repo, "rev-parse", "work"))
	store := runOpenStore(t)
	seat := newFakeSeat()
	seat.actions["root"] = func(_ context.Context, _ plandb.Task) (run.Report, error) {
		writeRunFile(t, filepath.Join(work, "reverted.txt"), "one\n")
		runGit(t, work, "add", "reverted.txt")
		runGit(t, work, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "commit", "-m", "add reverted")
		runGit(t, work, "-c", "user.name=Worker", "-c", "user.email=worker@example.test", "revert", "--no-edit", "HEAD")
		return run.Report{Result: "reverted"}, nil
	}
	if got := run.NewSupervisor(store, work, 1, run.Limits{}, seat.workerFor).Run(runContext(t)); got != run.OutcomeDone {
		t.Fatalf("run outcome = %q", got)
	}
	landing, err := run.Land(runContext(t), store, work, base, store.RootID())
	if err != nil || landing.Refused != "" || landing.Branch != "run-work" || !reflect.DeepEqual(landing.Changed, []string{"reverted.txt"}) {
		t.Fatalf("landing = %+v, %v", landing, err)
	}
	if notes := store.Notes(store.RootID(), 0); len(notes) == 0 || notes[len(notes)-1].Body != "landed on run-work: 1 file" {
		t.Fatalf("landing notes = %v", notes)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_EDITOR=true", "GIT_TERMINAL_PROMPT=0")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeRunFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRunFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
