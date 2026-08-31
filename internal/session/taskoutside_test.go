package session

// A TASK MAY READ THE WHOLE MACHINE AND MAY WRITE IN ONE DIRECTORY.
//
// Every command in this file is one a worker really ran, taken out of the two
// task journals of the run that made the law necessary (2026-08-31): a
// conversation opened in the person's home directory, a brief that named their
// live checkout by its full path, and two workers that took it at its word — then
// escalated through three spellings of the same act when the guard next door
// refused them with a sentence that was false about the path they had named.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// theGround and thePersonsRepository are the two paths the whole file is about.
// Neither has to exist: the comparison is made on the paths as written, and only
// a path that already looks outside is ever resolved on disk.
const (
	theGround           = "/work/trees/1"
	thePersonsCheckout  = "/Users/santoshkumar/Documents/agentfield/code/agentfield"
	theOtherCheckout    = "/Users/santoshkumar/Documents/agentfield/code/aforge-v2"
	outsideYourCopy     = "is outside your copy"
	worksInTheGroundNow = "this task works in " + theGround
)

// THE ESCALATION, IN THE ORDER IT HAPPENED. Each of these was refused as a verb
// and answered with a paragraph about the task's own copy, so the model tried the
// same act said another way. All three spellings are the same act, and all three
// are the same refusal now.
func TestATaskMayNotChangeARepositoryItIsNotStandingIn(t *testing.T) {
	refused := []string{
		// The brief said "work in this repo directly", and this is what that
		// became on the first step.
		"cd " + thePersonsCheckout + " && git checkout -b fix/codeql-54-polynomial-regex",
		// Said with the target out loud, after the first refusal.
		"git -C " + thePersonsCheckout + " checkout -b fix/codeql-54-polynomial-regex",
		// And said through the environment, after the second.
		"GIT_DIR=" + thePersonsCheckout + "/.git git update-ref refs/heads/agent/oss-telemetry-v2 ec645c88",
		// The commit built by hand out of plumbing, which no verb table names.
		"cd " + thePersonsCheckout + " && BLOB=$(git hash-object -w /tmp/patched_test_execution_logger.py)",
		"cd " + thePersonsCheckout + " && git commit-tree 1577880b -p b458f9c3 -m 'fix the alert'",
		"cd " + thePersonsCheckout + " && git write-tree",
		// Staging and branch surgery in the person's own checkout.
		"cd " + thePersonsCheckout + " && git add sdk/python/tests/test_execution_logger.py",
		"cd " + thePersonsCheckout + " && git branch -f agent/oss-telemetry-v2 ec645c88",
		"cd " + thePersonsCheckout + " && git branch -D fix/codeql-54-polynomial-regex",
		"cd " + thePersonsCheckout + " && git reset --hard HEAD~1",
		// And a merge into a repository that is not this task's — the act
		// taskgit.go was written for, aimed somewhere its sentences are false.
		"cd " + theOtherCheckout + " && git merge --ff-only origin/dev",
		// The hands that name their target need no git at all.
		"cp /tmp/patched.py " + thePersonsCheckout + "/sdk/python/tests/test_execution_logger.py",
		"mv notes.md " + thePersonsCheckout + "/notes.md",
		"rm -rf " + thePersonsCheckout + "/build",
		"mkdir -p " + thePersonsCheckout + "/sdk/typescript/src/verification",
		"cd " + thePersonsCheckout + " && mkdir -p sdk/typescript/src",
		"echo 'the fix' > " + thePersonsCheckout + "/NOTES.md",
		"cd " + thePersonsCheckout + " && cat sdk/python/x.py > sdk/python/y.py",
		"cd " + thePersonsCheckout + " && sed -i '' 's/a/b/' sdk/python/x.py",
		// And the directory above the ground, which is the same act with no
		// repository named at all.
		"cd .. && rm -rf 2",
	}
	for _, command := range refused {
		why := refusedOutsideGround(theGround, command)
		if why == "" {
			t.Fatalf("%q was allowed, want it refused as outside the ground", command)
		}
		if !strings.Contains(why, outsideYourCopy) || !strings.Contains(why, worksInTheGroundNow) {
			t.Fatalf("%q was refused with %q, want it to name both the thing and the ground", command, why)
		}
		// AND NEVER WITH THE OTHER GUARD'S SENTENCES, which are about a copy
		// this command was not aimed at. This is the whole defect: every clause
		// of "this is your own copy of the repository and it reaches no remote"
		// was false about the path these commands named.
		for _, lie := range []string{"is not yours to run", "your own copy of the repository", "reaches no remote"} {
			if strings.Contains(why, lie) {
				t.Fatalf("%q was refused with %q, which says %q about somewhere it does not own", command, why, lie)
			}
		}
	}
}

// READING IS ALWAYS ALLOWED, ANYWHERE. A task briefed about a repository it is
// not standing in has to be able to look at it, and looking has never been what
// goes wrong. Every command here also came out of the same two journals.
func TestATaskMayReadAnyRepositoryOnTheMachine(t *testing.T) {
	for _, command := range []string{
		"git -C " + thePersonsCheckout + " log --oneline -5",
		"cd " + thePersonsCheckout + " && git log --oneline -3",
		"cd " + thePersonsCheckout + " && git show origin/main:sdk/python/tests/test_execution_logger.py",
		"cd " + thePersonsCheckout + " && git diff --stat main..fix/codeql-54",
		"cd " + thePersonsCheckout + " && git status --porcelain",
		"cd " + thePersonsCheckout + " && git branch -a",
		"cd " + thePersonsCheckout + " && git remote -v",
		"cd " + thePersonsCheckout + " && git rev-list --count main..fix/codeql-54",
		"cd " + thePersonsCheckout + " && cat sdk/typescript/package.json | head -30",
		"grep -rn 'polynomial' " + thePersonsCheckout + "/sdk",
		"ls -la " + thePersonsCheckout,
		"rg --files " + thePersonsCheckout,
		// The machine's scratch is not anybody's work, and a command line uses it
		// constantly. Both of these are out of the same run.
		"cd " + thePersonsCheckout + " && curl -s -o /dev/null -I -w '%{http_code}' https://example.invalid",
		"git show HEAD:go.mod > /tmp/before.go.mod",
		"cd " + thePersonsCheckout + " && git diff > /tmp/theirs.patch",
		"cd " + thePersonsCheckout + " && git branch -a 2>&1 | grep -E 'main|fix'",
		// And its own copy is its own to work in, by every verb.
		"cd " + theGround + " && mkdir -p sdk/typescript/src && npm test",
		"cd " + theGround + " && git add -A && git commit -m 'the fix'",
		"echo 'the fix' > " + theGround + "/NOTES.md",
		"mkdir -p src/verification && touch src/verification/local.ts",
	} {
		if why := refusedOutsideGround(theGround, command); why != "" {
			t.Fatalf("%q was refused with %q, want reading anywhere and writing here left alone", command, why)
		}
	}
}

// THE ROAD HOME IS NOT A REMOTE, and this is the bypass class: when the guard
// would not let the work be committed, the run landed it through the forge's own
// API instead — blob, tree, commit, ref, and then a pull request onto the
// person's project. It is refused wherever the task is standing, because a push
// from inside its own copy reaches the same machine.
func TestATaskDoesNotLandItsOwnWork(t *testing.T) {
	for _, command := range []string{
		"git push origin fix/codeql-54-polynomial-regex",
		"GIT_DIR=" + thePersonsCheckout + "/.git git push origin fix/codeql-54",
		"cd " + theGround + " && git push -u origin HEAD",
		"gh api repos/Agent-Field/agentfield/git/blobs -f content=@patched.py -f encoding=utf-8",
		"gh api repos/Agent-Field/agentfield/git/trees -f base_tree=1577880b -F tree[][path]=x",
		"gh api repos/Agent-Field/agentfield/git/refs -f ref=refs/heads/fix/codeql-54",
		"gh api -X PATCH repos/Agent-Field/agentfield/git/refs/heads/main",
		"gh pr create --repo Agent-Field/agentfield --base main --head fix/codeql-54 --title 'fix' --body 'x'",
		"gh pr merge 1017 --squash",
		"gh issue create --title 'the alert' --body 'x'",
		"gh release create v1.2.3",
		"curl -X POST -H 'Authorization: bearer x' https://api.github.com/repos/Agent-Field/agentfield/git/refs -d '{}'",
	} {
		why := refusedTaskReach(command)
		if why == "" {
			t.Fatalf("%q was allowed, want a task's work to come home through its landing", command)
		}
		if !strings.Contains(why, "comes home through its landing") {
			t.Fatalf("%q was refused with %q, want the refusal to say where the work goes", command, why)
		}
	}
	// AND THE READING HALF OF THE SAME TOOLS IS UNTOUCHED. `gh pr list` and
	// `gh pr view` are how the run found out what it was fixing, and a guard that
	// took them away would be taking away the brief.
	for _, command := range []string{
		"gh pr list --repo Agent-Field/agentfield --state open",
		"gh pr view 1017 --repo Agent-Field/agentfield --json files",
		"gh pr diff 1017",
		"gh issue view 54 --repo Agent-Field/agentfield",
		"gh api repos/Agent-Field/agentfield/git/commits/b458f9c3 --jq '.tree.sha'",
		"gh api -X GET repos/Agent-Field/agentfield/pulls",
		"gh run list --workflow code-check.yml",
		"gh auth status",
		"curl -s https://api.github.com/repos/Agent-Field/agentfield",
		"curl -X POST -d 'q=x' https://search.example.invalid/api",
		"git fetch --all",
		"git status",
	} {
		if why := refusedTaskReach(command); why != "" {
			t.Fatalf("%q was refused with %q, want a task's reading and its own business left alone", command, why)
		}
	}
}

// ── the seam ────────────────────────────────────────────────────────────────

// THE REFUSAL IS FITTED ON THE PRE-ACTION SEAM, so it reaches the model as a
// result it can read and the tool never runs — and it is marked as the HARNESS's
// answer at that same chokepoint, so a worker that reads a refusal and tries
// something else is not then scolded for spinning (looped.go).
func TestAWorkerIsRefusedOnTheSeamForWritingOutsideItsGround(t *testing.T) {
	worker, ground := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
	})
	elsewhere := t.TempDir()
	episode := worker.newEpisode()

	outside := worker.executeTool(context.Background(), episode, nil,
		withdrawnCall("c1", "write", `{"path":`+quoteJSON(filepath.Join(elsewhere, "NOTES.md"))+`,"content":"x"}`), "")
	if !outside.isError {
		t.Fatalf("a write outside the ground came back as a success: %+v", outside)
	}
	if !strings.Contains(outside.text, outsideYourCopy) || !strings.Contains(outside.text, ground) {
		t.Fatalf("the refusal was %q, want it to name the path and the ground", outside.text)
	}
	if !outside.harness {
		t.Fatal("the refusal was booked against the model, so a worker that tries something else will be called a repeater")
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "NOTES.md")); err == nil {
		t.Fatal("the file was written anyway; the refusal has to come BEFORE the hand runs")
	}

	// AND THE SAME WRITE INSIDE THE GROUND IS THE WORK. This is the half a guard
	// gets wrong by being too broad, and it is the half the task is for.
	inside := worker.executeTool(context.Background(), episode, nil,
		withdrawnCall("c2", "write", `{"path":"NOTES.md","content":"the fix"}`), "")
	if inside.isError {
		t.Fatalf("a write in the task's own ground was refused: %q", inside.text)
	}
	if _, err := os.Stat(filepath.Join(ground, "NOTES.md")); err != nil {
		t.Fatalf("the write in the ground did not reach the disk: %v", err)
	}

	// AND A BASH AIMED OUT OF THE GROUND IS REFUSED WITH THE PATH LAW'S SENTENCE
	// AND NOT THE VERB GUARD'S, which is the defect this lane exists for.
	command := `{"command":"cd ` + elsewhere + ` && git checkout -b fix/codeql-54"}`
	shell := worker.executeTool(context.Background(), episode, nil, withdrawnCall("c3", "bash", command), "")
	if !shell.isError || !strings.Contains(shell.text, outsideYourCopy) {
		t.Fatalf("the shell call answered %+v, want the path law's refusal", shell)
	}
	if strings.Contains(shell.text, "not yours to run") {
		t.Fatalf("the refusal was %q, which is the verb guard's sentence about a copy this command never named", shell.text)
	}
	if !shell.harness {
		t.Fatal("the shell refusal was booked against the model")
	}

	// AND THE CONVERSATION IS UNTOUCHED. A person in their own checkout writes
	// wherever they like.
	chat, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	call := ai.ToolCall{Function: ai.ToolCallFunction{
		Name: "write", Arguments: `{"path":"` + filepath.Join(elsewhere, "theirs.md") + `","content":"x"}`,
	}}
	if _, _, allowed := (taskGroundGuard{agent: chat}).PreAction(context.Background(), nil, nil, call); !allowed {
		t.Fatal("the guard refused a person's own session; it answers for work inside a task and nothing else")
	}
}
