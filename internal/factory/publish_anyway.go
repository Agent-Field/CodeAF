package factory

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
 "unicode"
)

// SkippedChecksIdentity binds consent to the reported head, change and skipped
// checks. Empty means at least one unmet check cannot be waived by a person.
const EveryCheckPolicy = "tests pass before anything posts"

func SkippedChecksIdentity(it Item) string {
 if (it.Change == nil || len(it.Change.Files) == 0) && len(it.Files) == 0 { return "" }
	if it.Stream == nil || (it.HeadCommit == "" && (it.Change == nil || it.Change.Patch == "" || it.Change.PatchTruncated)) {
		return ""
	}
	var skips []string
	for _, st := range it.Stages {
		if st.Kind != StageCheck {
			continue
		}
		var ph *Phase
		for i := range it.Stream.Phases {
			if it.Stream.Phases[i].Name == st.Name {
				ph = &it.Stream.Phases[i]
				break
			}
		}
		if ph == nil {
			return ""
		}
		if ph.State == PhaseSkipped {
			if strings.TrimSpace(ph.SkipReason) == "" || !FileCondition(st) || Fits(st, it) {
				return ""
			}
			skips = append(skips, st.Name, st.When, ph.SkipReason)
		} else if ph.State != PhaseDone || ph.Result == nil || !ph.Result.Done || ph.Result.Exit != 0 {
			return ""
		}
	}
	if len(skips) == 0 {
		return ""
	}
	for _, ph := range it.Stream.Phases {
		if ph.State == PhaseSkipped || ph.Result == nil {
			continue
		}
		for _, cl := range ph.Result.Claims {
			if cl.Medium == "test" && !cl.OK {
				return ""
			}
		}
	}
	for _, cl := range it.Proof {
		if cl.Medium == "test" && !cl.OK {
			return ""
		}
	}
	raw, _ := json.Marshal(struct {
		Head   string
		Change *Change
		Files  []FileChange
		Skips  []string
	}{it.HeadCommit, it.Change, it.Files, skips})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// OffersPublishAnyway is the engine's current offer at a refused post step.
func OffersPublishAnyway(it Item) bool {
	if it.State != StateNeedsYou || it.QKind != QKindOutOfAttempts || SkippedChecksIdentity(it) == "" || it.Stream.Cur < 0 || it.Stream.Cur >= len(it.Stream.Phases) {
		return false
	}
	ph := it.Stream.Phases[it.Stream.Cur]
	if ph.Kind != StagePost || ph.State != PhaseWaiting || ph.Result == nil {
		return false
	}
	for _, cl := range ph.Result.Claims {
		if cl.Medium == "policy" && !cl.OK && PolicyWords(cl.Evidence) == EveryCheckPolicy {
			return true
		}
	}
	return false
}

// PublishedAnyway never treats automatic approval as a person's consent.
func PublishedAnyway(it Item) bool {
	a := it.PublishAnyway
	return a != nil && !a.Automatic && a.Kind == "publish_anyway" && a.State == "approved" && a.DecidedBy != "" && !a.At.IsZero() && a.Matched != "" && a.Matched == SkippedChecksIdentity(it)
}

// DecidePublishAnyway keeps the same durable approval shape as other decisions.
func DecidePublishAnyway(it *Item, who string, now time.Time) error {
	if !OffersPublishAnyway(*it) {
		return fmt.Errorf("Publish anyway requires only correctly skipped conditional checks; a check failed, is pending, was not measured, or the change moved")
	}
	who = NormalizeHandle(who)
	if who == "" || !MayApprove(*it, who, false) {
		return fmt.Errorf("only the person allowed to publish can publish anyway")
	}
	it.PublishAnyway = &Approval{Kind: "publish_anyway", State: "approved", DecidedBy: who, At: now.UTC(), Commit: it.HeadCommit, Matched: SkippedChecksIdentity(*it)}
	return nil
}

// PolicyWords normalizes the policy language shared by the runner and job actions.
func PolicyWords(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’':
			// an apostrophe vanishes: stranger's is strangers.
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

