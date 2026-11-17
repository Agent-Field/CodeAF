package factory

import (
	"strings"
	"testing"
	"time"
)

// landedWithFinding is an Auto job that landed with a failing repository
// policy finding before it has a pull request.
func landedWithFinding() Item {
	return Item{Autonomy: AutonomyAuto, Assignee: "sam", Cap: 5, State: StateLanded,
		Proof:  []Claim{{Text: "tests pass", OK: true, Evidence: "go test"}},
		Policy: []Claim{{Text: "FINDING: the changelog entry carries the issue number", Medium: "policy"}},
		Stream: &Stream{Spent: 1, Phases: []Phase{{Name: "write", State: PhaseDone}, {Name: "approve", Kind: StageGate, State: PhaseWaiting}}}}
}

// A FAILING FINDING IN AUTO IS A REPAIR, NOT A PAUSE: the repair's request is
// the finding, the attempt is counted on the run, and the job's history says
// so. Once the attempts are used up the job pauses on the finding exactly as
// it did before.
func TestAFailingFindingInAutoStartsARepairUntilTheAttemptsRunOut(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	it := landedWithFinding()
	for n := 1; n <= AutoRepairLimit; n++ {
		r, ok := AutoRepairDue(it)
		if !ok || r.OnPR || r.Attempt != n || r.Limit != AutoRepairLimit {
			t.Fatalf("attempt %d: due=%v repair=%+v", n, ok, r)
		}
		got, err := BeginAutoRepair(&it, now)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Words(), "FINDING: the changelog entry carries the issue number") || !strings.Contains(got.Words(), "verify the fix") {
			t.Fatalf("the repair request does not carry the finding: %q", got.Words())
		}
		if it.Stream.AutoRepairs != n {
			t.Fatalf("attempts counted %d, want %d", it.Stream.AutoRepairs, n)
		}
		if last := it.Activity[len(it.Activity)-1].What; !strings.HasPrefix(last, "Auto repair ") || !strings.Contains(last, "repository policy: FINDING") {
			t.Fatalf("the job's history reads %q", last)
		}
	}
	if r, ok := AutoRepairDue(it); ok {
		t.Fatalf("a repair is still due after %d attempts: %+v", AutoRepairLimit, r)
	}
	if _, err := BeginAutoRepair(&it, now); err == nil {
		t.Fatal("a repair began with the attempts used up")
	}
	// THE FALLBACK IS TODAY'S PAUSE, word for word.
	if why := AutonomyBlockReason(it); why != "Repository policy needs attention: FINDING: the changelog entry carries the issue number. Request changes or take over to continue." {
		t.Fatalf("the person sees %q", why)
	}
	if !OffersAutoResume(it) {
		t.Fatal("the person's resume is no longer offered once the repairs ran out")
	}
	// A NEW RUN STARTS COUNTING AGAIN.
	it.Stream = &Stream{Phases: []Phase{{Name: "write", State: PhaseDone}}}
	if _, ok := AutoRepairDue(it); !ok {
		t.Fatal("a new run inherited the old run's repair count")
	}
}

// MANUAL IS UNCHANGED: the person decides, so nothing repairs on its own; and
// a pause on anything but a failing finding keeps its own door.
func TestNoAutoRepairInManualOrForAnotherPause(t *testing.T) {
	for name, change := range map[string]func(*Item){
		"manual":      func(it *Item) { it.Autonomy = AutonomyManual },
		"take over":   func(it *Item) { it.Manual = true },
		"budget":      func(it *Item) { it.Stream.Spent = 5 },
		"no budget":   func(it *Item) { it.Cap = 0 },
		"publishing":  func(it *Item) { it.Publishing = true },
		"rule hold":   func(it *Item) { it.RuleHold = "protected · auth/ · auth/login.go" },
		"publish err": func(it *Item) { it.PublishError = "GitHub access expired" },
		"clean":       func(it *Item) { it.Policy[0].OK = true; it.Policy[0].Evidence = "fixed" },
		"overridden": func(it *Item) {
			it.Overrides = []CheckOverride{{Kind: OverridePolicy, Text: it.Policy[0].Text, By: "sam"}}
		},
		"approving": func(it *Item) { it.Approval = &Approval{Kind: "proof", State: "approving"} },
	} {
		it := landedWithFinding()
		change(&it)
		if r, ok := AutoRepairDue(it); ok {
			t.Errorf("%s: a repair is due: %+v", name, r)
		}
	}
	it := landedWithFinding()
	it.Autonomy = AutonomyManual
	before := len(it.Activity)
	if _, err := BeginAutoRepair(&it, time.Now()); err == nil || it.State != StateLanded || len(it.Activity) != before || it.Stream.AutoRepairs != 0 {
		t.Fatalf("Manual changed: err=%v state=%s activity=%d repairs=%d", err, it.State, len(it.Activity), it.Stream.AutoRepairs)
	}
}

// ON AN OPEN PULL REQUEST THE REPAIR IS A FOLLOW PR ROUND: the finding is the
// round's request, the round counts against Follow PR's own limit, and at the
// limit the job keeps today's pause.
func TestAFailingFindingOnAnOpenPRQueuesAFollowPRRepair(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	it := landedWithFinding()
	it.Publish = "pr"
	it.HeadCommit = strings.Repeat("b", 40)
	it.PullRequest = &PullRequest{Number: 1758, State: PROpen, HeadSHA: strings.Repeat("a", 40), ChecksRead: true, ReviewsRead: true}
	it.FollowUp = &PRFollowUp{Kind: "pr", State: "waiting_approval", Attempt: 1, Limit: DefaultPRRepairAttempts}
	EnsureFollowPR(&it)

	r, err := BeginAutoRepair(&it, now)
	if err != nil {
		t.Fatal(err)
	}
	f := it.FollowUp
	if !r.OnPR || r.Attempt != 2 || f.Attempt != 2 || f.State != "queued" || it.State != StateQueued || !strings.Contains(f.Request, "FINDING: the changelog entry carries the issue number") {
		t.Fatalf("repair=%+v follow=%+v state=%s", r, f, it.State)
	}
	if len(it.Policy) != 0 || len(it.Proof) != 0 {
		t.Fatal("the repaired round does not start from a fresh sheet")
	}

	// AT FOLLOW PR'S LIMIT THE JOB COMES TO THE PERSON AS TODAY.
	spent := landedWithFinding()
	spent.Publish, spent.HeadCommit, spent.PullRequest = it.Publish, it.HeadCommit, it.PullRequest
	spent.FollowUp = &PRFollowUp{Kind: "pr", State: "waiting_approval", Attempt: DefaultPRRepairAttempts, Limit: DefaultPRRepairAttempts}
	if r, ok := AutoRepairDue(spent); ok {
		t.Fatalf("a repair is due past Follow PR's limit: %+v", r)
	}
	if why := AutonomyBlockReason(spent); !strings.HasPrefix(why, "Repository policy needs attention: FINDING") {
		t.Fatalf("the person sees %q", why)
	}
}

// THE MANAGER RE-OFFERS THE MERGE ONLY WHERE THE PAGE OFFERS IT: an open pull
// request the gate passes. Otherwise it is refused with the gate's words and
// nothing is stamped.
func TestOfferMergeStampsOnlyAnOpenPullRequestTheGatePasses(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 123, time.UTC)
	it := mergeable()
	if err := OfferMerge(&it, now); err != nil {
		t.Fatal(err)
	}
	if !it.MergeOfferedAt.Equal(now.Truncate(time.Second)) {
		t.Fatalf("stamped %v", it.MergeOfferedAt)
	}

	for name, change := range map[string]func(*Item){
		"no pull request": func(it *Item) { it.PullRequest = nil },
		"merged":          func(it *Item) { it.PullRequest.State = PRMerged },
		"conflicts":       func(it *Item) { it.PullRequest.Conflict = true; it.PullRequest.Mergeable = MergeableNo },
		"failing checks":  func(it *Item) { it.PullRequest.Checks = PRChecks{Failed: 1} },
		"merging":         func(it *Item) { it.Merging = &MergeRun{State: MergeAsked} },
	} {
		it := mergeable()
		change(&it)
		err := OfferMerge(&it, now)
		if err == nil || !it.MergeOfferedAt.IsZero() {
			t.Errorf("%s: offered (%v), stamp %v", name, err, it.MergeOfferedAt)
		}
	}
}
