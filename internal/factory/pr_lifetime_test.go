package factory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFollowPRUsesOneStepAcrossCIReviewConflictAndRestart(t *testing.T) {
	it := followItem()
	now := time.Now()
	if !QueueCIFollowUp(&it, now) {
		t.Fatal("CI not queued")
	}
	for _, event := range []string{"review", "conflict", "external push"} {
		it.State = StateShipped
		WatchPublishedPR(&it)
		it.PullRequest.Checks = PRChecks{Passed: 2}
		switch event {
		case "review":
			it.PullRequest.ReviewDecision = "changes_requested"
			it.PullRequest.Reviews = []PRReview{{By: "reviewer", State: "changes_requested", Body: "Handle an empty response", At: now}}
		case "conflict":
			it.PullRequest.Conflict = true
			it.PullRequest.BaseSHA = strings.Repeat("b", 40)
		case "external push":
			it.PullRequest.HeadSHA = strings.Repeat("c", 40)
			it.PullRequest.Checks = PRChecks{Failed: 1}
			it.FollowUp.Attempt = 0
		}
		if !QueueCIFollowUp(&it, now) {
			t.Fatalf("%s not queued", event)
		}
		if len(it.Stream.Phases) != 2 || it.Stream.Phases[1].Name != FollowPRStep || len(it.Stages) != 1 {
			t.Fatal("follow-up invented extra stages")
		}
		data, _ := json.Marshal(it)
		var restored Item
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if QueueCIFollowUp(&restored, now) {
			t.Fatal("restart duplicated an active repair")
		}
	}
}
func TestFollowPRDetectsExternalMergeAndClosureWhilePaused(t *testing.T) {
	for _, state := range []string{PRMerged, PRClosed} {
		it := followItem()
		it.FollowUp.State = "needs_you"
		it.State = StateNeedsYou
		it.Question = "old question"
		it.Manual = true
		it.PullRequest.State = state
		if !QueueCIFollowUp(&it, time.Now()) || JobState(it) != "done" || it.Question != "" || it.FollowUp.State != state || it.Stream.Phases[len(it.Stream.Phases)-1].State != PhaseDone {
			t.Fatalf("external %s did not finish job: %+v", state, it.FollowUp)
		}
	}
}
func TestFollowPRDoesNotReplayOldReviewAfterRepair(t *testing.T) {
	it := followItem()
	it.PullRequest.Checks = PRChecks{Passed: 2}
	it.PullRequest.ReviewDecision = "changes_requested"
	it.PullRequest.Reviews = []PRReview{{By: "r", State: "changes_requested", Body: "fix empty result", At: time.Now()}}
	if !QueueCIFollowUp(&it, time.Now()) {
		t.Fatal("review not queued")
	}
	it.State = StateShipped
	WatchPublishedPR(&it)
	it.PullRequest.HeadSHA = strings.Repeat("d", 40)
	QueueCIFollowUp(&it, time.Now())
	if it.State != StateShipped || it.FollowUp.State != "healthy" {
		t.Fatal("old request started another repair")
	}
	it.PullRequest.Reviews[0].Body = "also fix cancellation"
	if !QueueCIFollowUp(&it, time.Now()) || it.State != StateQueued {
		t.Fatal("new feedback ignored")
	}
}
