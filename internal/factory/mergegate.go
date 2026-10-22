package factory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ── MERGING: ONE GATE, IN CODE ───────────────────────────────────────────
//
// A MERGE IS A PERSON OR A RULE, NEVER A MODEL. Nothing on any tool belt
// merges. A merge happens only when a person presses Merge on a job whose pull
// request the factory opened, or when that job's pinned pipeline turned
// auto-merge on and every fact below already holds. Both roads pass through
// [MergeGate], and the gate is a pure function of the job as stored: the same
// facts always give the same answer, so a test can pin every refusal.
//
// THE GATE FAILS CLOSED. A fact it cannot read (the pull request's head, its
// mergeability, its checks) refuses the merge and says which fact is missing.
// It never reads an unknown as a yes.
//
// The gate runs twice: on the factory when the merge is asked for, against
// the facts its last follow read, and again on the runner that carries the
// merge out, against facts it has just read from the forge. The forge is then
// handed the exact commit to merge, so a head that moves after both checks is
// refused by the forge as well.

// MergeUndo is how long a pressed Merge waits, with Undo, before it is sent.
const MergeUndo = 5 * time.Second

// Merge methods a pipeline may name. "" lets the forge's own repository
// settings choose: the first of merge, squash and rebase that it allows.
const (
	MergeMethodMerge  = "merge"
	MergeMethodSquash = "squash"
	MergeMethodRebase = "rebase"
)

// MergeMethods are the words `merge.method` reads, in the order the forge's
// repository settings are tried when the pipeline names none.
var MergeMethods = []string{MergeMethodMerge, MergeMethodSquash, MergeMethodRebase}

// MergePolicy is a pipeline's `merge:` settings. AUTO-MERGE IS OFF unless the
// pipeline turns it on, and a personal file may only turn it off.
type MergePolicy struct {
	Auto   bool   `json:",omitempty"`
	Method string `json:",omitempty"`
}

// Merge states while a merge is under way. A merge that worked leaves no
// state behind: the pull request reads merged, and the job is done.
const (
	// MergeAsked is a pressed Merge inside its Undo window.
	MergeAsked = "asked"
	// MergeQueued is a merge waiting for the runner that carries it out.
	MergeQueued = "queued"
	// MergeMerging is the runner talking to the forge.
	MergeMerging = "merging"
	// MergeFailed is a merge the gate or the forge refused when it was sent.
	MergeFailed = "failed"
)

// MergeRun is one merge, from the moment it is asked for until the runner
// reports what the forge did. By is the person who pressed Merge, or "" when
// the pipeline's auto-merge asked; Rule says which, in words Activity shows.
// Commit is the exact head the gate passed, and the only commit the forge is
// asked to merge.
type MergeRun struct {
	State     string    `json:"state"`
	By        string    `json:"by,omitempty"`
	Auto      bool      `json:"auto,omitempty"`
	Rule      string    `json:"rule,omitempty"`
	Commit    string    `json:"commit,omitempty"`
	Method    string    `json:"method,omitempty"`
	At        time.Time `json:"at"`
	UndoUntil time.Time `json:"undo_until,omitempty"`
	Why       string    `json:"why,omitempty"`
}

// MergeAsk is who is asking. By is a forge handle when a person pressed
// Merge; Auto is the pipeline's auto-merge, which no person pressed.
type MergeAsk struct {
	By   string
	Auto bool
}

// MergeVerdict is the gate's answer. Rule names what allowed the merge, and Why
// what refused it, each in the words Activity and the job's header show.
type MergeVerdict struct {
	OK     bool
	Rule   string
	Why    string
	Commit string
}

func refuseMerge(why string) MergeVerdict { return MergeVerdict{Why: why} }

// Follower is a source that can read one of its items' pull requests again,
// fresh. The runner carrying a merge out reads the forge through it right
// before the gate's second check, so that check never runs on a stale read.
type Follower interface {
	Follow(ctx context.Context, it Item) (PullRequest, error)
}

// MergeGate decides whether the job's pull request may be merged now. It reads
// only the job; the caller is responsible for the facts being fresh enough
// for the moment it asks.
func MergeGate(it Item, ask MergeAsk) MergeVerdict {
	pr := it.PullRequest
	switch {
	case it.Kind == KindPR:
		// CODEAF NEVER MERGES SOMEONE ELSE'S PULL REQUEST. A pull request job
		// reviews another person's change; merging it is theirs to do.
		return refuseMerge("codeaf merges only pull requests it opened")
	case pr == nil || pr.Number <= 0 || it.Publish != "pr":
		return refuseMerge("this job has opened no pull request")
	case it.DoneHow == "closed_locally":
		return refuseMerge("this job is closed locally; Reopen before merging")
	case it.DoneHow == "dropped":
		return refuseMerge("this job was dropped")
	case pr.State == PRMerged:
		return refuseMerge("already merged")
	case pr.State == PRClosed:
		return refuseMerge("the pull request is closed")
	case pr.State != PROpen:
		return refuseMerge("cannot read whether the pull request is open")
	}
	if m := it.Merging; m != nil && (m.State == MergeQueued || m.State == MergeMerging) {
		return refuseMerge("a merge is already on its way")
	}
	if ask.Auto {
		if it.MergePolicy == nil || !it.MergePolicy.Auto {
			return refuseMerge("auto-merge is off in this job's pipeline")
		}
	} else if NormalizeHandle(ask.By) == "" {
		return refuseMerge("a merge needs the person who pressed it")
	}
	if it.FollowUp != nil && (it.FollowUp.State == "queued" || it.FollowUp.State == "running" || it.FollowUp.State == "waiting_approval" || it.FollowUp.State == "needs_you") {
		return refuseMerge("CI follow-up must finish and its verified changes must be approved")
	}
	approved := strings.TrimSpace(it.HeadCommit)
	head := strings.TrimSpace(pr.HeadSHA)
	switch {
	case approved == "":
		return refuseMerge("no approved commit is recorded for this job")
	case head == "":
		return refuseMerge("cannot read the pull request's head commit")
	case !strings.EqualFold(head, approved):
		// THE APPROVED CODE IS EXACTLY WHAT MERGES. A push after the proof was
		// approved needs the proof approved again.
		return refuseMerge("the pull request has commits you did not approve")
	}
	if pr.Draft {
		return refuseMerge("the pull request is a draft")
	}
	switch pr.Mergeable {
	case MergeableNo:
		return refuseMerge("the pull request has conflicts")
	case MergeableYes:
	default:
		return refuseMerge("the forge has not said whether it can merge yet")
	}
	if !pr.ChecksRead {
		return refuseMerge("cannot read the pull request's checks")
	}
	if n := pr.Checks.Failed; n > 0 {
		return refuseMerge(plural(n, "check is", "checks are") + " failing")
	}
	if n := pr.Checks.Pending; n > 0 {
		return refuseMerge(plural(n, "check is", "checks are") + " still running")
	}
	if ask.Auto && pr.Checks.Passed == 0 {
		// A repository with no checks may still be merged by a person, who can
		// see that; auto-merge waits for at least one passing check.
		return refuseMerge("auto-merge waits for at least one passing check")
	}
	if !pr.ReviewsRead {
		return refuseMerge("cannot read the pull request's reviews")
	}
	switch {
	case pr.ReviewDecision == "changes_requested":
		return refuseMerge("a reviewer asked for changes")
	case pr.Required > 0 && pr.ReviewDecision != "approved":
		return refuseMerge("the branch needs " + plural(pr.Required, "approving review", "approving reviews"))
	}
	if ask.Auto {
		total, shown := 0, 0
		for _, c := range append(append([]Claim(nil), it.Proof...), it.Policy...) {
			total++
			if c.OK {
				shown++
			}
		}
		if total == 0 || shown < total {
			return refuseMerge(fmt.Sprintf("auto-merge needs every claim proven; %d of %d are", shown, total))
		}
		return MergeVerdict{OK: true, Commit: head, Rule: "auto-merge is on in this job's pipeline"}
	}
	return MergeVerdict{OK: true, Commit: head, Rule: "merged by @" + NormalizeHandle(ask.By)}
}

// MergeMerged is a runner's report that the forge merged the pull request.
// It is never stored as a job's state: the factory clears the merge when it
// reads the report, and the pull request reads merged.
const MergeMerged = "merged"

// MergeReport is what the runner carrying a merge out says it did: merging
// (it has begun), merged (SHA is the merge commit) or failed (Why says what
// refused it). PR is the pull request as the runner last read it.
type MergeReport struct {
	State string
	SHA   string
	Why   string
	PR    *PullRequest
}

// Mergeable words a pull request carries. "" is not yet known.
const (
	MergeableYes = "yes"
	MergeableNo  = "no"
)

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
