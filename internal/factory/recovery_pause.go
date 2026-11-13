package factory

import (
	"strconv"
	"strings"
)

// CanPauseRecovery is the recovery notice a person can put away while
// keeping the saved step and its unresolved decision.
func CanPauseRecovery(it Item) bool {
	if it.State != StateNeedsYou || it.QKind != QKindOutOfAttempts || !IsIssueJob(it) ||
		it.Talk == "" || it.Manual || it.Publishing || it.PublishStatus != nil || it.PublishError != "" ||
		it.PullRequest != nil || it.FollowUp != nil || it.Merging != nil || it.PublishAnyway != nil ||
		(it.Approval != nil && it.Approval.State != "approved") || JobState(it) == "done" {
		return false
	}
	s := it.Stream
	if s == nil || s.Paused || s.Cur < 0 || s.Cur >= len(s.Phases) || PendingQuestionID(it) == "" {
		return false
	}
	step := s.Phases[s.Cur]
	kind := step.Kind
	if kind == "" {
		if index := StageIndex(it.Stages, step.Name); index >= 0 {
			kind = it.Stages[index].Kind
		}
	}
	return step.State == PhaseWaiting && step.Round > 0 && step.Name != FollowPRStep && (kind == StageChat || kind == StageCheck)
}

// CanPauseDecision admits a saved wait or an open PR decision, but never
// interrupts a push or merge that the person already authorized.
func CanPauseDecision(it Item) bool {
	if it.Talk == "" || it.Manual || it.Publishing || StoppedByPerson(it) || JobState(it) == "done" ||
		(it.Stream != nil && it.Stream.Paused) || (it.Approval != nil && it.Approval.State == "approving") ||
		(it.Merging != nil && it.Merging.State != MergeFailed) {
		return false
	}
	if pr := it.PullRequest; it.Kind != KindPR && it.Publish == "pr" && pr != nil && pr.Number > 0 && pr.State == PROpen {
		return true
	}
	if CanPauseRecovery(it) {
		return true
	}
	switch NeedsKind(it) {
	case NeedsApproveStart, NeedsApprovePlan, NeedsApproveProof, NeedsSpendingLimit, NeedsPublishFailed, NeedsOutOfAttempts:
		return true
	case NeedsQuestion:
		return it.QKind != "results_not_saved" && PendingQuestionID(it) != ""
	case NeedsRunnerOffline:
		return it.Recovery == nil || (it.Recovery.State != "waiting" && it.Recovery.State != "queued")
	default:
		return false
	}
}

const pausedDecisionPrefix = "decision:"

// PauseDecision keeps the original stuck-step identity so an explicit chat
// retry still retries that step. Other pauses keep their own saved revision,
// even when an observation changes a PR's facts while the job is paused.
func PauseDecision(it *Item, revision int) {
	if CanPauseRecovery(*it) {
		it.PausedRecoveryQuestion = PendingQuestionID(*it)
		return
	}
	it.PausedRecoveryQuestion = pausedDecisionPrefix + strconv.Itoa(revision)
}

func PausedDecision(it Item) bool {
	return strings.HasPrefix(it.PausedRecoveryQuestion, pausedDecisionPrefix) && !StoppedByPerson(it) && JobState(it) != "done"
}

// RecoveryPaused covers a decision a person put away until manager chat
// resumes it, including stuck-step identities saved by earlier builds.
func RecoveryPaused(it Item) bool {
	return PausedDecision(it) || (it.PausedRecoveryQuestion != "" && it.PausedRecoveryQuestion == PendingQuestionID(it))
}

const RecoveryPauseWords = "Tell the manager “Try again” to continue."
const DecisionPauseWords = "Tell the manager “Resume” to continue."

func DecisionPauseDetail(it Item) string {
	if PausedDecision(it) {
		return DecisionPauseWords
	}
	return RecoveryPauseWords
}
