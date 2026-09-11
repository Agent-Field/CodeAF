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
	"strconv"
	"strings"
	"testing"
	"time"

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
		got := refusedTaskGit(one.command, taskGitVoice)
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

// AND IT MAY NOT REACH ANOTHER MACHINE FOR SOMEBODY ELSE'S WORK. A node's working
// copy is a copy of what the person has; a node that went and got something newer
// is reporting on a repository nobody asked it about.
//
// THE SENTENCE NO LONGER SAYS "reaches no remote", and that is a repair rather
// than a rewording: it was said to a worker whose brief pointed at the person's
// live checkout, where it was simply untrue, and a refusal a model can see
// through is a refusal it goes around (taskoutside.go carries that whole run).
func TestAWorkerMayNotReachARemote(t *testing.T) {
	for _, command := range []string{
		"git pull", "git pull --rebase origin main", "git fetch --all",
		"git remote add upstream https://example.invalid/x.git",
		"git clone https://example.invalid/x.git", "git submodule update --init",
	} {
		got := refusedTaskGit(command, taskGitVoice)
		if got == "" {
			t.Fatalf("%q was allowed, want work this task did not do kept out of its copy", command)
		}
		if !strings.Contains(got, "work this task did not do") {
			t.Fatalf("%q was refused with %q, want the refusal to say why", command, got)
		}
	}
}

// AND PUSH IS THE OTHER DIRECTION, which is why it has a sentence of its own: it
// is not somebody else's work arriving, it is this task's own work leaving past
// the landing that is the road it comes home on.
func TestAWorkerDoesNotPushItsOwnWork(t *testing.T) {
	for _, command := range []string{"git push", "git push origin HEAD", "git push -u origin fix/codeql-54"} {
		got := refusedTaskGit(command, taskGitVoice)
		if !strings.Contains(got, "comes home through its landing") {
			t.Fatalf("%q was refused with %q, want the refusal to name the road home", command, got)
		}
	}
}

// AND THE STASH, whose failure mode is the one that was found in a person's own
// checkout the morning after a run: a pop that would not go cleanly, and raw
// conflict markers left in files nobody looked at again. Reading the shelf is
// still allowed, because reading is always allowed.
func TestAWorkerMayReadTheStashAndNotUseIt(t *testing.T) {
	for _, command := range []string{"git stash", "git stash --include-untracked", "git stash pop", "git stash apply"} {
		if refusedTaskGit(command, taskGitVoice) == "" {
			t.Fatalf("%q was allowed", command)
		}
	}
	for _, command := range []string{"git stash list", "git stash show -p --stat"} {
		if got := refusedTaskGit(command, taskGitVoice); got != "" {
			t.Fatalf("%q was refused with %q, want reading the shelf to be allowed", command, got)
		}
	}
}

// THROWING THE WORKING COPY AWAY IS THROWING THE DELIVERABLE AWAY, and that is
// the only half of reset and restore that is refused. Unstaging and putting a
// file back as this worktree last had it are a worker's own business.
func TestAWorkerMayUnstageButMayNotDiscardItsWork(t *testing.T) {
	for _, command := range []string{"git reset --hard HEAD", "git reset --hard main", "git reset --merge", "git reset --keep origin/main"} {
		if refusedTaskGit(command, taskGitVoice) == "" {
			t.Fatalf("%q was allowed, want a worker's own deliverable protected from it", command)
		}
	}
	for _, command := range []string{"git reset", "git reset HEAD src/thing.py", "git reset --soft HEAD~1"} {
		if got := refusedTaskGit(command, taskGitVoice); got != "" {
			t.Fatalf("%q was refused with %q, want unstaging left alone", command, got)
		}
	}
	if refusedTaskGit("git restore --source=main src/thing.py", taskGitVoice) == "" {
		t.Fatal("git restore --source was allowed, and it is how a file is taken off another branch")
	}
	if got := refusedTaskGit("git restore src/thing.py", taskGitVoice); got != "" {
		t.Fatalf("git restore of the worker's own file was refused with %q", got)
	}
}

// READING IS ALWAYS ALLOWED. The whole point of the line this guard draws is that
// it is between knowing and taking: a worker that wants to know what is on main
// gets the whole answer. `add` and `commit` are left alone too — the brief tells a
// worker not to stage its own work (prompts/worker.md), so refusing them here would
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
		if got := refusedTaskGit(command, taskGitVoice); got != "" {
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

// C1, C2, C3 — A SESSION THAT WILL JUDGE ITS OWN WORK ANSWERS TO THE TASK'S
// WHOLE GIT LIST. The gate is the principal rather than the unattended flag:
// both kinds of Person keep their terminal, while a Steward gets a failed tool
// result before any part of the shell command can run.
func TestASessionCarryingItsOwnWorkAnswersToTheSameGitList(t *testing.T) {
	attended, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	stewardHeaded, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
	})
	if stewardHeaded.steward() == nil {
		t.Fatal("an unattended session with a ceiling did not get a Steward; the test no longer reaches the posture it claims to cover")
	}
	unattendedPerson, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
	})

	for _, command := range gitCommandsASessionMayNotRun() {
		call := bashGitCall(command)
		_, refusal, allowed := (taskGitGuard{agent: stewardHeaded}).PreAction(context.Background(), nil, nil, call)
		if allowed {
			t.Errorf("%q passed for a session carrying its own work", command)
		} else if !refusal.isError {
			t.Errorf("%q was refused without reaching the model as a failed call", command)
		}
		for name, person := range map[string]*Agent{
			"attended session":                     attended,
			"unattended session without a ceiling": unattendedPerson,
		} {
			if _, _, allowed := (taskGitGuard{agent: person}).PreAction(context.Background(), nil, nil, call); !allowed {
				t.Errorf("%q was refused for an %s", command, name)
			}
		}
	}
}

// C4 — READING AND SAVING ARE STILL ALWAYS ALLOWED. A session carrying its own
// work can inspect any ref, unstage or restore its own path, and commit without
// the guard mistaking those acts for taking somebody else's work.
func TestASessionsOwnReadingOfGitIsStillAllowed(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
	})
	if agent.steward() == nil {
		t.Fatal("an unattended session with a ceiling did not get a Steward")
	}
	for _, command := range []string{
		"git status --porcelain",
		"git diff HEAD main",
		"git log --oneline -3",
		"git show main:go.mod",
		"git branch --list",
		"git stash list",
		"git stash show -p --stat",
		"git reset HEAD src/thing.py",
		"git restore src/thing.py",
		"git add -A && git commit -m fix",
	} {
		if _, _, allowed := (taskGitGuard{agent: agent}).PreAction(context.Background(), nil, nil, bashGitCall(command)); !allowed {
			t.Errorf("%q was refused, want the session's reading and saving left alone", command)
		}
	}
}

// C5, C6 — THE SESSION READS ITS OWN REASON. Every category still produces the
// task's old sentence for a task, while a Steward gets no landing or deliverable
// vocabulary and is told to leave the work where this session will be judged.
func TestASessionIsRefusedInItsOwnRegisterAndNotTheTasks(t *testing.T) {
	const stashRefusal = "git stash is not yours to run here: it takes your working copy away, and what is in it is the work this session will be judged on. Leave the change in the tree, or commit it."
	if got := refusedTaskGit("git stash", sessionGitVoice); got != stashRefusal {
		t.Fatalf("session stash refusal = %q, want %q", got, stashRefusal)
	}

	for _, command := range gitCommandsASessionMayNotRun() {
		sessionRefusal := refusedTaskGit(command, sessionGitVoice)
		if sessionRefusal == "" {
			t.Errorf("%q had no session refusal", command)
			continue
		}
		for _, taskWords := range []string{"this task", "comes home", "the deliverable", "its landing"} {
			if strings.Contains(sessionRefusal, taskWords) {
				t.Errorf("%q was refused in the task's register: %q contains %q", command, sessionRefusal, taskWords)
			}
		}
		if !strings.Contains(sessionRefusal, sessionGitLeaveIt) {
			t.Errorf("%q was refused with %q, want it to say what the session should do instead", command, sessionRefusal)
		}
		taskRefusal := refusedTaskGit(command, taskGitVoice)
		if taskRefusal == "" {
			t.Errorf("%q no longer has the task's refusal", command)
		} else if taskRefusal == sessionRefusal {
			t.Errorf("%q reads the same in both postures: %q", command, taskRefusal)
		}
	}

	for _, one := range []struct {
		command string
		want    string
	}{
		{"git pull", "git pull is not yours to run: it would bring in work this task did not do, and this task reports what it writes as its own. Look with git status, diff, log and show — any branch, as much as you want. What you write with write and edit in this copy comes home on its own."},
		{"git merge", "git merge is not yours to run: it would put work this task did not do into your copy, and only what you write here comes home. Look with git status, diff, log and show — any branch, as much as you want. What you write with write and edit in this copy comes home on its own."},
		{"git stash", "git stash is not yours to run: it takes your working copy away and puts it back, and a stash that will not go back cleanly leaves conflict markers in the files. Look with git status, diff, log and show — any branch, as much as you want. What you write with write and edit in this copy comes home on its own."},
		{"git reset --hard HEAD", "git reset --hard is not yours to run: it throws your working copy away, and what is in it is the deliverable. Look with git status, diff, log and show — any branch, as much as you want. What you write with write and edit in this copy comes home on its own."},
		{"git restore --source=main src/thing.py", "git restore --source is not yours to run: it takes a file off another branch, and only what you write here comes home. Look with git status, diff, log and show — any branch, as much as you want. What you write with write and edit in this copy comes home on its own."},
		{"git push origin HEAD", "git push is not yours to run: This task's work comes home through its landing, and a pull request is the person's or the conversation's to open — say what you want in it in your report."},
	} {
		if got := refusedTaskGit(one.command, taskGitVoice); got != one.want {
			t.Errorf("task refusal for %q = %q, want its existing sentence %q", one.command, got, one.want)
		}
	}
}

// C6 — EVERY CLAUSE SAYS WHY, AND EVERY COMPLETED SENTENCE SAYS WHAT TO DO
// INSTEAD. Checking the two values together keeps a newly added field from
// quietly becoming an empty explanation in one register.
func TestEveryGitVoiceSaysWhyAndWhatToDoInstead(t *testing.T) {
	voices := []struct {
		name         string
		voice        gitVoice
		instead      string
		stashInstead string
		pushInstead  string
	}{
		{"task", taskGitVoice, taskGitInstead, taskGitInstead, taskLandingInstead},
		{"session", sessionGitVoice, sessionGitInstead, sessionGitLeaveIt, sessionGitInstead},
	}
	for _, one := range voices {
		fields := map[string]string{
			"opening": one.voice.opening, "remote": one.voice.remote,
			"moves": one.voice.moves, "stash": one.voice.stash,
			"discard": one.voice.discard, "offAnotherBranch": one.voice.offAnotherBranch,
			"push": one.voice.push,
		}
		for field, value := range fields {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s voice has an empty %s clause", one.name, field)
			}
		}
		for _, ending := range []struct {
			command string
			want    string
		}{
			{"git pull", one.instead},
			{"git merge", one.instead},
			{"git stash", one.stashInstead},
			{"git reset --hard", one.instead},
			{"git restore --source main", one.instead},
			{"git push", one.pushInstead},
		} {
			got := refusedTaskGit(ending.command, one.voice)
			if !strings.HasSuffix(got, ending.want) {
				t.Errorf("%s voice refused %q with %q, want it to end by saying %q", one.name, ending.command, got, ending.want)
			}
		}
	}
}

// gitCommandsASessionMayNotRun names a representative shape of every verb in
// the one production list, including each guarded form whose harmless sibling
// is allowed. It is shared by the posture and wording tests so those contracts
// cannot quietly cover different commands.
func gitCommandsASessionMayNotRun() []string {
	return []string{
		"git stash",
		"git stash pop",
		"git merge --ff-only main",
		"cd /tmp/x && git stash && pytest",
		"git pull --rebase origin main",
		"git fetch --all",
		"git clone https://example.invalid/project.git",
		"git remote add upstream https://example.invalid/project.git",
		"git submodule update --init",
		"git checkout main",
		"git switch main",
		"git rebase main",
		"git cherry-pick HEAD~1",
		"git revert HEAD",
		"git am change.patch",
		"git apply change.patch",
		"git worktree add ../other main",
		"git update-ref refs/heads/other HEAD",
		"git symbolic-ref HEAD refs/heads/other",
		"git reset --hard HEAD",
		"git reset --merge HEAD",
		"git reset --keep HEAD",
		"git restore --source=main src/thing.py",
		"git push origin HEAD",
	}
}

func bashGitCall(command string) ai.ToolCall {
	return ai.ToolCall{Function: ai.ToolCallFunction{
		Name: "bash", Arguments: `{"command":` + strconv.Quote(command) + `}`,
	}}
}
