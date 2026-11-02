package run

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
)

// ── A BRANCH NAME ALREADY TAKEN ON THE REMOTE ──────────────────────────────
//
// A job's branch name comes from its issue, so it outlives the worktree: a run
// on another machine or a new home, or one after a Drop, cuts the same name
// afresh from the default branch while the remote still holds the older run.
// Git then rejects the push, and no retry can change that.
//
// THE APPROVED COMMIT IS NEVER REWRITTEN to fit (the approval names that exact
// object), and nobody's commits are overwritten. A branch still at the commit
// this checkout last pushed there holds nothing a person added, so the new work
// replaces it, under a lease on that commit. Any other tip might be anyone's, so
// it stays as it is and the work goes up beside it.

// publishBranchNames is how many names a job tries: its own, then -2, -3 and so on.
const publishBranchNames = 9

// lastPushedRef records, on this checkout, the commit it last pushed to a branch.
func lastPushedRef(branch string) string { return "refs/factory/pushed/" + branch }

// notePushed is best effort: without the record a later run takes a new name.
func notePushed(git Git, dir, branch, head string) {
	_, _ = git.Run(dir, "update-ref", lastPushedRef(branch), head)
}

// placeRejected puts head on the first branch name that loses nobody's work and
// answers the name. The caller's plain push to the job's own name was refused.
func placeRejected(git Git, pushBranch func(dir, ref string) error, dir string, it factory.Item, branch, head string) (string, error) {
	for n := 1; n <= publishBranchNames; n++ {
		name := branch
		if n > 1 {
			name = fmt.Sprintf("%s-%d", branch, n)
		}
		tip, err := branchTip(git, dir, name)
		if err != nil {
			return "", err
		}
		ours, _ := git.Run(dir, "rev-parse", "--verify", "--quiet", lastPushedRef(name))
		switch {
		case tip == "" || tip == head:
			return name, push(pushBranch, dir, it, head+":refs/heads/"+name)
		case tip == strings.TrimSpace(ours):
			return name, replaceOwnPush(git, dir, name, tip, head)
		}
	}
	return "", &publish.Error{Code: publish.CodeBranchNamesTaken, Detail: branch + " and its spare names hold other commits",
		Err: fmt.Errorf("%s and its spare names hold commits this job did not make", branch)}
}

// branchTip is the commit a branch has on the remote, "" when it has none.
func branchTip(git Git, dir, branch string) (string, error) {
	out, err := git.Run(dir, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", publish.Wrap(fmt.Errorf("could not read %s on the remote: %w", branch, err), "push")
	}
	tip, _, _ := strings.Cut(strings.TrimSpace(out), "\t")
	return tip, nil
}

// replaceOwnPush moves a branch only while it is still at expect: a person's
// push landing in between makes the remote refuse, and nothing is overwritten.
func replaceOwnPush(git Git, dir, branch, expect, head string) error {
	ref := "refs/heads/" + branch
	out, err := git.Run(dir, "push", "--force-with-lease="+ref+":"+expect, "origin", head+":"+ref)
	if err == nil {
		return nil
	}
	return publish.Wrap(errors.New(strings.TrimSpace(out+"\n"+err.Error())), "push")
}

// besideWords is what the job says when the work went up on another name.
func besideWords(branch, name string) string {
	return fmt.Sprintf("%s on GitHub holds commits this job did not make, so it was left as it is and this work is on %s", branch, name)
}
