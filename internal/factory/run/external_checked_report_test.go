package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestExternalHeadCompletionRejectsStaleOrMissingEvidence(t *testing.T) {
	for _, mode := range []string{"complete", "wrong reported head", "remote moved", "checked head moved", "material moved", "pending CI", "missing report", "draft", "missing material", "critical decision"} {
		t.Run(mode, func(t *testing.T) {
			head := strings.Repeat("a", 40)
			report := &factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "no_change", Reason: "Full current-head review completed", Readiness: &factory.ReadinessReview{Head: head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}
			it := factory.Item{State: factory.StateRunning, Cap: 10, HeadCommit: head, PullRequest: &factory.PullRequest{Number: 7, State: factory.PROpen, HeadSHA: head, ChecksRead: true, ReviewsRead: true, Mergeable: factory.MergeableYes, CI: "ci ✓", Checks: factory.PRChecks{Passed: 1}, ReadinessKey: "snapshot"}, FollowUp: &factory.PRFollowUp{Kind: "readiness", State: "healthy", Readiness: &factory.PRReadiness{External: true, Head: head, Key: "snapshot", State: "checked"}}, Stream: &factory.Stream{Spent: 3, Phases: []factory.Phase{{Name: factory.FollowPRStep, State: factory.PhaseWaiting, Result: report}}}}
			switch mode {
			case "wrong reported head":
				report.Repair.Readiness.Head = strings.Repeat("b", 40)
			case "remote moved":
				it.PullRequest.HeadSHA = strings.Repeat("b", 40)
			case "checked head moved":
				it.HeadCommit = strings.Repeat("b", 40)
			case "material moved":
				it.PullRequest.ReadinessKey = "different"
			case "pending CI":
				it.PullRequest.Checks.Pending = 1
			case "missing report":
				it.Stream.Phases[0].Result = nil
			case "draft":
				it.PullRequest.Draft = true
			case "missing material":
				it.PullRequest.ReadinessKey = ""
			case "critical decision":
				it.Question, it.QKind = "Choose the public API", "scope"
			}
			err := FinishReport(&it, factory.StateShipped, time.Now())
			if mode != "complete" {
				if err == nil {
					t.Fatal("stale or incomplete evidence completed the PR review")
				}
				return
			}
			if err != nil || it.State != factory.StateShipped || it.Approval != nil || it.Publishing || it.PublishStatus != nil || it.Stream.Spent != 3 {
				t.Fatalf("inspection demanded publication or changed spend: %v", err)
			}
		})
	}
}
