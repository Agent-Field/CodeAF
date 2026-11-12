package factory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func trackedJob() Item {
	it := Item{Kind: KindIssue, State: StateShipped, Autonomy: AutonomyAuto, Cap: 30, HeadCommit: strings.Repeat("a", 40), Publish: "pr", PullRequest: &PullRequest{State: PROpen, HeadSHA: strings.Repeat("a", 40), ChecksRead: true, ReviewsRead: true, Checks: PRChecks{Pending: 1}, FeedbackTracked: true, Feedback: "A finding", FeedbackItems: []PRFeedback{{ID: "inline:1", Revision: "v1"}}}}
	WatchPublishedPR(&it)
	return it
}
func TestTrackedFeedbackAcknowledgedOnlyAfterSuccessAndSurvivesRestart(t *testing.T) {
	now := time.Now()
	it := trackedJob()
	if !QueueCIFollowUp(&it, now) || it.FollowUp.Attempt != 1 {
		t.Fatal("new finding not queued")
	}
	if len(it.FollowUp.Reviewed) != 0 {
		t.Fatal("queued finding prematurely acknowledged")
	}
	it.State = StateShipped
	WatchPublishedPR(&it)
	raw, _ := json.Marshal(it)
	var restored Item
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	QueueCIFollowUp(&restored, now)
	if restored.FollowUp.State == "queued" {
		t.Fatal("unchanged comment repeated after restart")
	}
	restored.PullRequest.Feedback = "same body at a different line"
	restored.PullRequest.FeedbackItems[0].Resolved = true
	QueueCIFollowUp(&restored, now)
	if restored.FollowUp.State == "queued" {
		t.Fatal("resolution manufactured work")
	}
	restored.PullRequest.FeedbackItems[0].Resolved = false
	restored.PullRequest.FeedbackItems[0].Revision = "v2"
	if !QueueCIFollowUp(&restored, now) || restored.FollowUp.State != "queued" {
		t.Fatal("edited concern ignored")
	}
}
func TestPausedRepairKeepsMonitoringAndNewFeedbackWakesOnlyRecoverableAuto(t *testing.T) {
	for _, mode := range []string{"auto", "manual", "takeover", "critical", "stopped", "budget", "closed"} {
		t.Run(mode, func(t *testing.T) {
			it := trackedJob()
			QueueCIFollowUp(&it, time.Now())
			it.State = StateNeedsYou
			it.FollowUp.State = "needs_you"
			it.FollowUp.BlockedBy = "verification"
			it.QKind = QKindOutOfAttempts
			it.Question = "verification blocked"
			it.Asking = &Asked{Stage: FollowPRStep}
			switch mode {
			case "manual":
				it.Autonomy = AutonomyManual
			case "takeover":
				it.Manual = true
			case "critical":
				it.QKind = "scope"
				it.Asking = &Asked{Stage: FollowPRStep, Question: "choose policy"}
			case "stopped":
				it.Failure = &Failure{Code: CodeStopped}
			case "budget":
				it.Cap = 0
			case "closed":
				it.DoneHow = "closed_locally"
			}
			it.PullRequest.FeedbackItems = append(it.PullRequest.FeedbackItems, PRFeedback{ID: "inline:2", Revision: "v1"})
			QueueCIFollowUp(&it, time.Now())
			if (it.State == StateQueued) != (mode == "auto") {
				t.Fatalf("state %s", it.State)
			}
			if mode != "closed" && it.FollowUp.Monitoring == "" {
				t.Fatal("monitoring disappeared while blocked")
			}
		})
	}
}
func TestPendingCLAIsAnExternalGateAndDoesNotStartCodeRepair(t *testing.T) {
	it := trackedJob()
	it.PullRequest.FeedbackItems = []PRFeedback{{ID: "conversation:1", Revision: "v1", External: true}}
	it.PullRequest.Feedback = "CLA required"
	it.PullRequest.CheckRuns = []CheckRun{{Name: "license/cla", State: "in_progress", URL: "https://cla-assistant.io/org/repo"}}
	QueueCIFollowUp(&it, time.Now())
	if it.FollowUp.Attempt != 0 || it.State != StateShipped || !strings.Contains(it.FollowUp.Reason, "Contributor agreement pending") {
		t.Fatalf("external gate treated as repair: %+v", it.FollowUp)
	}
}

func TestTrackedFeedbackDoesNotHidePartialMonitoringOrChurnItsState(t *testing.T) {
	it := trackedJob()
	it.Manual = true
	it.FollowUp.State = "needs_you"
	it.PullRequest.Problems = []PRReadProblem{{Area: "checks", Summary: "Checks unavailable"}}
	if !QueueCIFollowUp(&it, time.Now()) || !strings.Contains(it.FollowUp.Monitoring, "Checks unavailable") || it.FollowUp.PendingFeedback != 1 {
		t.Fatalf("known feedback hid incomplete monitoring: %+v", it.FollowUp)
	}
	if QueueCIFollowUp(&it, time.Now()) {
		t.Fatal("unchanged partial monitoring churned state")
	}
	it.PullRequest.Problems = nil
	if !QueueCIFollowUp(&it, time.Now()) || strings.Contains(it.FollowUp.Monitoring, "Checks unavailable") {
		t.Fatal("complete monitoring did not clear its old problem")
	}
}

func TestLegacyCLAPauseAllowsIndependentNewReviewButPreservesTheAgreement(t *testing.T) {
	it := trackedJob()
	it.State = StateNeedsYou
	it.QKind = "scope"
	it.Question = "Sign CLA"
	it.FollowUp.State = "needs_you"
	it.FollowUp.Reason = "license/cla needs the contributor agreement"
	it.PullRequest.CheckRuns = []CheckRun{{Name: "license/cla", State: "in_progress", URL: "https://cla-assistant.io/o/r"}}
	QueueCIFollowUp(&it, time.Now())
	if it.State != StateQueued || it.PullRequest.CheckRuns[0].State != "in_progress" {
		t.Fatal("external gate prevented maintenance or was bypassed")
	}
}

func TestReviewSuccessResetsOnlyItsRepairEpisodeAndKeepsLaterFeedback(t *testing.T) {
	it := trackedJob()
	QueueCIFollowUp(&it, time.Now())
	it.FollowUp.Attempt = 3
	it.PullRequest.FeedbackItems = append(it.PullRequest.FeedbackItems, PRFeedback{ID: "inline:2", Revision: "r2"})
	it.State = StateShipped
	WatchPublishedPR(&it)
	if it.FollowUp.Attempt != 0 || it.FollowUp.Reviewed["inline:2"] != "" {
		t.Fatal("success consumed later feedback or retained exhausted episode")
	}
	QueueCIFollowUp(&it, time.Now())
	if it.State != StateQueued || it.FollowUp.Attempt != 1 {
		t.Fatal("later feedback did not get a bounded independent episode")
	}
}

func TestReopenedReviewThreadIsActionableWithoutCreatingWorkOnResolution(t *testing.T) {
	it := trackedJob()
	QueueCIFollowUp(&it, time.Now())
	it.State = StateShipped
	WatchPublishedPR(&it)
	it.PullRequest.FeedbackItems[0].Resolved = true
	QueueCIFollowUp(&it, time.Now())
	if it.State != StateShipped {
		t.Fatal("resolution created a paid repair")
	}
	it.PullRequest.FeedbackItems[0].Resolved = false
	QueueCIFollowUp(&it, time.Now())
	if it.State != StateQueued || it.FollowUp.Attempt != 1 {
		t.Fatal("reopened unresolved thread was missed")
	}
}
