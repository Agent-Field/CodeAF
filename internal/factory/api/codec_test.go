package api

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestPRFeedbackTrackingAndCheckSourcesRoundTrip(t *testing.T) {
	p := &factory.PullRequest{FeedbackTracked: true, FeedbackItems: []factory.PRFeedback{{ID: "inline:1", Revision: "r1", Resolved: false}}, CheckRuns: []factory.CheckRun{{Source: "status", Name: "license/cla", State: "in_progress"}}}
	got := toPR(fromPR(p))
	if !got.FeedbackTracked || len(got.FeedbackItems) != 1 || got.FeedbackItems[0].ID != "inline:1" || got.CheckRuns[0].Source != "status" {
		t.Fatal("PR feedback or check provenance was lost")
	}
}

func TestVerificationPausePresentationNamesItsActualRecovery(t *testing.T) {
	j := Job{State: StateNeedsYou, FollowUp: &factory.PRFollowUp{State: "needs_you", BlockedBy: "verification", Reason: "Invalid test command"}, NeedsYou: &NeedsYou{Kind: factory.NeedsOutOfAttempts}}
	PresentJob(&j)
	if j.StatePresentation.Title != "Verification needs retry" {
		t.Fatal("verification problem labelled as a generic stuck job")
	}
}
