package factory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A fresh observed snapshot must be able to release only a budget-owned pause;
// raising the cap must not erase retry accounting or a different human decision.
func TestFollowPRBudgetRecoveryKeepsWorkAndAttempts(t *testing.T) {
	it := followItem()
	it.Autonomy = AutonomyAuto
	it.Stream.Spent = it.Cap
	it.FollowUp.Attempt = 1
	if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.State != "needs_you" {
		t.Fatal("budget exhaustion did not park maintenance")
	}
	raw, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	var restored Item
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	restored.Cap = 10
	restored.PullRequest.HeadSHA = strings.Repeat("b", 40)
	if !QueueCIFollowUp(&restored, time.Now()) || restored.State != StateQueued || restored.FollowUp.State != "queued" {
		t.Fatalf("budget-owned maintenance did not recover on the latest PR observation: state=%s follow=%s", restored.State, restored.FollowUp.State)
	}
	if restored.FollowUp.Attempt != 2 || restored.Stream.Spent != 3 || restored.HeadCommit != it.HeadCommit {
		t.Fatal("recovery reset attempt/spend history or claimed an unverified head")
	}
}

func TestFollowPRBudgetRecoveryPreservesOtherPauses(t *testing.T) {
	for _, mode := range []string{"takeover", "manual", "critical decision", "attempt limit"} {
		t.Run(mode, func(t *testing.T) {
			it := followItem()
			it.Autonomy = AutonomyAuto
			it.Stream.Spent = it.Cap
			it.FollowUp.Attempt = 1
			QueueCIFollowUp(&it, time.Now())
			it.Cap = 10
			switch mode {
			case "takeover":
				it.Manual = true
			case "manual":
				it.Autonomy = AutonomyManual
			case "critical decision":
				it.State, it.QKind, it.Question = StateNeedsYou, "scope", "Choose the public API"
			case "attempt limit":
				it.FollowUp.Attempt = it.FollowUp.Limit
			}
			QueueCIFollowUp(&it, time.Now())
			if it.State == StateQueued || it.FollowUp.State == "queued" {
				t.Fatal("a higher cap erased a different pause or exhausted attempt limit")
			}
		})
	}
}

func TestFollowPRExternalReadyHeadRequiresFreshVerification(t *testing.T) {
	it := followItem()
	it.Autonomy = AutonomyAuto
	it.PullRequest = &PullRequest{Number: 7, State: PROpen, Draft: false, HeadSHA: strings.Repeat("b", 40),
		ChecksRead: true, ReviewsRead: true, Mergeable: MergeableYes, CI: "ci ✓", Checks: PRChecks{Passed: 1},
		ReadinessKey: "current-snapshot", ReadinessContext: "Complete current-head review material"}
	if !QueueCIFollowUp(&it, time.Now()) || it.State != StateQueued || it.FollowUp.State != "queued" {
		t.Fatal("green CI on a new external head was treated as already verified")
	}
	if it.HeadCommit == it.PullRequest.HeadSHA {
		t.Fatal("observation alone falsely promoted the external commit to verified")
	}
}

func TestFollowPRBudgetRecoveryTerminalPendingAndRestartMatrix(t *testing.T) {
	for _, event := range []string{"pending", "merged", "closed", "new feedback", "legacy budget", "exhausted attempts"} {
		t.Run(event, func(t *testing.T) {
			it := followItem()
			it.Autonomy = AutonomyAuto
			it.Stream.Spent = it.Cap
			it.FollowUp.Attempt = 1
			QueueCIFollowUp(&it, time.Now())
			it.Cap = 10
			switch event {
			case "pending":
				it.PullRequest.Checks = PRChecks{Pending: 1}
			case "merged":
				it.PullRequest.State = PRMerged
				it.Manual = true
			case "closed":
				it.PullRequest.State = PRClosed
				it.Manual = true
			case "new feedback":
				it.PullRequest.Feedback = "Handle cancellation without losing results"
				it.PullRequest.ReviewsRead = true
			case "legacy budget":
				it.FollowUp.BlockedBy = ""
				it.FollowUp.Reason = "Follow PR reached its repair or spending limit. Take over, or raise the limit and resume."
			case "exhausted attempts":
				it.FollowUp.Attempt = it.FollowUp.Limit
			}
			changed := QueueCIFollowUp(&it, time.Now())
			switch event {
			case "pending":
				if !changed || it.State != StateShipped || it.FollowUp.State != "watching" {
					t.Fatal("budget remained stranded while CI is pending")
				}
			case "merged", "closed":
				if !changed || it.FollowUp.State != it.PullRequest.State || it.State != StateShipped {
					t.Fatal("budget/takeover hid terminal PR state")
				}
			case "exhausted attempts":
				if it.FollowUp.State != "needs_you" || it.FollowUp.Attempt != it.FollowUp.Limit {
					t.Fatal("higher cap reset repair attempts")
				}
			default:
				if !changed || it.State != StateQueued || it.FollowUp.Attempt != 2 {
					t.Fatal("fresh work did not resume in existing bounds")
				}
			}
			raw, _ := json.Marshal(it)
			var restored Item
			_ = json.Unmarshal(raw, &restored)
			if QueueCIFollowUp(&restored, time.Now()) {
				t.Fatal("restart duplicated the same scheduled action")
			}
		})
	}
}

func TestBudgetRecoveryCannotEraseCriticalDecisionsOrHumanPause(t *testing.T) {
	for _, block := range []string{"critical", "human pause", "takeover", "manual", "publishing", "attempts"} {
		t.Run(block, func(t *testing.T) {
			now := time.Now()
			it := followItem()
			it.Autonomy = AutonomyAuto
			it.State = StateNeedsYou
			it.Stream.Spent = it.Cap
			StampFailure(&it, NewFailure(FollowPRStep, CodeTransportError, "connection reset", "", FailureStopped, 1, now))
			PlanStepRecovery(&it, now)
			it.Cap = 10
			switch block {
			case "critical":
				it.Question, it.QKind = "Choose the public API", "scope"
			case "human pause":
				it.Stream.Paused = true
			case "takeover":
				it.Manual = true
			case "manual":
				it.Autonomy = AutonomyManual
			case "publishing":
				it.Publishing = true
			case "attempts":
				it.Recovery.Attempts = it.Recovery.Limit
			}
			PlanStepRecovery(&it, now.Add(time.Minute))
			if it.Recovery.State == "waiting" || it.Recovery.State == "queued" || it.Stream.Spent != 3 {
				t.Fatal("budget edit overrode a separate block or reset spend")
			}
		})
	}
}

// The Follow PR step's note says what the follow-up says: a budget pause
// never leaves it reading "watching".
func TestFollowPRBudgetPauseIsTheStepsNoteToo(t *testing.T) {
	it := followItem()
	it.Autonomy = AutonomyAuto
	it.Stream.Spent = it.Cap
	it.FollowUp.Attempt = 1
	if !QueueCIFollowUp(&it, time.Now()) || it.FollowUp.State != "needs_you" {
		t.Fatal("budget exhaustion did not park maintenance")
	}
	if note := it.Stream.Phases[EnsureFollowPR(&it)].Note; !strings.HasPrefix(note, "Job budget reached") {
		t.Fatalf("the Follow PR step still says %q", note)
	}
}
