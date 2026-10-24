package factory

import (
	"fmt"
	"strings"
	"time"
)

// PRReadiness is the durable review of one exact remote snapshot. The key
// includes the head, base, description, diff, checks and review feedback.
type PRReadiness struct {
	External bool      `json:"external,omitempty"`
	Key      string    `json:"key"`
	Head     string    `json:"head"`
	State    string    `json:"state"`
	Summary  string    `json:"summary,omitempty"`
	At       time.Time `json:"at,omitempty"`
}
type ReadinessReview struct {
	Head               string `json:"head"`
	DiffReviewed       bool   `json:"diff_reviewed"`
	DescriptionMatches bool   `json:"description_matches"`
	FeedbackAddressed  bool   `json:"feedback_addressed"`
	NoUnfinishedWork   bool   `json:"no_unfinished_work"`
}

func ReadinessGate(it Item) error {
	if it.Question != "" || it.Asking != nil {
		return fmt.Errorf("a pending decision must be resolved before readiness can complete")
	}
	p := it.PullRequest
	if it.Manual || it.DoneHow == "dropped" || it.DoneHow == "closed_locally" {
		return fmt.Errorf("automation is paused or the job was dropped")
	}
	if p == nil || p.State != PROpen || p.HeadSHA == "" {
		return fmt.Errorf("an open PR and its exact head are required")
	}
	if len(p.Problems) > 0 {
		return fmt.Errorf("%s", p.Problems[0].Summary)
	}
	if !p.ChecksRead || !p.ReviewsRead || p.Mergeable != MergeableYes || p.Conflict {
		return fmt.Errorf("PR checks, reviews or mergeability are incomplete or blocked")
	}
	if p.Checks.Passed < 1 || p.Checks.Failed > 0 || p.Checks.Pending > 0 || p.CI != "ci ✓" {
		return fmt.Errorf("current-head CI must pass with no pending checks")
	}
	// Readiness reviews the feedback itself. A reviewer's approving vote is
	// a merge requirement, never a prerequisite for requesting their review.
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
	if why := ReadinessReportFault(it.FollowUp.Readiness.Head, r); why != "" {
		return fmt.Errorf("%s", why)
	}
	if (!it.FollowUp.Readiness.External && r.Repair.Readiness.Head != it.HeadCommit) || r.Repair.Readiness.Head != it.PullRequest.HeadSHA {
		return fmt.Errorf("the checkout or remote PR head changed after the final review")
	}
	return nil
}

// ReadinessReportFault is shared by the reporting door and the final gate.
// Schema omissions are returned while the model can still correct them; live
// PR facts are separately revalidated before the recorded ready transition.
func ReadinessReportFault(head string, r StageResult) string {
	v := r.Repair
	if v == nil || v.Decision != "no_change" || strings.TrimSpace(v.Reason) == "" {
		return "final review needs repair.decision no_change and an evidence-based reason; report needs_repair for fixable findings or needs_you for a critical decision"
	}
	if !r.Done || r.Exit != 0 {
		return "final review did not finish successfully; resolve the reported execution problem and report again"
	}
	if r.Findings != 0 {
		return "final review has unresolved findings; report needs_repair with the specific findings"
	}
	if v.Readiness == nil {
		return "include repair.readiness with the full captured head, diff_reviewed, description_matches, feedback_addressed and no_unfinished_work"
	}
	a := v.Readiness
	if a.Head == "" || a.Head != head {
		return "repair.readiness.head must equal the full captured PR head " + head
	}
	if !a.DiffReviewed || !a.DescriptionMatches || !a.FeedbackAddressed || !a.NoUnfinishedWork {
		return "complete the diff, description, feedback and unfinished-work checks; report needs_repair if a check finds a blocker"
	}
	return ""
}
