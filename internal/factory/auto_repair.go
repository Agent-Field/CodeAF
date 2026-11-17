package factory

import (
	"fmt"
	"strings"
	"time"
)

// ── AUTO FIXES WHAT THE CHECKS FOUND BEFORE IT ASKS THE PERSON ─────────────
//
// A landed job in Auto whose proof claims or repository policy checks fail
// used to pause for the person ("Repository policy needs attention: …"),
// while the job's own repair loop sat unused. The owner's decision of
// 2026-11-16: a failing finding on the job's change or pull request is not a
// pause in Auto. Auto sends it back through its own repair loop — the finding
// is the repair request, the job repairs and verifies, and the checked result
// lands again — within the job's budget and repair attempt limits, and the
// person sees the merge offer only once the findings are clean.
//
// TWO ROADS, ONE RULE. Before the job has a pull request, the repair is a
// send-back: one more prove step after the last finished one, its ask the
// findings (the same door Request changes takes). Once the pull request is
// open, the repair is a Follow PR round with the findings as its request, the
// same round review findings and failing CI start, and it counts against
// Follow PR's own repair limit.
//
// WHEN THE ATTEMPTS RUN OUT the job stays landed exactly as before this
// change: [AutonomyBlockReason] says the finding, and the person's doors
// (Request changes, Take over, resume) are unchanged. Manual never repairs on
// its own; there the person decides.

// AutoRepairLimit is how many times Auto sends a run's failing findings back
// before its first publication, the same bound as Follow PR's repairs.
const AutoRepairLimit = DefaultPRRepairAttempts

// AutoRepair is one repair Auto would start for a landed job's failing
// findings: what they are, on which road, and which attempt it is.
type AutoRepair struct {
	Findings []CheckOverride
	// OnPR is a Follow PR round on the job's open pull request; false is a
	// send-back before the first publication.
	OnPR    bool
	Attempt int
	Limit   int
}

// AutoRepairDue says Auto should send the landed job's failing findings back
// through its own repair loop now, rather than pause for the person, and how.
// It is false in Manual, when the pause is about anything other than a failing
// finding (a budget, a question, a rule hold, a failed publication), while an
// approval is already under way, and when the repair attempts are used up.
func AutoRepairDue(it Item) (AutoRepair, bool) {
	// OffersAutoResume is the pause on a failing finding and nothing else:
	// Auto, landed, a budget left, no question, no rule hold, no failed
	// publication, and a finding no override covers.
	if !OffersAutoResume(it) || it.Publishing || it.Stream == nil || len(it.Stream.Phases) == 0 {
		return AutoRepair{}, false
	}
	if a := it.Approval; a != nil && a.State == "approving" {
		return AutoRepair{}, false
	}
	if it.Cap <= 0 || JobLifetimeSpent(it) >= it.Cap {
		return AutoRepair{}, false
	}
	r := AutoRepair{Findings: FailingFindings(it)}
	if pr := it.PullRequest; pr != nil {
		f := it.FollowUp
		if pr.State != PROpen || f == nil || it.Manual || (it.Merging != nil && it.Merging.State != MergeFailed) {
			return AutoRepair{}, false
		}
		r.OnPR, r.Limit = true, f.Limit
		if r.Limit < 1 {
			r.Limit = DefaultPRRepairAttempts
		}
		if f.Attempt >= r.Limit {
			return AutoRepair{}, false
		}
		r.Attempt = f.Attempt + 1
		return r, true
	}
	r.Limit = AutoRepairLimit
	if it.Stream.AutoRepairs >= r.Limit {
		return AutoRepair{}, false
	}
	r.Attempt = it.Stream.AutoRepairs + 1
	return r, true
}

// Words is the repair's request: what the checks found, and that each finding
// is shown fixed in its own words, so the sheet that lands next says whether
// it is.
func (r AutoRepair) Words() string {
	return "Auto repair " + r.count() + ": the checks before publication found problems. Fix each one, verify the fix, and report each finding again in exactly these words, shown with evidence: " + OverrideSummary(r.Findings) + "."
}

// Activity is the line the job's history keeps for the repair.
func (r AutoRepair) Activity() string {
	return "Auto repair " + r.count() + " · " + OverrideSummary(r.Findings)
}

func (r AutoRepair) count() string { return fmt.Sprintf("%d of %d", r.Attempt, r.Limit) }

// BeginAutoRepair records the repair Auto starts for the landed job's failing
// findings, and answers it. On the pull request it queues the Follow PR round
// with the findings as its request; before one it counts the attempt, and the
// caller sends the job back with [AutoRepair.Words] through the runner's
// SendBack door. It refuses when no repair is due, so a job a person or a
// worker moved meanwhile is left alone.
func BeginAutoRepair(it *Item, now time.Time) (AutoRepair, error) {
	r, ok := AutoRepairDue(*it)
	if !ok {
		return AutoRepair{}, fmt.Errorf("no Auto repair is due")
	}
	it.Note(now, r.Activity())
	if !r.OnPR {
		it.Stream.AutoRepairs = r.Attempt
		return r, nil
	}
	f := *it.FollowUp
	f.Kind, f.Readiness = "pr", nil
	f.Request = r.Words()
	f.State, f.BlockedBy, f.EventKey = "watching", "", ""
	it.FollowUp = &f
	it.State = StateShipped
	it.Approval = nil
	if !QueueCIFollowUp(it, now) || it.FollowUp == nil || it.FollowUp.State != "queued" {
		return AutoRepair{}, fmt.Errorf("Follow PR could not queue the repair: %s", strings.TrimSpace(it.FollowUp.Reason))
	}
	return r, nil
}
