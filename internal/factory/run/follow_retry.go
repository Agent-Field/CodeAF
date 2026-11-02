package run

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

const noChangeReviewRefusal = "current-head feedback review is incomplete or the checkout changed"
const followRefusalReceipt = "Follow PR refusal: "
const followCheckoutReceipt = "Follow PR refused checkout: "

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
	// A checkout refusal is revalidated by its worker before any model call.
	if refusedCheckout(r) != nil {
		return nil
	}
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

// A complete assessment can be refused because the checkout changed. Only the
// worker can reread that checkout, so server answers admit an unpaid recheck.
func refusedCheckout(r factory.StageResult) *verification.Snapshot {
	for _, note := range r.Notes {
		if raw, ok := strings.CutPrefix(note, followCheckoutReceipt); ok {
			var at verification.Snapshot
			if json.Unmarshal([]byte(raw), &at) == nil && at.Head != "" && at.Fingerprint != "" {
				return &at
			}
		}
	}
	return nil
}

func followRefusalNotes(job Job, res factory.StageResult) []string {
	notes := []string{followRefusalReceipt + followRefusalKey(job.Item, noChangeReviewRefusal)}
	if factory.ReadinessReportFault(job.Item.PullRequest.HeadSHA, res) != "" {
		return notes
	}
	at, err := verification.SnapshotAt(job.Dir)
	if err != nil || at.Head == "" || at.Fingerprint == "" {
		return nil
	}
	raw, _ := json.Marshal(at)
	return append(notes, followCheckoutReceipt+string(raw))
}

func cachedCheckoutRefusal(job Job) *factory.StageResult {
	if job.Stage.Name != factory.FollowPRStep || job.Item.Stream == nil {
		return nil
	}
	for _, ph := range job.Item.Stream.Phases {
		if ph.Name != job.Stage.Name || ph.Result == nil {
			continue
		}
		r := *ph.Result
		before := refusedCheckout(r)
		if before == nil || verificationReason(r) != noChangeReviewRefusal {
			return nil
		}
		receipt := followRefusalReceipt + followRefusalKey(job.Item, noChangeReviewRefusal)
		same := false
		for _, note := range r.Notes {
			same = same || note == receipt
		}
		if !same {
			return nil
		}
		now, err := verification.SnapshotAt(job.Dir)
		if err != nil || now != *before {
			return nil
		}
		r.Spent, r.Chat, r.Model = 0, "", ""
		r.Output = "The checkout still has the same unfinished changes or does not match the published head. Restore the clean published checkout, wait for a new push or comment, or stop the job. No paid round was started."
		return &r
	}
	return nil
}
