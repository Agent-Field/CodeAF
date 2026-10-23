package factory

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// ReviewVersions contains actionable comment revisions, never mutable diff
// coordinates. Removing or resolving an old thread cannot manufacture new work.
func ReviewVersions(pr *PullRequest) map[string]string {
	out := map[string]string{}
	if pr == nil || !pr.FeedbackTracked {
		return out
	}
	for _, r := range pr.FeedbackItems {
		if !r.Resolved && !r.External {
			out[r.ID] = r.Revision
		}
	}
	for _, r := range pr.Reviews {
		if strings.TrimSpace(r.Body) == "" || r.URL == "" || r.State == "pending" {
			continue
		}
		out["review:"+r.URL] = fmt.Sprintf("%x", sha256.Sum256([]byte(r.State+"\n"+r.Body)))
	}
	return out
}
func unseenReviews(pr *PullRequest, handled map[string]string) int {
	n := 0
	for id, version := range ReviewVersions(pr) {
		if handled[id] != version {
			n++
		}
	}
	return n
}

// Acknowledgement belongs to a completed repair, not admission to the queue.
// Feedback arriving during execution remains unseen for the next observation.
func AcknowledgeReview(it *Item) {
	if it.FollowUp == nil {
		return
	}
	f := it.FollowUp
	if f.Inspecting != nil {
		if f.Reviewed == nil {
			f.Reviewed = map[string]string{}
		}
		current := ReviewVersions(it.PullRequest)
		for id, version := range f.Inspecting {
			if current[id] == version {
				f.Reviewed[id] = version
			}
		}
		f.Inspecting = nil
		f.Attempt = 0
	}
}
func ReviewPauseRecoverable(it Item) bool {
	if it.FollowUp == nil || it.Manual || !Automatic(it) || it.Publishing || it.DoneHow != "" || it.Stream == nil || it.Stream.Paused || it.Cap <= JobSpent(it) {
		return false
	}
	if it.Failure != nil && it.Failure.Code == CodeStopped {
		return false
	}
	// Legacy CLA escalations paused the entire stage. Only the named external
	// prerequisite is releasable; signing remains the person's GitHub action.
	if it.Asking == nil && it.QKind == "scope" && externalAgreementPause(it) {
		return true
	}
	if it.QKind == "out_of_attempts" && it.Asking != nil && it.Asking.Stage == FollowPRStep {
		return true
	}
	return it.Asking == nil && it.Question == "" && (it.FollowUp.BlockedBy == "verification" || it.FollowUp.BlockedBy == FollowBlockedAttempts)
}

// ReleaseVerificationPause grants one bounded recheck after a concrete guard
// correction. It never accepts a repair or repeats publication by itself.
func ReleaseVerificationPause(it *Item, now time.Time) {
	if it.FollowUp == nil {
		return
	}
	f := it.FollowUp
	f.RecoveryVersion = 1
	f.State, f.BlockedBy = "watching", ""
	f.Request = "Recheck the saved repair against current review feedback. Reuse matching current-checkout test receipts; do not repeat passing checks unless code or test scope changes. Report no_change if the published head already addresses the findings."
	it.State = StateShipped
	it.Question, it.QKind = "", ""
	it.Asking, it.Failure = nil, nil
	it.Stream.Log = append(it.Stream.Log, LogLine{At: now, Tone: "said", Text: "Recovered a corrected test command. Rechecking the saved repair; completed work and spending retained."})
}

func externalAgreementPause(it Item) bool {
	if it.PullRequest == nil || it.FollowUp == nil {
		return false
	}
	low := strings.ToLower(it.FollowUp.Reason)
	if !(strings.Contains(low, "license/cla") || strings.Contains(low, "contributor agreement")) {
		return false
	}
	for _, c := range it.PullRequest.CheckRuns {
		if ExternalPRCheck(c) && c.State != "success" {
			return true
		}
	}
	return false
}
