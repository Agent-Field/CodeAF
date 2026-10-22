package factory

import (
	"strings"
	"testing"
	"time"
)

func readyItem() Item {
	head := strings.Repeat("a", 40)
	return Item{Kind: KindIssue, State: StateShipped, Publish: "pr", Cap: 10, HeadCommit: head, Autonomy: AutonomyAuto,
		PullRequest: &PullRequest{Number: 7, State: PROpen, Draft: true, HeadSHA: head, Mergeable: MergeableYes, ChecksRead: true, ReviewsRead: true, CI: "ci ✓", Checks: PRChecks{Passed: 1}, CheckRuns: []CheckRun{{Name: "test", State: "success"}}, ReadinessKey: "snapshot", ReadinessContext: "complete diff and body"},
		FollowUp:    &PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}, Stream: &Stream{}}
}
func TestReadinessQueueAndChangedFacts(t *testing.T) {
	it := readyItem()
	it.Proof = []Claim{{Text: "original verified behavior", OK: true, Evidence: "recorded test receipt"}}
	if !QueueCIFollowUp(&it, time.Now()) || it.State != StateQueued || it.FollowUp.Kind != "readiness" || it.FollowUp.Details != "complete diff and body" {
		t.Fatal("healthy draft did not queue a final review")
	}
	if len(it.Proof) != 1 || it.Proof[0].Text != "original verified behavior" {
		t.Fatal("readiness discarded original proof")
	}
	a := it.FollowUp.Readiness
	a.State = "reviewed"
	it.FollowUp.State = "ready_approval"
	it.State = StateLanded
	it.Approval = &Approval{Kind: "proof", State: "approving", Automatic: true}
	it.PullRequest.ReadinessKey = "changed-description"
	if !QueueCIFollowUp(&it, time.Now()) || it.Approval != nil || it.FollowUp.Readiness.Key != "changed-description" || it.State != StateQueued {
		t.Fatal("old approval survived changed review material")
	}
}
func TestReadinessGateNeverTreatsUnknownAsGreen(t *testing.T) {
	cases := map[string]func(*Item){
		"takeover": func(i *Item) { i.Manual = true }, "no checks": func(i *Item) { i.PullRequest.ChecksRead = false }, "no reviews": func(i *Item) { i.PullRequest.ReviewsRead = false },
		"zero passed": func(i *Item) { i.PullRequest.Checks.Passed = 0 }, "pending": func(i *Item) { i.PullRequest.Checks.Pending = 1 }, "failed": func(i *Item) { i.PullRequest.Checks.Failed = 1 },
		"combined pending": func(i *Item) { i.PullRequest.CI = "ci …" }, "unknown mergeability": func(i *Item) { i.PullRequest.Mergeable = "" }, "conflict": func(i *Item) { i.PullRequest.Conflict = true },
		"unknown check": func(i *Item) { i.PullRequest.CheckRuns[0].State = "unknown" }, "budget": func(i *Item) { i.Cap = 0 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			it := readyItem()
			change(&it)
			if ReadinessGate(it) == nil {
				t.Fatal("unsafe all-clear")
			}
			QueueCIFollowUp(&it, time.Now())
			if it.FollowUp.Kind == "readiness" {
				t.Fatal("queued unsafe readiness")
			}
		})
	}
}

func TestReadinessDoesNotRequireAHumanApprovingReview(t *testing.T) {
	for _, decision := range []string{"", "review_required", "changes_requested"} {
		it := readyItem()
		it.PullRequest.Required, it.PullRequest.ReviewDecision = 2, decision
		if err := ReadinessGate(it); err != nil {
			t.Fatalf("%s blocked final code review: %v", decision, err)
		}
		// Previously handled feedback is still included in the final review;
		// the reviewer can retain their request until they inspect the update.
		if decision == "changes_requested" {
			if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.Kind != "pr" {
				t.Fatal("fresh review request was ignored")
			}
			it.State, it.FollowUp.State = StateShipped, "healthy"
		}
		if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.Kind != "readiness" {
			t.Fatalf("%s never reached final review", decision)
		}
	}
}
func TestReadinessReviewRequiresExplicitCompleteEvidence(t *testing.T) {
	it := readyItem()
	QueueCIFollowUp(&it, time.Now())
	res := StageResult{Done: true, Repair: &CIRepairResult{Decision: "no_change", Reason: "checked current diff and body", Readiness: &ReadinessReview{Head: it.HeadCommit, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}
	if e := ValidateReadinessReview(it, res); e != nil {
		t.Fatal(e)
	}
	res.Repair.Readiness.DescriptionMatches = false
	if ReportFault(Stage{Name: FollowPRStep, Kind: StageChat, Until: UntilProven}, res) == "" {
		t.Fatal("incomplete readiness report not returned for correction")
	}
	if ValidateReadinessReview(it, res) == nil {
		t.Fatal("description mismatch passed")
	}
	res.Repair.Readiness.DescriptionMatches = true
	res.Findings = 1
	if ValidateReadinessReview(it, res) == nil {
		t.Fatal("unfinished finding passed")
	}
}
func TestReadinessExternalReadyEndsTheGateAndKeepsMonitoring(t *testing.T) {
	it := readyItem()
	QueueCIFollowUp(&it, time.Now())
	it.FollowUp.Readiness.State = "reviewed"
	it.FollowUp.State = "ready_approval"
	it.State = StateLanded
	it.Approval = &Approval{Kind: "proof", State: "waiting"}
	it.PullRequest.Draft = false
	if !QueueCIFollowUp(&it, time.Now()) || it.Approval != nil || it.State != StateShipped || it.FollowUp.Readiness.State != "ready" {
		t.Fatal("external ready not recognized")
	}
	it.PullRequest.Feedback = "new request"
	if !QueueCIFollowUp(&it, time.Now()) || it.State != StateQueued || it.FollowUp.Kind != "pr" {
		t.Fatal("monitoring stopped after ready")
	}
}
