package session

// A WORKER'S GIT MAY READ ANYTHING AND MOVE NOTHING.
//
// Written from two commands that really ran, in one benchmark cell, from two
// different workers of the same family:
//
//	cd <its own worktree> && git merge --ff-only main
//	cd <its own worktree> && git merge task/pr-validation-workflow-issue-20-5d1af9 --no-edit
//
// The first fast-forwarded a part onto a branch that already held the upstream
// project's own fixes for the issues that part had been asked to fix, and the
// part then reported the work as done. The second was a parent merging a
// sibling's branch by hand, which is the landing road's job (task_run.go's
// comeHome). Both ran INSIDE the node's own worktree, on the node's own HEAD, and
// touched no path outside it — which is why the guard is written about refs and
// not about directories.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE TWO COMMANDS THAT WERE MEASURED, and the family of acts they belong to.
func TestAWorkerMayNotMoveItsCopyOntoWorkItDidNotDo(t *testing.T) {
	refused := []struct {
		command string
		want    string
	}{
		{"cd /tmp/trees/2 && git merge --ff-only main", "git merge is not yours to run"},
		{"cd /tmp/trees/1 && git merge task/pr-validation-workflow-issue-20 --no-edit", "git merge is not yours to run"},
		{"git rebase main", "git rebase is not yours to run"},
		{"git cherry-pick 2649136", "git cherry-pick is not yours to run"},
		{"git checkout main", "git checkout is not yours to run"},
		{"git switch main", "git switch is not yours to run"},
		{"git revert HEAD", "git revert is not yours to run"},
		{"git worktree add ../elsewhere main", "git worktree is not yours to run"},
		// The chain is where every measured breach actually lived, and a check
		// that read only the first word of the command would have passed all of
		// them.
		{"git log --oneline -3 && echo ---- && git merge --ff-only main", "git merge is not yours to run"},
		{"git status; git rebase origin/main", "git rebase is not yours to run"},
		// Said with the target out loud is the same act.
		{"git -C /somewhere/else merge main", "git merge is not yours to run"},
		{"git --no-pager checkout main", "git checkout is not yours to run"},
	}
	for _, one := range refused {
		got := refusedTaskGit(one.command)
		if !strings.Contains(got, one.want) {
			t.Fatalf("%q answered %q, want %q in it", one.command, got, one.want)
		}
		// EVERY REFUSAL SAYS WHAT TO DO INSTEAD, in the same breath. A worker
		// told only "no" spends its next three steps saying the same thing in
		// other words, which the no-progress counter then reads as spinning.
		if !strings.Contains(got, "comes home on its own") {
			t.Fatalf("%q was refused with %q, want the sentence to name what it may do instead", one.command, got)
		}
	}
}

// AND IT MAY NOT REACH ANOTHER MACHINE. A node's working copy is a copy of what
// the person has; a node that went and got something newer is reporting on a
// repository nobody asked it about.
func TestAWorkerMayNotReachARemote(t *testing.T) {
	for _, command := range []string{
		"git pull", "git pull --rebase origin main", "git fetch --all",
		"git push origin HEAD", "git remote add upstream https://example.invalid/x.git",
		"git clone https://example.invalid/x.git", "git submodule update --init",
	} {
		got := refusedTaskGit(command)
		if got == "" {
			t.Fatalf("%q was allowed, want a worker's copy to reach no remote", command)
		}
		if !strings.Contains(got, "reaches no remote") {
			t.Fatalf("%q was refused with %q, want the refusal to say why", command, got)
		}
	}
}

// AND THE STASH, whose failure mode is the one that was found in a person's own
// checkout the morning after a run: a pop that would not go cleanly, and raw
// conflict markers left in files nobody looked at again. Reading the shelf is
// still allowed, because reading is always allowed.
func TestAWorkerMayReadTheStashAndNotUseIt(t *testing.T) {
	for _, command := range []string{"git stash", "git stash --include-untracked", "git stash pop", "git stash apply"} {
		if refusedTaskGit(command) == "" {
			t.Fatalf("%q was allowed", command)
		}
	}
	for _, command := range []string{"git stash list", "git stash show -p --stat"} {
		if got := refusedTaskGit(command); got != "" {
			t.Fatalf("%q was refused with %q, want reading the shelf to be allowed", command, got)
		}
	}
}

// THROWING THE WORKING COPY AWAY IS THROWING THE DELIVERABLE AWAY, and that is
// the only half of reset and restore that is refused. Unstaging and putting a
// file back as this worktree last had it are a worker's own business.
func TestAWorkerMayUnstageButMayNotDiscardItsWork(t *testing.T) {
	for _, command := range []string{"git reset --hard HEAD", "git reset --hard main", "git reset --merge", "git reset --keep origin/main"} {
		if refusedTaskGit(command) == "" {
			t.Fatalf("%q was allowed, want a worker's own deliverable protected from it", command)
		}
	}
	for _, command := range []string{"git reset", "git reset HEAD src/thing.py", "git reset --soft HEAD~1"} {
		if got := refusedTaskGit(command); got != "" {
			t.Fatalf("%q was refused with %q, want unstaging left alone", command, got)
		}
	}
	if refusedTaskGit("git restore --source=main src/thing.py") == "" {
		t.Fatal("git restore --source was allowed, and it is how a file is taken off another branch")
	}
	if got := refusedTaskGit("git restore src/thing.py"); got != "" {
		t.Fatalf("git restore of the worker's own file was refused with %q", got)
	}
}

// READING IS ALWAYS ALLOWED. The whole point of the line this guard draws is that
// it is between knowing and taking: a worker that wants to know what is on main
// gets the whole answer. `add` and `commit` are left alone too — the brief tells a
// worker not to stage its own work (prompts/task.md), so refusing them here would
// be the harness answering a question its own prompt already settled.
func TestAWorkerMayLookAnywhereAndSaveWhatItWrote(t *testing.T) {
	for _, command := range []string{
		"git status --porcelain",
		"git diff HEAD main",
		"git log --oneline main -5",
		"git show main:.github/workflows/code-check.yml",
		"git merge-base --is-ancestor HEAD main",
		"git merge-base HEAD main",
		"git branch --list",
		"git rev-parse HEAD",
		"git ls-files",
		"git add -A && git commit -m 'the arithmetic module'",
		"python -m pytest && git add src && git commit -m done",
		"echo 'git merge is a thing people talk about' > notes.md",
	} {
		if got := refusedTaskGit(command); got != "" {
			t.Fatalf("%q was refused with %q, want a worker's reading and saving left alone", command, got)
		}
	}
}

// ── the seam ────────────────────────────────────────────────────────────────

// THE REFUSAL IS FITTED ON THE PRE-ACTION SEAM and reaches the model as a result
// it can read, not as an error that ends anything. The seam is the one moment
// every execution passes through, which is why this is a hook rather than a
// wrapper around the belt's bash: an auditor's belt and a hand's belt both
// rebuild bash from bare directly and would have missed a wrapper entirely.
func TestTheGitGuardRefusesOnTheSeamAndOnlyInsideATask(t *testing.T) {
	call := ai.ToolCall{Function: ai.ToolCallFunction{
		Name: "bash", Arguments: `{"command":"cd /tmp/trees/2 && git merge --ff-only main"}`,
	}}

	// THE CONVERSATION IS UNTOUCHED. A person's own session, in a checkout they
	// opened themselves, may merge exactly as they would at their own terminal.
	chat, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, _, allowed := (taskGitGuard{agent: chat}).PreAction(context.Background(), nil, nil, call); !allowed {
		t.Fatal("the guard refused a person's own session; it answers for work inside a task and nothing else")
	}

	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	_, refused, allowed := (taskGitGuard{agent: worker}).PreAction(context.Background(), nil, nil, call)
	if allowed {
		t.Fatal("a worker's git merge passed the seam")
	}
	if !refused.isError {
		t.Fatal("the refusal did not reach the model as a failed call")
	}
	if !strings.Contains(refused.text, "git merge is not yours to run") {
		t.Fatalf("refusal = %q, want the guard's own wording", refused.text)
	}

	// A hand that is not bash, and a bash the guard has nothing to say about,
	// both go through untouched.
	for _, fine := range []ai.ToolCall{
		{Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"go.mod"}`}},
		{Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"git status"}`}},
		{Function: ai.ToolCallFunction{Name: "bash", Arguments: `not json`}},
	} {
		if _, _, allowed := (taskGitGuard{agent: worker}).PreAction(context.Background(), nil, nil, fine); !allowed {
			t.Fatalf("the guard refused %s %s", fine.Function.Name, fine.Function.Arguments)
		}
	}
}
