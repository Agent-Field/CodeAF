package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// THE ROAD A CHAT DOOR'S OWN BASH PUSH TOOK.
//
// An unattended `codeaf chat --yolo` run hosted a /task in a clone checked out
// on a local branch named `santos/dev` — the same name as the shared remote
// branch. The task itself was refused its push by the git guard's task register
// (taskgit.go, "git push is not yours to run"), read the refusal, and stopped.
// The DOOR hosting it then ran, as its own bash calls, the three lines this
// test replays — the last two of which landed two commits on the shared remote
// branch, with no pull request and nobody's word but its own.
//
// The brief this test was written from says what should hold: a --yolo run
// does not push to a branch it did not create. The nearest thing a test can
// reach without a live remote is the CONSENT DECISION that let the command
// run: the pre-action chain every bash call passes through (loop.go's
// ep.preAction), whose first citizen is the approval gate and whose last is
// the git guard. This test sends the three lines that really ran down that
// chain and asserts the refusal.
//
// WHAT THIS TEST CANNOT COVER, and does not pretend to:
//
//   - A live remote. The push never runs here; the test pins the decision
//     that stood in front of it, not the push itself.
//   - The task classifier at head.go's consequenceGated — the v1 resident's
//     road. internal/session imports internal/head nowhere, so nothing on the
//     chat road classifies a bash command by verb; that is a different
//     product's road and this fix does not wire it up.
//   - The git guard's principal gate (taskgit.go's whoseCopy) still leaves a
//     [Person]-headed session unguarded ON PURPOSE — taskgit_test.go's own rows
//     assert that. The fix is the NARROWER third register beside it
//     (taskgit.go's refusedUnattendedBranchMovement): an unattended run
//     standing on a branch it did not create may not push, merge or rebase it.
//     This test now reaches that register, because the session in it carries
//     the --yolo fact (Config.Unattended) and stands on a branch it did not
//     cut.
//
// THE FIX IS IN taskgit.go. The first draft of the test was written to fail
// before it existed; it now passes against the register that closes the road.
func TestAYoloRunDoesNotPushToABranchItDidNotCreate(t *testing.T) {
	// THE GATE THE FLAG BUILDS. cmd/codeaf's v3Policy replaces the default
	// with allow when --yolo is set (chatv3.go's `if yolo { mode = … allow }`),
	// keeps every rule the person wrote, and seeds no bash entry of its own —
	// so the policy below is the one a --yolo launch hands the session when
	// nothing else has been written down.
	policy, err := approval.Load(map[string]any{
		"default": string(approval.ActionAllow),
		"tools": map[string]any{
			"read": "allow", "grep": "allow", "find": "allow", "ls": "allow",
			"jobs":     "allow",
			"remember": "allow", "track": "allow", "recall": "allow",
			"manual": "allow", "settings": "allow",
		},
	})
	if err != nil {
		t.Fatalf("the --yolo policy did not load: %v", err)
	}

	// THE RUN'S POSTURE, IN FULL. The incident launch was `codeaf chat --yolo`
	// in a terminal: the door sets `cfg.Interactive = (no --once)` and
	// `cfg.Unattended = yolo` (cmd/codeaf/chatv3.go), which with no
	// --max-hours/--max-cost is a [Person] on every reading. The first draft of
	// this test set Interactive and the --yolo policy but NOT Unattended, so
	// nothing told the guard this was an unattended run; the fix is gated on
	// exactly that fact, so the session records it here.
	//
	// AND IT STANDS IN A REAL CHECKOUT. The incident's door was hosted in a
	// clone on `santos/dev` — a local branch sharing the remote's name, and a
	// branch the run did not create. The guard reads the branch off the
	// session's own workspace, so the session is given one.
	repo := t.TempDir()
	mustGit(t, repo, "init", "-q", "-b", "santos/dev")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "first")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Interactive = true
		config.Unattended = true
		config.Workspace = repo
		config.ApprovalPolicy = &policy
		config.AskConsent = true
	})
	if agent.steward() != nil {
		t.Fatal("the test agent grew a Steward; it no longer reaches the posture the incident ran under")
	}
	ep := agent.newEpisode()

	// THE THREE LINES THAT REALLY RAN, verbatim from the door's transcript
	// (toolCalls[].function.arguments). The first was refused by git itself —
	// non-fast-forward — and the second landed two commits on the shared
	// branch after a rebase. The third pushed a second branch out of a
	// directory the door created for the purpose.
	commands := []string{
		"cd /home/santosh/src/hint-fix && git push origin santos/dev 2>&1 | tail -3",
		"cd /home/santosh/src/hint-fix && git rebase origin/santos/dev 2>&1 | tail -2 && git log --oneline -3 && git push origin santos/dev 2>&1 | tail -3",
		"cd /tmp/hint-fix-pr && git switch -c fix/opening-hint-test-owns-profile && git push -u origin fix/opening-hint-test-owns-profile",
	}
	for _, command := range commands {
		_, refusal, allowed := ep.preAction(context.Background(), nil, bashGitCall(command))
		if allowed {
			args := mustJSON(t, map[string]string{"command": command})
			t.Errorf("%q was ALLOWED for a --yolo run standing on a branch it did not create; "+
				"the gate answered %q and no citizen refused it — this is the road the incident took",
				command, policy.Check("bash", json.RawMessage(args)))
			continue
		}
		if !refusal.isError {
			t.Errorf("%q was refused without reaching the model as a failed call", command)
		}
		if !strings.Contains(refusal.text, "push") {
			t.Errorf("%q was refused with %q, want the refusal to name the push it refuses", command, refusal.text)
		}
	}
}

// ── the three lines of the scope, said apart ────────────────────────────────

// AN UNATTENDED RUN MAY MOVE A BRANCH IT CUT ITSELF, AND NO OTHER. The three
// lines the brief named, each on its own session so branch ownership is the only
// thing that differs between them.
func TestAnUnattendedRunMayMoveOnlyTheBranchItCut(t *testing.T) {
	// (1) THE RUN'S OWN BRANCH STAYS PUSHABLE. A run standing on a branch it cut
	// — a standing copy ([StandingTree.Branch]) or a task branch
	// ([TaskNode.branch]) — may push it. The cut is seeded directly because this
	// test is about the RULE and not about [Agent.cutStandingTree].
	own := gitRepoOn(t, "task/demo-abc")
	ownRun, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Workspace = own
	})
	seedCutBranch(t, ownRun, own, "task/demo-abc")
	for _, command := range []string{"git push origin task/demo-abc", "git push -u origin HEAD"} {
		if _, refusal, allowed := (taskGitGuard{agent: ownRun}).PreAction(context.Background(), nil, nil, bashGitCall(command)); !allowed {
			t.Errorf("%q was refused with %q for a run pushing a branch it cut itself", command, refusal.text)
		}
	}

	// (2) AN ATTENDED SESSION IS UNTOUCHED. The same repository, the same
	// commands, and only [Config.Unattended] differs: a person in their own
	// checkout keeps their whole terminal, which is what taskgit_test.go's rows
	// already pin and this must not take away.
	foreign := gitRepoOn(t, "santos/dev")
	attended, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Interactive = true
		c.Workspace = foreign
	})
	for _, command := range []string{"git push origin santos/dev", "git merge --ff-only origin/santos/dev", "git rebase origin/santos/dev"} {
		if _, refusal, allowed := (taskGitGuard{agent: attended}).PreAction(context.Background(), nil, nil, bashGitCall(command)); !allowed {
			t.Errorf("%q was refused with %q for an attended session; the person's own git is theirs", command, refusal.text)
		}
	}

	// (3) PUSH, MERGE AND REBASE OF A BRANCH THE RUN DID NOT CREATE ARE
	// REFUSED. The merge and the rebase move the branch; the push sends it out.
	run, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Workspace = foreign
	})
	for _, command := range []string{"git push origin santos/dev", "git merge --ff-only origin/santos/dev", "git rebase origin/santos/dev"} {
		_, refusal, allowed := (taskGitGuard{agent: run}).PreAction(context.Background(), nil, nil, bashGitCall(command))
		if allowed {
			t.Errorf("%q was allowed for a run standing on a branch it did not create", command)
			continue
		}
		if !refusal.isError || !strings.Contains(refusal.text, "did not create") {
			t.Errorf("%q was refused with %q, want the branch-ownership refusal", command, refusal.text)
		}
	}

	// AND A RUN STILL READS, SAVES AND CREATES. Only the three movers are
	// refused; a branch it makes and switches to stays its own business.
	for _, command := range []string{"git status --porcelain", "git log --oneline -3", "git add -A && git commit -m x", "git switch -c wip/x"} {
		if _, refusal, allowed := (taskGitGuard{agent: run}).PreAction(context.Background(), nil, nil, bashGitCall(command)); !allowed {
			t.Errorf("%q was refused with %q, want the run's reading and saving left alone", command, refusal.text)
		}
	}
}

// gitRepoOn makes a throwaway repository standing on one branch.
func gitRepoOn(t *testing.T, branch string) string {
	t.Helper()
	repo := t.TempDir()
	mustGit(t, repo, "init", "-q", "-b", branch)
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "first")
	return repo
}

// seedCutBranch records that this session cut one branch, the way a standing
// copy or a task tree would.
func seedCutBranch(t *testing.T, agent *Agent, repo, branch string) {
	t.Helper()
	agent.mu.Lock()
	agent.trees = append(agent.trees, StandingTree{Folder: repo, Dir: repo, Branch: branch, Root: repo})
	agent.mu.Unlock()
}
