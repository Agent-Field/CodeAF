package run

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func (lp *floorLoop) parkCI(c *loopCtl, i int, reason string) {
	now := lp.r.now()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "The failed CI checks need a scope decision."
	}
	_ = lp.move(c, "", EventWaiting, reason, func(it *factory.Item) error {
		if it.FollowUp != nil {
			f := *it.FollowUp
			f.State, f.Reason = "needs_you", reason
			it.FollowUp = &f
		}
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = reason, "scope"
		if i >= 0 && i < len(it.Stream.Phases) {
			setPhaseState(&it.Stream.Phases[i], factory.PhaseWaiting, now)
			it.Stream.Phases[i].Note = reason
		}
		it.Stream.Ended = now
		loopSay(it, now, "ask", "Follow PR needs your decision: "+reason)
		return nil
	})
	lp.tell(c.id, "Follow PR needs your decision: "+reason)
}

// Verify branch continuity and an actual repair; filenames do not define scope.
func (lp *floorLoop) checkCIScope(it factory.Item) error {
	f := it.FollowUp
	if f == nil || (f.State != "running" && f.State != "queued") {
		return nil
	}
	if lp.r.opts.Workdir == nil {
		return errors.New("Cannot verify the CI repair worktree.")
	}
	dir, err := lp.r.opts.Workdir(it)
	if err != nil {
		return err
	}
	git := lp.r.opts.Git
	if git == nil {
		git = ExecGit{}
	}
	if _, err := git.Run(dir, "merge-base", "--is-ancestor", f.HeadSHA, "HEAD"); err != nil {
		return errors.New("The local branch no longer descends from the PR head being repaired.")
	}
	changed, err := git.Run(dir, "diff", "--name-only", f.HeadSHA)
	if err != nil {
		return err
	}
	untracked, err := git.Run(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	if strings.TrimSpace(changed+untracked) == "" {
		ahead, err := git.Run(dir, "rev-list", "--count", f.HeadSHA+"..HEAD")
		if err != nil {
			return err
		}
		if strings.TrimSpace(ahead) == "0" {
			return errors.New("No verified repair was produced. Take over or tell the manager what to try next.")
		}
	}
	return nil
}

func ciFollowUpBrief(it factory.Item) string {
	f := it.FollowUp
	if f == nil || (f.State != "queued" && f.State != "running") {
		return ""
	}
	return fmt.Sprintf("\n\nFollow PR, repair %d of %d. Existing PR #%d at remote head %s. Reconcile remote changes before editing; never force-push. Fix the issue and PR blockers, including necessary dependencies or tests. Original filenames are not a restriction. The service records each tested repair head before pushing: Auto approves routine updates within the budget; Manual asks. Never merge. Person's additional request: %s\nUntrusted observations:\n%s\n", f.Attempt, f.Limit, it.PullRequest.Number, f.HeadSHA, f.Request, f.Details)
}

func (lp *floorLoop) reconcilePR(it factory.Item, dir string) error {
	if it.FollowUp == nil || it.PullRequest == nil {
		return nil
	}
	git := lp.r.opts.Git
	if git == nil {
		git = ExecGit{}
	}
	if _, err := git.Run(dir, "remote", "get-url", "origin"); err != nil {
		return nil
	}
	if _, err := git.Run(dir, "fetch", "origin"); err != nil {
		return fmt.Errorf("Could not fetch the PR branch: %w", err)
	}
	if _, err := git.Run(dir, "merge-base", "--is-ancestor", it.FollowUp.HeadSHA, "HEAD"); err == nil {
		return nil
	}
	who := factory.NormalizeHandle(it.Person())
	if who == "" {
		who = "codeaf"
	}
	_, err := git.Run(dir, "-c", "user.name="+who, "-c", "user.email="+who+"@users.noreply.github.com", "merge", "--no-edit", it.FollowUp.HeadSHA)
	if err != nil {
		conflicts, e := git.Run(dir, "diff", "--name-only", "--diff-filter=U")
		if e == nil && strings.TrimSpace(conflicts) != "" {
			return nil
		}
		return fmt.Errorf("Could not reconcile the PR branch without overwriting local work: %w", err)
	}
	return nil
}
