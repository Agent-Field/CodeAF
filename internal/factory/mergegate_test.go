package factory

import (
	"strings"
	"testing"
)

const mergeHead = "1111111111111111111111111111111111111111"

// mergeable is a job whose pull request every rule lets through, so each
// case below changes exactly one fact.
func mergeable() Item {
	return Item{
		Kind: KindIssue, Publish: "pr", HeadCommit: mergeHead,
		Proof:       []Claim{{Text: "delay never exceeds 30s", OK: true, Evidence: "test"}},
		MergePolicy: &MergePolicy{Auto: true},
		PullRequest: &PullRequest{
			Number: 7, URL: "https://github.com/acme/api/pull/7", State: PROpen,
			HeadSHA: mergeHead, Mergeable: MergeableYes,
			ChecksRead: true, Checks: PRChecks{Passed: 3},
			ReviewsRead: true,
		},
	}
}

func TestMergeGateLetsAPersonMergeACleanPullRequest(t *testing.T) {
	v := MergeGate(mergeable(), MergeAsk{By: "@Jm"})
	if !v.OK || v.Commit != mergeHead || v.Rule != "merged by @jm" {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestMergeGateLetsAutoMergeThroughOnlyWhenEveryFactHolds(t *testing.T) {
	v := MergeGate(mergeable(), MergeAsk{Auto: true})
	if !v.OK || v.Rule != "auto-merge is on in this job's pipeline" {
		t.Fatalf("verdict = %+v", v)
	}
}

// THE GATE NEVER MERGES WHEN AUTO-MERGE IS OFF. Every road to off — no
// pinned policy, a policy that says off — refuses the automatic merge, while
// a person can still press Merge on the same job.
func TestMergeGateNeverAutoMergesWhenAutoMergeIsOff(t *testing.T) {
	for name, policy := range map[string]*MergePolicy{
		"no policy pinned": nil,
		"policy says off":  {Auto: false, Method: MergeMethodSquash},
	} {
		it := mergeable()
		it.MergePolicy = policy
		if v := MergeGate(it, MergeAsk{Auto: true}); v.OK || v.Why != "auto-merge is off in this job's pipeline" {
			t.Fatalf("%s: auto verdict = %+v", name, v)
		}
		if v := MergeGate(it, MergeAsk{By: "jm"}); !v.OK {
			t.Fatalf("%s: a person's merge was refused: %+v", name, v)
		}
	}
}

func TestMergeGateRefusesEveryUnsafeOrUnknownFact(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Item)
		ask    MergeAsk
		why    string
	}{
		{"someone else's pull request", func(it *Item) { it.Kind = KindPR }, MergeAsk{By: "jm"}, "codeaf merges only pull requests it opened"},
		{"nothing published", func(it *Item) { it.PullRequest = nil }, MergeAsk{By: "jm"}, "this job has opened no pull request"},
		{"published a comment", func(it *Item) { it.Publish = "comment" }, MergeAsk{By: "jm"}, "this job has opened no pull request"},
		{"dropped", func(it *Item) { it.DoneHow = "dropped" }, MergeAsk{By: "jm"}, "this job was dropped"},
		{"already merged", func(it *Item) { it.PullRequest.State = PRMerged }, MergeAsk{By: "jm"}, "already merged"},
		{"closed", func(it *Item) { it.PullRequest.State = PRClosed }, MergeAsk{By: "jm"}, "the pull request is closed"},
		{"state unread", func(it *Item) { it.PullRequest.State = "" }, MergeAsk{By: "jm"}, "cannot read whether the pull request is open"},
		{"a merge on its way", func(it *Item) { it.Merging = &MergeRun{State: MergeMerging} }, MergeAsk{By: "jm"}, "a merge is already on its way"},
		{"nobody pressed", func(*Item) {}, MergeAsk{}, "a merge needs the person who pressed it"},
		{"no approved commit", func(it *Item) { it.HeadCommit = "" }, MergeAsk{By: "jm"}, "no approved commit is recorded for this job"},
		{"head unread", func(it *Item) { it.PullRequest.HeadSHA = "" }, MergeAsk{By: "jm"}, "cannot read the pull request's head commit"},
		{"head moved", func(it *Item) { it.PullRequest.HeadSHA = strings.Repeat("2", 40) }, MergeAsk{By: "jm"}, "the pull request has commits you did not approve"},
		{"draft", func(it *Item) { it.PullRequest.Draft = true }, MergeAsk{By: "jm"}, "the pull request is a draft"},
		{"conflict", func(it *Item) { it.PullRequest.Mergeable = MergeableNo }, MergeAsk{By: "jm"}, "the pull request has conflicts"},
		{"mergeability unknown", func(it *Item) { it.PullRequest.Mergeable = "" }, MergeAsk{By: "jm"}, "the forge has not said whether it can merge yet"},
		{"checks unread", func(it *Item) { it.PullRequest.ChecksRead = false }, MergeAsk{By: "jm"}, "cannot read the pull request's checks"},
		{"a check failing", func(it *Item) { it.PullRequest.Checks.Failed = 1 }, MergeAsk{By: "jm"}, "1 check is failing"},
		{"checks running", func(it *Item) { it.PullRequest.Checks.Pending = 2 }, MergeAsk{By: "jm"}, "2 checks are still running"},
		{"auto with no checks", func(it *Item) { it.PullRequest.Checks = PRChecks{} }, MergeAsk{Auto: true}, "auto-merge waits for at least one passing check"},
		{"reviews unread", func(it *Item) { it.PullRequest.ReviewsRead = false }, MergeAsk{By: "jm"}, "cannot read the pull request's reviews"},
		{"changes requested", func(it *Item) { it.PullRequest.ReviewDecision = "changes_requested" }, MergeAsk{By: "jm"}, "a reviewer asked for changes"},
		{"reviews required", func(it *Item) { it.PullRequest.Required = 2; it.PullRequest.ReviewDecision = "review_required" }, MergeAsk{By: "jm"}, "the branch needs 2 approving reviews"},
		{"auto with an unproven claim", func(it *Item) { it.Proof = append(it.Proof, Claim{Text: "no regressions"}) }, MergeAsk{Auto: true}, "auto-merge needs every claim proven; 1 of 2 are"},
		{"auto with no claims", func(it *Item) { it.Proof = nil }, MergeAsk{Auto: true}, "auto-merge needs every claim proven; 0 of 0 are"},
	} {
		it := mergeable()
		tc.change(&it)
		v := MergeGate(it, tc.ask)
		if v.OK || v.Why != tc.why || v.Commit != "" {
			t.Errorf("%s: verdict = %+v, want refusal %q", tc.name, v, tc.why)
		}
	}
}

// A person may merge a repository with no checks, which they can see; a
// failed earlier merge does not block asking again.
func TestMergeGateLetsAPersonMergeWithoutChecksOrAfterAFailedTry(t *testing.T) {
	it := mergeable()
	it.PullRequest.Checks = PRChecks{}
	it.Merging = &MergeRun{State: MergeFailed, Why: "the forge refused"}
	if v := MergeGate(it, MergeAsk{By: "jm"}); !v.OK {
		t.Fatalf("verdict = %+v", v)
	}
}

// The same job always gets the same answer: the gate reads nothing but it.
func TestMergeGateIsDeterministic(t *testing.T) {
	it := mergeable()
	it.PullRequest.Checks.Pending = 1
	first := MergeGate(it, MergeAsk{Auto: true})
	for i := 0; i < 50; i++ {
		if got := MergeGate(it, MergeAsk{Auto: true}); got != first {
			t.Fatalf("run %d = %+v, first = %+v", i, got, first)
		}
	}
}
