package run

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
)

// syncPRBase brings the base branch's live head into the owned PR worktree.
// The stage verifies the resulting checkout before the normal publication path
// can update the PR branch. This operation never pushes or changes the PR's
// target. Its words say when it fell back to the forge's recorded base.
func (lp *floorLoop) syncPRBase(ctx context.Context, id int, dir string) (base, message string, err error) {
	base, message, fallback, err := lp.syncPRBaseLive(ctx, id, dir)
	if err != nil {
		return "", "", err
	}
	if fallback != "" {
		message += " " + fallback
	}
	return base, message, nil
}

// ── THE BASE SYNC MERGES THE BASE BRANCH AS IT IS NOW ─────────────────────
//
// The owner's decision of 2026-11-17 ("use latest dev"): the forge's base
// commit for a pull request (BaseSHA) moves only when the pull request's head
// is pushed, so the door merged a base the branch had long left. Job 31
// (codeaf#1758) merged ace7888ab and answered "already included" while the PR
// still conflicted with dev at a597fdcc3, which the door never brought in.
// The door now reads the base branch's head on origin and merges that commit,
// recording it as the synced base. When it cannot (the forge named no base
// branch, the read or fetch failed, or the live head does not contain the
// recorded base), it says so and merges the recorded base as before.
//
// fallback is that saying, for the job's history; "" when the live head was
// used.
func (lp *floorLoop) syncPRBaseLive(ctx context.Context, id int, dir string) (base, message, fallback string, err error) {
	it, err := lp.activePRBaseSync(ctx, id)
	if err != nil {
		return "", "", "", err
	}
	pr := it.PullRequest
	r := lp.r
	if r.opts.Workdir == nil || dir == "" {
		return "", "", "", errors.New("Follow PR has no owned worktree")
	}
	ownedDir, err := r.opts.Workdir(it)
	if err != nil {
		return "", "", "", err
	}
	if samePath(ownedDir) != samePath(dir) || (r.opts.RepoDir != nil && samePath(r.opts.RepoDir(it.Repo)) == samePath(dir)) {
		return "", "", "", errors.New("Follow PR can update the base only in this job's worktree")
	}

	var src factory.Source
	git := r.opts.Git
	if r.opts.Publisher != nil {
		target, err := r.opts.Publisher(ctx, it)
		if err != nil {
			return "", "", "", fmt.Errorf("could not read the PR with this runner's account: %w", err)
		}
		src = target.Source
		if target.Git != nil {
			git = target.Git
		}
	} else if r.opts.Source != nil {
		src = r.opts.Source(it.Repo)
	}
	follower, ok := src.(factory.Follower)
	if !ok {
		return "", "", "", errors.New("this runner cannot read the current PR base")
	}
	fresh, err := follower.Follow(ctx, it)
	if err != nil {
		return "", "", "", fmt.Errorf("could not read the current PR base: %w", err)
	}
	if fresh.Number != pr.Number || fresh.State != factory.PROpen || fresh.HeadSHA != pr.HeadSHA || (pr.HeadBranch != "" && fresh.HeadBranch != pr.HeadBranch) {
		return "", "", "", errors.New("the PR closed or its head moved. Refresh Follow PR before updating the base")
	}
	if !publish.ValidCommit(fresh.BaseSHA) {
		return "", "", "", errors.New("the forge did not return a complete PR base commit")
	}

	if git == nil {
		git = ExecGit{}
	}
	root, err := git.Run(dir, "rev-parse", "--show-toplevel")
	if err != nil || samePath(strings.TrimSpace(root)) != samePath(dir) {
		return "", "", "", errors.New("the working folder is not the root of this job's worktree")
	}
	branch := recordedWorkBranch(it)
	if branch == "" {
		branch = itemBranch(it)
	}
	if err := checkPRBaseWorkBranch(git, dir, branch); err != nil {
		return "", "", "", err
	}
	if _, err := git.Run(dir, "merge-base", "--is-ancestor", pr.HeadSHA, "HEAD"); err != nil {
		return "", "", "", errors.New("the job's branch no longer contains the current PR head. Refresh Follow PR before updating the base")
	}

	// THE LIVE HEAD OF THE BASE BRANCH, fetched as an object only; the
	// forge's recorded base when it cannot be had.
	base = fresh.BaseSHA
	live, liveErr := livePRBase(git, dir, fresh.BaseBranch, fresh.BaseSHA)
	if liveErr != nil {
		fallback = "Could not use the live head of the base branch (" + liveErr.Error() + "), so this merged the PR's recorded base " + shortSHA(base) + " instead; a conflict with newer base commits may remain."
	} else {
		base = live
	}
	const inProgress = "The PR base merge is in progress locally. Resolve any conflicts, finish the merge commit, and verify the resulting checkout before publication."
	if pending, err := git.Run(dir, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err == nil {
		pending = strings.TrimSpace(pending)
		// A merge this door began on an earlier call is still that merge,
		// even if the base branch has moved on since.
		if pending == base {
			return base, inProgress, fallback, nil
		}
		if pending != "" && pending == it.FollowUp.BaseSyncSHA {
			return pending, inProgress, "", nil
		}
		return "", "", "", errors.New("a different merge is in progress. Finish it before updating the PR base")
	}
	if err := checkPRBaseWorkClean(git, dir); err != nil {
		return "", "", "", err
	}
	if _, err := git.Run(dir, "merge-base", "--is-ancestor", base, "HEAD"); err == nil {
		if _, err := git.Run(dir, "merge-base", "--is-ancestor", base, pr.HeadSHA); err == nil {
			return base, "The current base is already included in the published PR head.", fallback, nil
		}
		return base, "The current PR base is already included in the local worktree. Verify this checkout for publication.", fallback, nil
	}

	// Fetch just the authenticated object and leave all branch refs alone.
	// The live head was fetched when it was read.
	if liveErr != nil {
		if _, err := git.Run(dir, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "origin", base); err != nil {
			return "", "", "", fmt.Errorf("could not fetch the current PR base from origin: %w", err)
		}
	}
	// The operator or another command may have touched the worktree while
	// the fetch ran. Recheck its branch and cleanliness before changing it.
	if err := checkPRBaseWorkBranch(git, dir, branch); err != nil {
		return "", "", "", err
	}
	if err := checkPRBaseWorkClean(git, dir); err != nil {
		return "", "", "", err
	}
	if _, err := git.Run(dir, "merge-base", "--is-ancestor", pr.HeadSHA, "HEAD"); err != nil {
		return "", "", "", errors.New("the job's branch changed while fetching the PR base")
	}
	current, err := lp.activePRBaseSync(ctx, id)
	if err != nil {
		return "", "", "", err
	}
	currentPR := current.PullRequest
	if currentPR.BaseSHA != pr.BaseSHA && currentPR.BaseSHA != base {
		return "", "", "", errors.New("the PR base moved while fetching. Refresh Follow PR before updating it")
	}
	if currentPR.Number != pr.Number || currentPR.HeadSHA != pr.HeadSHA || currentPR.HeadBranch != pr.HeadBranch ||
		current.FollowUp.Attempt != it.FollowUp.Attempt || current.Stream.Cur != it.Stream.Cur ||
		current.Stream.Phases[current.Stream.Cur].Round != it.Stream.Phases[it.Stream.Cur].Round {
		return "", "", "", errors.New("the PR maintenance round changed while fetching the base")
	}
	who := factory.NormalizeHandle(current.Person())
	if who == "" {
		who = "codeaf"
	}
	if _, err := git.Run(dir, "-c", "user.name="+who, "-c", "user.email="+who+"@users.noreply.github.com", "merge", "--ff", "--no-edit", "--no-autostash", "--no-overwrite-ignore", base); err != nil {
		if pending, e := git.Run(dir, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); e == nil && strings.TrimSpace(pending) == base {
			return base, inProgress, fallback, nil
		}
		return "", "", "", fmt.Errorf("could not merge the PR base into this worktree: %w", err)
	}
	if _, err := git.Run(dir, "merge-base", "--is-ancestor", base, "HEAD"); err != nil {
		return "", "", "", errors.New("the local checkout does not contain the requested PR base after the merge")
	}
	return base, "The current PR base is merged locally. Verify this checkout so Factory can update the existing PR branch.", fallback, nil
}

// livePRBase reads the head of the pull request's base branch on origin and
// fetches that commit as an object, writing no ref. It refuses a branch name
// the forge did not give or git would not take, an answer that is not one
// complete commit, and a head that does not contain the recorded base (a
// rewritten branch is not the base the pull request was opened on).
func livePRBase(git Git, dir, branch, recorded string) (string, error) {
	branch = strings.TrimSpace(branch)
	if !plainBranchName(branch) {
		return "", errors.New("the forge did not name the PR's base branch")
	}
	ref := "refs/heads/" + branch
	out, err := git.Run(dir, "ls-remote", "--exit-code", "origin", ref)
	if err != nil {
		return "", fmt.Errorf("could not read %s on origin: %w", branch, err)
	}
	head := ""
	for _, line := range strings.Split(out, "\n") {
		if sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && strings.TrimSpace(name) == ref {
			head = strings.TrimSpace(sha)
		}
	}
	if !publish.ValidCommit(head) {
		return "", fmt.Errorf("origin did not answer one commit for %s", branch)
	}
	if _, err := git.Run(dir, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "origin", head); err != nil {
		return "", fmt.Errorf("could not fetch %s at %s from origin: %w", branch, shortSHA(head), err)
	}
	if head != recorded && publish.ValidCommit(recorded) {
		if _, err := git.Run(dir, "merge-base", "--is-ancestor", recorded, head); err != nil {
			return "", fmt.Errorf("%s at %s does not contain the recorded base", branch, shortSHA(head))
		}
	}
	return head, nil
}

// plainBranchName is a branch name safe to put after refs/heads/ on a git
// command line: no option, no whitespace or control character, nothing git
// reads as a revision range or pattern.
func plainBranchName(b string) bool {
	if b == "" || strings.HasPrefix(b, "-") || strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".lock") ||
		strings.Contains(b, "..") || strings.Contains(b, "//") || strings.Contains(b, "@{") {
		return false
	}
	for _, r := range b {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}

// shortSHA is a commit as the job's history names it.
func shortSHA(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

func checkPRBaseWorkBranch(git Git, dir, branch string) error {
	got, err := git.Run(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(got) != branch {
		return errors.New("the worktree is no longer on the job's branch")
	}
	return nil
}

func checkPRBaseWorkClean(git Git, dir string) error {
	status, err := git.Run(dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("could not read the job's worktree: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("the job's worktree has uncommitted changes. Preserve that work before updating the PR base")
	}
	return nil
}

func (lp *floorLoop) activePRBaseSync(ctx context.Context, id int) (factory.Item, error) {
	it, err := lp.r.opts.Store.Get(id)
	if err != nil {
		return it, err
	}
	if err := factory.CheckPersonStop(it); err != nil {
		return it, err
	}
	if err := ctx.Err(); err != nil {
		return it, err
	}
	s, f, pr := it.Stream, it.FollowUp, it.PullRequest
	if !factory.IsIssueJob(it) || it.State != factory.StateRunning || it.Manual || it.Publishing || it.Question != "" || it.Asking != nil ||
		f == nil || f.Kind == "readiness" || f.State != "running" || pr == nil || pr.Number <= 0 || pr.State != factory.PROpen ||
		pr.HeadSHA != f.HeadSHA || !publish.ValidCommit(pr.HeadSHA) || s == nil || s.Paused || s.Cur < 0 || s.Cur >= len(s.Phases) ||
		!factory.IsCIRepairStage(factory.Stage{Name: s.Phases[s.Cur].Name}) || s.Phases[s.Cur].State != factory.PhaseRunning {
		return it, errors.New("base updates require an active Follow PR repair for this job's open PR")
	}
	return it, lp.r.allowWrite(ctx, it, factory.Stage{Name: factory.FollowPRStep, Kind: factory.StageChat})
}
