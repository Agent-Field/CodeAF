package factory

import (
	"fmt"
	"strings"
	"time"
	"unicode"
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
	// ONLY A VERDICT THAT STILL STANDS IS REPAIRED ([CurrentFindings]): a
	// finding an earlier round reported and a later round of the same step
	// no longer carries is the old verdict, and neither pauses Auto nor is
	// repaired ([FindingStands]).
	r := AutoRepair{Findings: CurrentFindings(it)}
	if len(r.Findings) == 0 {
		return AutoRepair{}, false
	}
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
// is reported again as a claim in its own words. A sheet row is found by its
// words ([ClaimKey]), and a policy row only by a policy claim, so the request
// quotes each finding exactly as its row holds it, with no label inside the
// quote, and names the medium a policy finding is reported with: the claim
// that shows it fixed then lands on the row that failed. (Labelled
// "repository policy: FINDING: …", as [OverrideSummary] says it, the restated
// claim was a new row and the failing one stayed.)
func (r AutoRepair) Words() string {
	parts := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		if f.Kind == OverridePolicy {
			parts = append(parts, "repository policy, reported with medium policy: “"+f.Text+"”")
		} else {
			parts = append(parts, "proof: “"+f.Text+"”")
		}
	}
	return "Auto repair " + r.count() + ": the checks before publication found problems. Fix each one and verify the fix, then report each finding again as a claim whose text is exactly the quoted words, shown with evidence: " + strings.Join(parts, "; ") + "."
}

// Activity is the line the job's history keeps for the repair.
func (r AutoRepair) Activity() string {
	return "Auto repair " + r.count() + " · " + OverrideSummary(r.Findings)
}

func (r AutoRepair) count() string { return fmt.Sprintf("%d of %d", r.Attempt, r.Limit) }

// CurrentFindings are the landed job's failing findings ([FailingFindings])
// whose verdict still stands: the latest round of some step still reports the
// claim. A sheet keeps a row until a claim in the same words replaces it, so a
// finding an earlier round reported outlives the later round of the same step
// that repaired it in other words. Job 31 (2026-11-17): Follow PR's readiness
// review reported the changelog finding as a policy claim; its next round
// fixed it (f72f8f6883) and proved "the changelog entry carries the pull
// request number …" as a test claim; the failing policy row stayed, and a
// week later Auto sent Follow PR to fix it again. Only a step's latest round
// is kept ([Phase.Result]), so a row no latest round carries is that old
// verdict. Nothing on the sheet is changed here. A row this misses because
// the record cannot tell ([FindingReplaced] false) is not repaired either,
// and still pauses Auto.
func CurrentFindings(it Item) []CheckOverride {
	var out []CheckOverride
	for _, f := range FailingFindings(it) {
		if latestRoundCarries(it, f) {
			out = append(out, f)
		}
	}
	return out
}

// latestRoundCarries says the latest round of some step reports the finding's
// claim, on the same list (a policy claim for a policy finding).
func latestRoundCarries(it Item, f CheckOverride) bool {
	if it.Stream == nil {
		return false
	}
	key := ClaimKey(f.Text)
	for _, ph := range it.Stream.Phases {
		if ph.Result == nil {
			continue
		}
		for _, cl := range ph.Result.Claims {
			if (cl.Medium == "policy") == (f.Kind == OverridePolicy) && ClaimKey(cl.Text) == key {
				return true
			}
		}
	}
	return false
}

// ── ONLY A VERDICT THAT STILL STANDS BLOCKS ────────────────────────────────
//
// The owner's decision of 2026-11-17 ("ignore replaced findings"): Auto's
// pause ([AutonomyBlockReason]), the proof approval and the merge gate count
// only findings the latest round still reports. Job 31 stayed paused for a
// week on the changelog policy row its next Follow PR round had fixed and
// proved in other words, so Auto never pushed the fix.

// FindingStands says a sheet row is a failing finding that still blocks: it
// fails, no override of the person's covers it, and no later round replaced
// it ([FindingReplaced]).
func FindingStands(it Item, kind string, cl Claim) bool {
	return !cl.OK && !ClaimOverridden(it, kind, cl) && !FindingReplaced(it, kind, cl)
}

// FindingReplaced says a failing sheet row is an old verdict: no step's latest
// round reports its claim on its list any more. It says so only when the
// record can tell ([roundsKnown]); otherwise the row is taken as standing,
// as before this rule. A passing row is never replaced.
func FindingReplaced(it Item, kind string, cl Claim) bool {
	if cl.OK || !roundsKnown(it) {
		return false
	}
	return !latestRoundCarries(it, CheckOverride{Kind: kind, Text: cl.Text})
}

// roundsKnown says the record keeps every step's latest round, so a row none
// of them reports is one a later round replaced: every step that ran (done,
// failed or waiting; an approve step reports no claims) kept its result, and
// some result carries claims. A record from before results were kept, a
// step whose result is missing, and a reader's copy whose results carry no
// claims all read false.
func roundsKnown(it Item) bool {
	if it.Stream == nil {
		return false
	}
	claims := false
	for _, ph := range it.Stream.Phases {
		switch ph.State {
		case PhaseDone, PhaseFailed, PhaseWaiting:
		default:
			continue
		}
		if ph.Kind == StageGate && ph.Result == nil {
			continue
		}
		if ph.Result == nil {
			return false
		}
		if len(ph.Result.Claims) > 0 {
			claims = true
		}
	}
	return claims
}

// ClaimKey is a claim's words for matching a sheet row: lower case, letters
// and digits only, single spaces.
func ClaimKey(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

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
