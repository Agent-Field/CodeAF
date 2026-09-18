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
//   - THE BRANCH-OWNERSHIP QUESTION. Nothing in the consent road knows what a
//     branch is. internal/approval matches command text against patterns;
//     there is no table entry, rule shape or classification under which "a
//     branch this run did not create" can be said, so the test pins the whole
//     command line and asserts a refusal of the act. The landing road
//     (task_branch_protection.go) is the only place a branch is named, and it
//     asks whether the name is protected or has moved, never whether the run
//     created it.
//   - The second road that let it happen: the git guard's principal gate
//     (taskgit.go's whoseCopy) leaves a [Person]-headed session unguarded ON
//     PURPOSE — taskgit_test.go's own rows assert that. This test does not
//     overturn that contract; it fails on the gate that ran first, which
//     allowed the line before the guard was ever consulted.
//
// NO FIX IS IN THIS TREE. This test is written to fail until somebody decides
// what the answer for a --yolo session's own git push should be.
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

	// THE RUN'S POSTURE. A chat the person is sitting in front of and an
	// unattended run that named no ceiling both keep a [Person]
	// (principal_wire.go's newPrincipalFor); the incident run was the first of
	// those. Interactive is set to name the posture the incident actually had.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Interactive = true
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
