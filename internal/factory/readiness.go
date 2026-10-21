package factory

import (
	"fmt"
	"strings"
	"time"
)

// PRReadiness is the durable review of one exact remote snapshot. The key
// includes the head, base, description, diff, checks and review feedback.
type PRReadiness struct {
	Key     string    `json:"key"`
	Head    string    `json:"head"`
	State   string    `json:"state"`
	Summary string    `json:"summary,omitempty"`
	At      time.Time `json:"at,omitempty"`
}
type ReadinessReview struct {
	Head               string `json:"head"`
	DiffReviewed       bool   `json:"diff_reviewed"`
	DescriptionMatches bool   `json:"description_matches"`
	FeedbackAddressed  bool   `json:"feedback_addressed"`
	NoUnfinishedWork   bool   `json:"no_unfinished_work"`
}

func ReadinessGate(it Item) error {
	p := it.PullRequest
	if it.Manual || it.DoneHow == "dropped" {
		return fmt.Errorf("automation is paused or the job was dropped")
	}
	if p == nil || p.State != PROpen || p.HeadSHA == "" {
		return fmt.Errorf("an open PR and its exact head are required")
	}
	if !p.ChecksRead || !p.ReviewsRead || p.Mergeable != MergeableYes || p.Conflict {
		return fmt.Errorf("PR checks, reviews or mergeability are incomplete or blocked")
	}
	if p.Checks.Passed < 1 || p.Checks.Failed > 0 || p.Checks.Pending > 0 || p.CI != "ci ✓" {
		return fmt.Errorf("current-head CI must pass with no pending checks")
	}
	if p.ReviewDecision == "changes_requested" {
		return fmt.Errorf("a requested-changes review is still outstanding")
	}
	for _, r := range p.CheckRuns {
		if r.State != "success" && r.State != "neutral" && r.State != "skipped" {
			return fmt.Errorf("check %s has not passed", r.Name)
		}
	}
	if it.Cap <= 0 || JobSpent(it) >= it.Cap {
		return fmt.Errorf("job budget reached or unavailable")
	}
	return nil
}

func ReadinessPending(it Item) bool {
	return it.FollowUp != nil && it.FollowUp.Kind == "readiness" && it.FollowUp.Readiness != nil && it.FollowUp.Readiness.State == "reviewed"
}

func ReadinessApprovalWords(it Item) string {
	if Automatic(it) {
		return "Final review passed. Auto will mark the existing draft PR ready for review after rechecking its head, description, checks and feedback. Monitoring continues; merge remains your decision."
	}
	return "Final review passed. Approval needed: mark the existing draft PR ready for review. This will notify reviewers; it does not push code or merge. Press Approve to continue."
}

func ValidateReadinessReview(it Item, r StageResult) error {
	if err := ReadinessGate(it); err != nil {
		return err
	}
	if it.FollowUp == nil || it.FollowUp.Readiness == nil || it.FollowUp.Readiness.Key == "" {
		return fmt.Errorf("no complete readiness snapshot was captured")
	}
	if it.PullRequest.ReadinessKey != "" && it.PullRequest.ReadinessKey != it.FollowUp.Readiness.Key {
		return fmt.Errorf("readiness facts changed during the review")
	}
	v := r.Repair
	if v == nil || v.Decision != "no_change" || strings.TrimSpace(v.Reason) == "" || v.Readiness == nil || !r.Done || r.Exit != 0 || r.Findings != 0 {
		return fmt.Errorf("readiness requires an explicit complete final review with no unresolved findings")
	}
	a := v.Readiness
	if a.Head != it.FollowUp.Readiness.Head || a.Head != it.HeadCommit || a.Head != it.PullRequest.HeadSHA || !a.DiffReviewed || !a.DescriptionMatches || !a.FeedbackAddressed || !a.NoUnfinishedWork {
		return fmt.Errorf("final review must confirm the exact PR head, diff, description, addressed feedback and no unfinished work")
	}
	return nil
}
