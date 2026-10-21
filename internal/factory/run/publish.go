package run

// ── PUBLISHING: A PERSON'S APPROVAL, CARRIED OUT ──────────────────────────
//
// PUBLISH IS A PERSON. Approving at the proof gate is a server-side event in
// the factory's record (`evt_…`, internal/factory/record); the factory turns
// it into the trailer `Factory-Approval: evt_… @handle <commit>` and hands the trailer
// to the worker that publishes. AGENTS NEVER ATTEST: nothing here publishes
// without a trailer in that exact shape, and the trailer never comes from a
// model's words.
//
// For an issue (or any job that is not a pull request) the worker commits the
// job's worktree before the proof gate opens, reports its head, then pushes
// its branch with the person's token (never with force) and opens the pull
// request through the job's source, its body the proof summary, the claims
// and the trailer as its last line. When there are no commits beyond the default
// branch, it posts that body as an issue comment without pushing. For a pull
// request job the approval is a
// review comment on that pull request with the same body: codeaf never pushes
// to someone else's branch. The worker reports the pr
// {number, url} or the comment URL, and the factory marks the job done.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
)

// trailerShape carries the record's approval id, its person and the exact
// approved head. An older receipt remains valid only for an unreported head.
var trailerShape = regexp.MustCompile(`^Factory-Approval: evt_[0-9a-f]{8} @[a-z0-9-]+(?: (?:[0-9a-f]{40}|[0-9a-f]{64}))?$`)

// ErrNoApproval is a publish asked for without a recorded approval.
var ErrNoApproval = errors.New("job requires a recorded approval before publishing")

// ValidTrailer says trailer is a recorded approval's trailer line.
func ValidTrailer(trailer string) bool { return trailerShape.MatchString(trailer) }

// TrailerHandle is the forge handle a valid trailer names, "" for none.
func TrailerHandle(trailer string) string {
	if !ValidTrailer(trailer) {
		return ""
	}
	_, handle, _ := strings.Cut(trailer, " @")
	handle, _, _ = strings.Cut(handle, " ")
	return handle
}

// TrailerCommit is the exact approved head, absent on legacy receipts.
func TrailerCommit(trailer string) string {
	if !ValidTrailer(trailer) {
		return ""
	}
	parts := strings.Fields(trailer)
	if len(parts) == 4 {
		return parts[3]
	}
	return ""
}

// PublishItem is the factory's half of approving a landed job: the proof
// gate's check and mark, and the job queued for a worker to publish it. The
// trailer rides to that worker as a signal (internal/factory/lease).
func PublishItem(it *factory.Item, edited bool, now time.Time) error {
	if err := approveStored(it, edited, now); err != nil {
		return err
	}
	// A retry of a publish that failed starts clean. The approval already
	// recorded is the one this queue carries.
	it.PublishError = ""
	// A new publish is a new head: a merge that failed on the old one says
	// nothing about it.
	it.Merging = nil
	it.State = factory.StateQueued
	it.MarkPublishing("queued", "", "", now)
	loopSay(it, now, "said", "approved · publishing")
	return nil
}

// ShipPublished is the factory's end of a publish a worker reported: the job
// shipped, its pull request or issue comment on the job. A job with neither
// receipt reported has published nothing and does not ship this way.
func ShipPublished(it *factory.Item, now time.Time) error {
	url := strings.TrimSpace(it.CommentURL)
	if pr := it.PullRequest; pr != nil {
		if pr.Number <= 0 || strings.TrimSpace(pr.URL) == "" {
			return errors.New("a published job reports its pull request")
		}
		url = pr.URL
	}
	if url == "" {
		return errors.New("a published job reports its pull request or comment")
	}
	ensureStream(it, now)
	it.State = factory.StateShipped
	factory.WatchPublishedPR(it)
	it.MarkPublishing("opened", "", "", now)
	it.Question, it.QKind = "", ""
	it.Asking = nil
	it.Stream.Ended = now
	it.Stream.Paused = false
	loopSay(it, now, "ok", "published · your approval · "+url)
	return nil
}

// Publish carries a person's approval of a landed job out on this machine:
// commit, push and open the pull request, or post a comment for proof-only
// work and pull request jobs, then ship the job with its receipt. It runs on its own
// goroutine, so [Runner.Running] holds while it works and the worker keeps
// beating; a publish that fails leaves the job landed, marks publishing
// failed, and keeps the reason on its log and in PublishError. The approval
// already recorded stays valid for that same commit, and retrying does not
// ask for a second one.
func (r *Runner) Publish(id int, trailer string) error {
	trailer = strings.TrimSpace(trailer)
	if !ValidTrailer(trailer) {
		return ErrNoApproval
	}
	lp := r.loop()
	lp.mu.Lock()
	if lp.ctls[id] != nil {
		lp.mu.Unlock()
		return r.notLanded(id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, steer: make(chan string, 1), answers: make(chan loopAnswer, 1), phase: -1}
	lp.ctls[id] = c
	lp.mu.Unlock()
	now := r.now()
	err := lp.write(c, func(it *factory.Item) error {
		if err := factory.RequireIssueJob(*it); err != nil {
			return err
		}
		if it.State != factory.StateLanded {
			return fmt.Errorf("%s has not landed", it.Ref())
		}
		if TrailerCommit(trailer) != it.HeadCommit {
			return publish.ErrCommitChanged
		}
		it.State = factory.StateRunning
		it.MarkPublishing("pushing", "", "", now)
		loopSay(it, now, "said", "publishing · "+trailer)
		return nil
	})
	if err != nil {
		lp.forget(c)
		return err
	}
	go func() {
		defer lp.release(c)
		lp.publishRun(c, trailer)
	}()
	return nil
}

func (lp *floorLoop) publishRun(c *loopCtl, trailer string) {
	it, err := lp.r.opts.Store.Get(c.id)
	if err != nil {
		return
	}
	receipt, err := lp.r.publish(c.ctx, it, trailer)
	now := lp.r.now()
	if err != nil {
		_ = lp.write(c, func(it *factory.Item) error {
			it.State = factory.StateLanded
			detail := loopFirstLine(err.Error())
			if detail == "" {
				detail = "could not publish"
			}
			it.PublishError = detail
			it.MarkPublishing("failed", publish.ErrorCode(err), strings.TrimSpace(publish.ErrorDetail(err)), now)
			loopSay(it, now, "fail", "could not publish: "+detail)
			return nil
		})
		return
	}
	if lp.write(c, func(it *factory.Item) error {
		it.PullRequest, it.CommentURL = receipt.pr, receipt.comment
		it.Publish = "comment"
		url := receipt.comment
		if receipt.pr != nil {
			it.Publish, url = "pr", receipt.pr.URL
		}
		it.State = factory.StateShipped
		if receipt.pr == nil {
			it.DoneHow = "answered"
			it.DoneAt = now
		}
		it.PublishError = ""
		if receipt.pr != nil {
			if it.FollowUp == nil {
				it.FollowUp = &factory.PRFollowUp{Kind: "pr", Limit: factory.DefaultPRRepairAttempts}
			}
			if it.FollowUp.PublishConsent == "" {
				it.FollowUp.PublishConsent = trailer
				it.FollowUp.AutoPush = true
			}
			if factory.ReadinessPending(*it) && !receipt.pr.Draft {
				a := *it.FollowUp.Readiness
				a.State, a.At = "ready", now
				it.FollowUp.Readiness = &a
				it.Note(now, "PR marked ready for review · head "+a.Head+" · "+a.Summary)
				loopSay(it, now, "ok", fmt.Sprintf("PR #%d marked ready for review · reviewed head %s · %s", receipt.pr.Number, a.Head, a.Summary))
			}
			factory.WatchPublishedPR(it)
		}
		it.MarkPublishing("opened", "", "", now)
		loopSay(it, now, "ok", "published · "+url)
		return nil
	}) == nil {
		lp.emit(c.id, "", EventShipped, "shipped")
		if receipt.pr != nil {
			if factory.ReadinessPending(it) && !receipt.pr.Draft {
				lp.tell(c.id, fmt.Sprintf("PR #%d is ready for review. Final review passed on head %s. Follow PR continues monitoring CI, feedback and conflicts; merge remains your decision.", receipt.pr.Number, it.HeadCommit))
				return
			}
			mode := "Manual asks before pushing repairs; enable Auto for automatic checked updates within the job budget."
			if factory.Automatic(it) {
				mode = "Auto records and pushes checked repairs within the job budget."
			}
			lp.tell(c.id, "PR available: "+receipt.pr.URL+". Follow PR watches CI, reviews and conflicts. "+mode+" Merge remains your decision.")
		} else {
			lp.tell(c.id, "Published issue comment: "+receipt.comment)
		}
	}
}

// published is the receipt of the action actually taken on the forge.
type published struct {
	pr      *factory.PullRequest
	comment string
}

// publish writes the approved job to its source and answers its receipt.
func (r *Runner) publish(ctx context.Context, it factory.Item, trailer string) (published, error) {
	var src factory.Source
	pushBranch := r.opts.Push
	if r.opts.Publisher != nil {
		target, err := r.opts.Publisher(ctx, it)
		if err != nil {
			return published{}, publish.Wrap(err, "push")
		}
		src, pushBranch = target.Source, target.Push
	} else if r.opts.Source != nil {
		src = r.opts.Source(it.Repo)
	}
	if src == nil {
		return published{}, fmt.Errorf("%s is not on a source codeaf can post to", it.Ref())
	}
	if it.FollowUp != nil && it.FollowUp.Kind == "readiness" {
		if !factory.ReadinessPending(it) {
			return published{}, errors.New("final readiness review is incomplete; it cannot publish code or change draft status")
		}
		if TrailerCommit(trailer) != it.FollowUp.Readiness.Head {
			return published{}, publish.ErrCommitChanged
		}
		ready, ok := src.(interface {
			Ready(context.Context, factory.Item) (factory.PullRequest, error)
		})
		if !ok {
			return published{}, errors.New("this factory source does not support ready for review")
		}
		pr, e := ready.Ready(ctx, it)
		if e != nil {
			return published{}, e
		}
		return published{pr: &pr}, nil
	}
	var existingPR *factory.PullRequest
	if it.FollowUp != nil && it.FollowUp.State == "waiting_approval" {
		follower, ok := src.(factory.Follower)
		if !ok {
			return published{}, errors.New("cannot re-read the existing PR before updating it")
		}
		fresh, err := follower.Follow(ctx, it)
		if err != nil {
			return published{}, err
		}
		if it.PullRequest == nil || fresh.Number != it.PullRequest.Number || fresh.State != factory.PROpen || fresh.HeadSHA != it.FollowUp.HeadSHA {
			return published{}, errors.New("the existing PR closed or its head moved; review it before publishing the repair")
		}
		existingPR = &fresh
	}
	body := PublishBody(it, trailer)
	if isPull(it) {
		rcpt, err := src.Write(ctx, factory.Action{Verb: "comment", Item: it, Body: body})
		if err != nil {
			return published{}, err
		}
		url := strings.TrimSpace(it.URL)
		if url == "" {
			url = rcpt.URL
		}
		return published{pr: &factory.PullRequest{Number: it.Num, URL: url, Draft: it.Draft, State: factory.PROpen}}, nil
	}
	if r.opts.Workdir == nil {
		return published{}, publish.Wrap(fmt.Errorf("codeaf does not know where %s is checked out", it.Ref()), "worktree")
	}
	dir, err := r.opts.Workdir(it)
	if err != nil {
		return published{}, publish.Wrap(err, "worktree")
	}
	git := r.opts.Git
	if git == nil {
		git = ExecGit{}
	}
	// COMMIT-BOUND CONSENT NEVER COMMITS NEW WORK. The proof gate already
	// committed the reviewed work; a dirty or moved worktree needs approval again.
	if it.HeadCommit != "" {
		head, err := git.Run(dir, "rev-parse", "HEAD")
		if err != nil {
			return published{}, publish.Wrap(err, "worktree")
		}
		status, err := git.Run(dir, "status", "--porcelain")
		if err != nil {
			return published{}, publish.Wrap(err, "worktree")
		}
		approvedHead := TrailerCommit(trailer)
		if strings.TrimSpace(head) != approvedHead || strings.TrimSpace(status) != "" {
			if strings.TrimSpace(head) != it.HeadCommit {
				if err := r.opts.Store.Update(it.ID, func(saved *factory.Item) error { saved.HeadCommit = strings.TrimSpace(head); return nil }); err != nil {
					return published{}, err
				}
			}
			return published{}, publish.ErrCommitChanged
		}
	} else if err := commitApproved(git, dir, it, trailer); err != nil {
		return published{}, err
	}
	mode, err := publishMode(git, dir)
	if err != nil {
		return published{}, fmt.Errorf("could not compare %s's branch: %w", it.Ref(), err)
	}
	if mode == "comment" {
		rcpt, err := src.Write(ctx, factory.Action{Verb: "comment", Item: it, Body: body})
		if err != nil {
			return published{}, err
		}
		if strings.TrimSpace(rcpt.URL) == "" {
			return published{}, errors.New("the source posted no comment it could name")
		}
		return published{comment: rcpt.URL}, nil
	}
	if pushBranch == nil {
		return published{}, publish.Wrap(errors.New("this machine cannot push"), "push")
	}
	branch := branchFor(it)
	ref := branch
	if it.HeadCommit != "" {
		ref = it.HeadCommit + ":refs/heads/" + branch
	}
	if err := push(pushBranch, dir, it, ref); err != nil {
		return published{}, publish.Wrap(err, "push")
	}
	if it.FollowUp != nil && it.FollowUp.State == "waiting_approval" && it.PullRequest != nil {
		if updater, ok := src.(interface {
			UpdatePRBody(context.Context, factory.Item, string) error
		}); ok {
			if err := updater.UpdatePRBody(ctx, it, body); err != nil {
				return published{}, fmt.Errorf("repair pushed, but could not update the PR description: %w", err)
			}
		}
		pr := *it.PullRequest
		if existingPR != nil {
			pr = *existingPR
		}
		pr.HeadSHA = it.HeadCommit
		pr.ChecksRead = false
		pr.CheckRuns = nil
		pr.Checks = factory.PRChecks{}
		return published{pr: &pr}, nil
	}
	rcpt, err := src.Write(ctx, factory.Action{Verb: "pr", Item: it, Branch: branch, Body: body, Draft: true})
	if err != nil {
		return published{}, publish.Wrap(err, "pr")
	}
	n := rcpt.Number
	if n <= 0 {
		n = pullNumber(rcpt.URL)
	}
	if n <= 0 || strings.TrimSpace(rcpt.URL) == "" {
		return published{}, fmt.Errorf("the source opened no pull request it could name")
	}
	draft := true
	if it.PullRequest != nil {
		draft = it.PullRequest.Draft
	}
	return published{pr: &factory.PullRequest{Number: n, URL: rcpt.URL, Draft: draft, State: factory.PROpen}}, nil
}

// commitApproved commits everything the job left in its worktree as one
// commit, the job's title and the trailer its message. A worktree with
// nothing uncommitted is left as it is: its branch already holds the work.
// A machine with no git identity commits as the approving handle's GitHub
// noreply address.
func commitApproved(git Git, dir string, it factory.Item, trailer string) error {
	status, err := git.Run(dir, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("could not read %s's worktree: %w", it.Ref(), err)
	}
	if strings.TrimSpace(status) == "" {
		return nil
	}
	if _, err := git.Run(dir, "add", "-A"); err != nil {
		return fmt.Errorf("could not stage %s's work: %w", it.Ref(), err)
	}
	title := strings.TrimSpace(it.Title)
	if title == "" {
		title = "factory work on " + it.Ref()
	}
	handle := TrailerHandle(trailer)
	if handle == "" {
		handle = factory.NormalizeHandle(it.Person())
	}
	if handle == "" {
		handle = "codeaf"
	}
	args := []string{"-c", "user.name=" + handle, "-c", "user.email=" + handle + "@users.noreply.github.com"}
	args = append(args, "commit", "-q", "-m", title+"\n\n"+trailer)
	if _, err := git.Run(dir, args...); err != nil {
		return fmt.Errorf("could not commit %s's work: %w", it.Ref(), err)
	}
	return nil
}

// PublishBody is what a publish says on the forge: the proof summary, each
// claim, and the trailer as the last line.
func PublishBody(it factory.Item, trailer string) string {
	body := factory.PublishBody(it, trailer)
	return portablePublishText(body)
}

// pullNumber is the number at the end of a pull request's address.
func pullNumber(url string) int {
	i := strings.LastIndex(url, "/pull/")
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimRight(url[i+len("/pull/"):], "/"))
	if err != nil {
		return 0
	}
	return n
}

// publishMode measures both uncommitted work and commits not on the default
// branch. A clean worktree can still hold a commit, and an older default
// branch commit can be behind without holding any work to publish.
func publishMode(git Git, dir string) (string, error) {
	status, err := git.Run(dir, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) != "" {
		return "pr", nil
	}
	base, err := publishBase(git, dir)
	if err != nil {
		return "", err
	}
	count, err := git.Run(dir, "rev-list", "--count", base+"..HEAD", "--")
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(strings.TrimSpace(count))
	if err != nil || n < 0 {
		return "", errors.New("git returned no commit count")
	}
	if n == 0 {
		return "comment", nil
	}
	return "pr", nil
}

// publishBase prefers the remote's default, including a custom branch name.
// Small local repositories may have no remote HEAD yet; their main or master
// is the fallback. An unknown base refuses publishing rather than guessing
// that a job's own HEAD has nothing to contribute.
func publishBase(git Git, dir string) (string, error) {
	if ref, err := git.Run(dir, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil && strings.TrimSpace(ref) != "" {
		return strings.TrimSpace(ref), nil
	}
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master"} {
		if _, err := git.Run(dir, "rev-parse", "--verify", "--quiet", ref); err == nil {
			return ref, nil
		}
	}
	return "", errors.New("cannot find the default branch")
}

// publishPreview is the worker's proof-gate hint. Publishing measures again
// after committing, because work can change while the person reads the proof.
func (r *Runner) publishPreview(it factory.Item) string {
	if isPull(it) {
		return "pr"
	}
	if r.opts.Workdir == nil {
		return ""
	}
	dir, err := r.opts.Workdir(it)
	if err != nil {
		return ""
	}
	git := r.opts.Git
	if git == nil {
		git = ExecGit{}
	}
	mode, _ := publishMode(git, dir)
	return mode
}

// Home folder names are private machine context and never belong in a forge post.
var publishHomePath = regexp.MustCompile(`/((Users)|(home))/[^/\s` + "`" + `]+`)

func portablePublishText(s string) string {
	return publishHomePath.ReplaceAllStringFunc(s, func(string) string { return "$HOME" })
}
