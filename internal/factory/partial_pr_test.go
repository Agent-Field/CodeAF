package factory

import (
	"testing"
	"time"
)

func TestPartialPRBlocksReadinessButAllowsKnownRepairs(t *testing.T) {
	newItem := func() Item {
		return Item{Kind: KindIssue, State: StateShipped, Cap: 10, HeadCommit: "head", PullRequest: &PullRequest{Number: 7, State: PROpen, Draft: true, HeadSHA: "head", Mergeable: MergeableYes, CI: "ci ✓", ChecksRead: true, ReviewsRead: true, Checks: PRChecks{Passed: 8}, Problems: []PRReadProblem{{Area: "readiness", Summary: "Policy access denied"}}}, FollowUp: &PRFollowUp{State: "healthy", Limit: 3}}
	}
	it := newItem()
	if ReadinessGate(it) == nil {
		t.Fatal("incomplete policy allowed readiness")
	}
	if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.State != "watching" || it.FollowUp.Reason != "Ready for review blocked: Policy access denied" {
		t.Fatal("incomplete read remained healthy")
	}
	if QueueCIFollowUp(&it, time.Now()) {
		t.Fatal("same problem changed state repeatedly")
	}
	for _, kind := range []string{"ci", "comment", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			it := newItem()
			switch kind {
			case "ci":
				it.PullRequest.Checks.Failed = 1
			case "comment":
				it.PullRequest.Feedback = "Please fix race"
			case "conflict":
				it.PullRequest.Conflict = true
			}
			if !QueueCIFollowUp(&it, time.Now()) || it.State != StateQueued || it.FollowUp.Kind == "readiness" {
				t.Fatal("independent known repair was blocked by policy error")
			}
		})
	}
}

func TestPartialPRMonitoringTransitionsIgnoreRepairDedupe(t *testing.T) {
	it := Item{Kind: KindIssue, State: StateShipped, Cap: 10, PullRequest: &PullRequest{Number: 7, State: PROpen, HeadSHA: "head", CI: "ci ✓", ChecksRead: true, ReviewsRead: true, Checks: PRChecks{Passed: 8}}, FollowUp: &PRFollowUp{State: "healthy", Kind: "pr", Limit: 3}}
	it.FollowUp.EventKey = followEvent(it.PullRequest, "")
	it.PullRequest.ReviewsRead = false
	it.PullRequest.Problems = []PRReadProblem{{Area: "review policy", Code: "access_denied", Summary: "Policy access denied"}}
	if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.State != "watching" || it.FollowUp.Reason != "Policy access denied" {
		t.Fatal("event dedupe swallowed monitor failure")
	}
	if QueueCIFollowUp(&it, time.Now()) {
		t.Fatal("unchanged error churned state")
	}
	it.PullRequest.ReviewsRead = true
	it.PullRequest.Problems = nil
	if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.State != "healthy" {
		t.Fatal("event dedupe swallowed recovery")
	}
	if QueueCIFollowUp(&it, time.Now()) {
		t.Fatal("unchanged recovery churned state")
	}
}

func TestIncompleteFeedbackReadStartsNoRepairRound(t *testing.T) {
	now := time.Now()
	complete := func() *PullRequest {
		return &PullRequest{Number: 7, State: PROpen, HeadSHA: "h1", BaseSHA: "b1", ChecksRead: true, ReviewsRead: true, CI: "ci ✓",
			Checks: PRChecks{Passed: 1}, CheckRuns: []CheckRun{{Name: "ci", State: "success"}},
			ReviewDecision: "changes_requested", Reviews: []PRReview{{By: "bob", State: "changes_requested", Body: "rename X"}},
			Feedback: "PR conversation comment 1 by @bob: please rename X\n"}
	}
	it := Item{Kind: KindIssue, State: StateShipped, Autonomy: AutonomyAuto, Cap: 10, Publish: "pr",
		FollowUp: &PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}, PullRequest: complete()}
	if !QueueCIFollowUp(&it, now) || it.FollowUp.State != "queued" || it.FollowUp.Attempt != 1 {
		t.Fatalf("first feedback: %+v", *it.FollowUp)
	}
	it.State, it.FollowUp.State = StateShipped, "healthy"
	for _, area := range []string{"reviews", "review policy", "feedback"} {
		partial := complete()
		// Only the part whose read failed is missing; the rest is as complete as before.
		partial.ReviewsRead = false
		if area == "feedback" {
			partial.Feedback = ""
		} else {
			partial.Reviews = nil
		}
		partial.Problems = []PRReadProblem{{Area: area, Code: "transient", Retryable: true}}
		it.PullRequest = partial
		QueueCIFollowUp(&it, now)
		if it.FollowUp.State == "queued" || it.FollowUp.Attempt > 1 {
			t.Fatalf("%s read blip queued a repair: %+v", area, *it.FollowUp)
		}
		it.PullRequest = complete()
		QueueCIFollowUp(&it, now)
		if it.FollowUp.State == "queued" || it.FollowUp.Attempt > 1 {
			t.Fatalf("recovery after a %s blip queued a repair: %+v", area, *it.FollowUp)
		}
	}
	edited := complete()
	edited.Feedback = "PR conversation comment 1 by @bob: please rename X and Y\n"
	it.PullRequest = edited
	if !QueueCIFollowUp(&it, now) || it.FollowUp.State != "queued" {
		t.Fatalf("genuinely new feedback ignored: %+v", *it.FollowUp)
	}
}

func TestIndependentRepairDuringPartialFeedbackKeepsHandledReviewKey(t *testing.T) {
	for _, trigger := range []string{"CI", "conflict"} {
		t.Run(trigger, func(t *testing.T) {
			now := time.Now()
			pr := &PullRequest{Number: 7, State: PROpen, HeadSHA: "h1", BaseSHA: "b1", ChecksRead: true, ReviewsRead: true,
				Checks: PRChecks{Passed: 1}, Feedback: "PR comment 1: rename X"}
			it := Item{Kind: KindIssue, State: StateShipped, Autonomy: AutonomyAuto, Cap: 10, Publish: "pr",
				FollowUp: &PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}, PullRequest: pr}
			if !QueueCIFollowUp(&it, now) {
				t.Fatal("initial feedback was not queued")
			}
			key := it.FollowUp.ReviewKey
			it.State, it.FollowUp.State = StateShipped, "healthy"
			pr.Feedback, pr.ReviewsRead = "", false
			pr.Problems = []PRReadProblem{{Area: "feedback", Code: "transient", Retryable: true}}
			if trigger == "CI" {
				pr.Checks = PRChecks{Failed: 1}
			} else {
				pr.Conflict = true
			}
			if !QueueCIFollowUp(&it, now) || it.FollowUp.State != "queued" || it.FollowUp.Attempt != 2 {
				t.Fatalf("known %s problem was not queued: %+v", trigger, it.FollowUp)
			}
			if it.FollowUp.ReviewKey != key {
				t.Fatal("partial feedback replaced the last handled review key")
			}
			it.State, it.FollowUp.State = StateShipped, "healthy"
			pr.Feedback, pr.ReviewsRead, pr.Problems = "PR comment 1: rename X", true, nil
			pr.Checks, pr.Conflict = PRChecks{Passed: 1}, false
			QueueCIFollowUp(&it, now)
			if it.FollowUp.State == "queued" || it.FollowUp.Attempt > 2 {
				t.Fatalf("restored handled feedback queued duplicate work: %+v", it.FollowUp)
			}
		})
	}
}
