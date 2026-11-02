package run

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const noChangeReviewRefusal = "current-head feedback review is incomplete or the checkout changed"
const followRefusalReceipt = "Follow PR refusal: "

// The receipt binds a refused no-change report to the observations it read.
// It travels with the existing result notes across worker reports and restarts.
func followRefusalKey(it factory.Item, reason string) string {
	p := it.PullRequest
	if p == nil {
		return ""
	}
	raw, _ := json.Marshal(struct {
		Head, Feedback, Decision, Reason string
		Versions                         map[string]string
		Checks                           factory.PRChecks
		Conflict                         bool
	}{p.HeadSHA, p.Feedback, p.ReviewDecision, reason, factory.ReviewVersions(p), p.Checks, p.Conflict})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func unchangedFollowRefusal(it factory.Item, ph factory.Phase) error {
	if ph.Name != factory.FollowPRStep || ph.Result == nil {
		return nil
	}
	r := *ph.Result
	if verificationReason(r) != noChangeReviewRefusal {
		return nil
	}
	key := followRefusalReceipt + followRefusalKey(it, noChangeReviewRefusal)
	for _, note := range r.Notes {
		if note == key {
			return StepControlRefusal("This PR has the same head and comments, and its current-head review is still incomplete or the checkout changed. Wait for a new push or comment before trying again, or stop the job. No paid round was started.")
		}
	}
	return nil
}

func followAnswerRefusal(it factory.Item, yes bool, words string) error {
	if it.QKind != factory.QKindOutOfAttempts || factory.OutOfAttemptsChoice(yes, strings.TrimSpace(words)) != factory.ChoiceTryOnceMore || it.Stream == nil {
		return nil
	}
	i := it.Stream.Cur
	if i < 0 || i >= len(it.Stream.Phases) {
		return nil
	}
	return unchangedFollowRefusal(it, it.Stream.Phases[i])
}
