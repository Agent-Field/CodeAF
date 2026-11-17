package factory

import (
	"strings"
	"testing"
	"time"
)

// landedWithFinding is an Auto job that landed with a failing repository
// policy finding before it has a pull request; its review step's latest
// round is the one that reported it.
func landedWithFinding() Item {
	finding := Claim{Text: "FINDING: the changelog entry carries the issue number", Evidence: "pr: 1758", Medium: "policy"}
	return Item{Autonomy: AutonomyAuto, Assignee: "sam", Cap: 5, State: StateLanded,
		Proof:  []Claim{{Text: "tests pass", OK: true, Evidence: "go test"}},
		Policy: []Claim{finding},
		Stream: &Stream{Spent: 1, Phases: []Phase{
			{Name: "write", State: PhaseDone},
			{Name: "review", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Findings: 1, Claims: []Claim{finding}}},
			{Name: "approve", Kind: StageGate, State: PhaseWaiting}}}}
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
	it.Stream = &Stream{Phases: landedWithFinding().Stream.Phases}
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

// job31 is codeaf#1758 as it stood on 2026-11-17 before Auto repaired it:
// Auto, landed on the open pull request #2587. Follow PR's readiness review
// (round 1, head abb64e5) reported the changelog finding as a policy claim;
// round 2 fixed it (f72f8f6883), verified it, and proved it in other words as
// a test claim. The failing policy row from round 1 is still on the sheet;
// every step that ran kept its latest round's report, as job 31's record does.
func job31() Item {
	finding := "FINDING: the changelog entry carries the issue number instead of this PR's number — docs/changes/unreleased/1758-fixed-exec-scratch.md is named 1758-... and sets pr: 1758, while docs/rules/changelog.md requires the pull request number and every sibling unreleased entry follows it; the plan deferred exactly this renumbering to PR open and it did not happen. Also missing a trailing newline in the same file."
	round2 := []Claim{
		{Text: "The changelog entry carries the pull request number in both filename and pr: field with a trailing newline — the sole open readiness finding is repaired", OK: true, Evidence: "git show f72f8f688; make changelog-check exit 0", Medium: "test"},
		{Text: "Measured checks verify the current repair checkout: the new e2e acceptance tests and scratchRootFor unit tests pass on HEAD f72f8f688", OK: true, Evidence: "go test exit 0", Medium: "test"},
	}
	review := []Claim{{Text: "Repository standards hold: gofmt clean on changed dirs, changelog entry well-formed, vet passed", OK: true, Evidence: "gofmt -l; make changelog-check", Medium: "policy"}}
	it := Item{Autonomy: AutonomyAuto, Assignee: "zeropoint95", Cap: 5, State: StateLanded, Publish: "pr",
		HeadCommit: "f72f8f6883e4d96efbed873779367fee53f0512c",
		RuleChecks: &RuleChecks{Publish: "passed", PublishHead: "f72f8f6883e4d96efbed873779367fee53f0512c"},
		Proof:      append([]Claim(nil), round2...),
		Policy:     append([]Claim{{Text: finding, Evidence: "docs/changes/unreleased/1758-fixed-exec-scratch.md: pr: 1758", Medium: "policy"}}, review...),
		PullRequest: &PullRequest{Number: 2587, State: PROpen, HeadSHA: "abb64e5b71a7c4a5caf32d71cbc2f900509fa534",
			BaseSHA: "ace7888ab16f8b6c911f10f5a9bad75ddebf0d01", ChecksRead: true, ReviewsRead: true, Checks: PRChecks{Passed: 7}},
		FollowUp: &PRFollowUp{Kind: "pr", State: "waiting_approval", HeadSHA: "abb64e5b71a7c4a5caf32d71cbc2f900509fa534", Attempt: 1, Limit: DefaultPRRepairAttempts},
		Stream: &Stream{Spent: 3.3, Cur: 6, Phases: []Phase{
			{Name: "plan", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Done: true, Claims: []Claim{
				{Text: "The exec door never calls WithScratch, so scratch defaults to the workspace root", OK: true, Evidence: "cmd/codeaf/exec.go", Medium: "transcript"}}}},
			{Name: "approve", Kind: StageGate, State: PhaseDone},
			{Name: "write", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Done: true, Output: "wired WithScratch on the exec and run doors"}},
			{Name: "test", Kind: StageChat, State: PhaseDone, Round: 2, Result: &StageResult{Done: true}},
			{Name: "review", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Done: true, Claims: review}},
			{Name: "proof", Kind: StageChat, State: PhaseDone, Round: 2, Result: &StageResult{Done: true}},
			{Name: FollowPRStep, Kind: StageChat, State: PhaseDone, Round: 2,
				Rounds: []RoundLine{{Round: 1, Rejected: true, Result: "not proven", Reason: "1 finding"}, {Round: 2, Result: "proven", Reason: "4 of 4 claims shown"}},
				Result: &StageResult{Done: true, Claims: round2}},
		}}}
	return it
}

// A FINDING A LATER ROUND REPAIRED IS NOT REPAIRED AGAIN, AND DOES NOT PAUSE
// AUTO. Job 31's failing policy row is round 1's verdict; Follow PR's latest
// round fixed and proved it in other words, so no step's latest round still
// reports it. Auto neither spends a Follow PR attempt fixing what is fixed
// nor pauses on it (the owner's decision of 2026-11-17). While the latest
// round still reports the finding, the repair is due as before.
func TestAFindingALaterRoundRepairedIsNotRepairedAgain(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 56, 51, 0, time.UTC)
	it := job31()
	if got := CurrentFindings(it); len(got) != 0 {
		t.Fatalf("a superseded finding reads current: %+v", got)
	}
	if r, ok := AutoRepairDue(it); ok {
		t.Fatalf("Auto would repair a finding Follow PR already repaired: %+v", r)
	}
	if _, err := BeginAutoRepair(&it, now); err == nil || it.FollowUp.Attempt != 1 || it.FollowUp.State != "waiting_approval" || it.FollowUp.Request != "" || it.State != StateLanded || len(it.Activity) != 0 {
		t.Fatalf("a repair began: err=%v follow=%+v state=%s activity=%v", err, it.FollowUp, it.State, it.Activity)
	}
	if why := AutonomyBlockReason(it); why != "" {
		t.Fatalf("Auto pauses on a replaced finding: %q", why)
	}
	if OffersAutoResume(it) || len(FailingFindings(it)) != 0 {
		t.Fatalf("a replaced finding still reads failing: %+v", FailingFindings(it))
	}

	// THE SAME FINDING, STILL REPORTED BY FOLLOW PR'S LATEST ROUND, IS REPAIRED.
	live := job31()
	ph := &live.Stream.Phases[len(live.Stream.Phases)-1]
	ph.Round, ph.Rounds = 1, ph.Rounds[:1]
	ph.Result = &StageResult{Findings: 1, Claims: []Claim{{Text: live.Policy[0].Text, Evidence: "pr: 1758", Medium: "policy"}}}
	r, err := BeginAutoRepair(&live, now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OnPR || r.Attempt != 2 || len(r.Findings) != 1 || live.FollowUp.State != "queued" || !strings.Contains(live.FollowUp.Request, "FINDING: the changelog entry carries the issue number") {
		t.Fatalf("repair=%+v follow=%+v", r, live.FollowUp)
	}

	// A PROOF CLAIM IN THE SAME WORDS IS NOT THE POLICY ROW'S VERDICT.
	other := job31()
	other.Stream.Phases[len(other.Stream.Phases)-1].Result.Claims = append(other.Stream.Phases[len(other.Stream.Phases)-1].Result.Claims, Claim{Text: other.Policy[0].Text, OK: true, Evidence: "fixed", Medium: "test"})
	if _, ok := AutoRepairDue(other); ok {
		t.Fatal("a proof claim stood for the policy finding's verdict")
	}
}

// THE REPAIR REQUEST QUOTES EACH FINDING AS ITS ROW HOLDS IT: a claim in the
// quoted words lands on the row that failed (same words, and a policy claim
// for a policy row), where "repository policy: FINDING: …" was a new row and
// left the failing one standing.
func TestTheRepairRequestQuotesEachFindingAsItsRowHolds(t *testing.T) {
	it := landedWithFinding()
	it.Proof = append(it.Proof, Claim{Text: "the e2e test passes", Evidence: "exit 1"})
	it.Stream.Phases[1].Result.Claims = append(it.Stream.Phases[1].Result.Claims, Claim{Text: "the e2e test passes", Evidence: "exit 1", Medium: "test"})
	r, ok := AutoRepairDue(it)
	if !ok || len(r.Findings) != 2 {
		t.Fatalf("due=%v repair=%+v", ok, r)
	}
	words := r.Words()
	for _, want := range []string{
		"repository policy, reported with medium policy: “FINDING: the changelog entry carries the issue number”",
		"proof: “the e2e test passes”",
		"verify the fix",
		"exactly the quoted words",
	} {
		if !strings.Contains(words, want) {
			t.Errorf("the request lacks %q: %q", want, words)
		}
	}
	if strings.Contains(words, "repository policy: FINDING") {
		t.Errorf("a label sits inside the finding's words: %q", words)
	}
	for _, f := range r.Findings {
		start := strings.Index(words, "“"+f.Text+"”")
		if start < 0 {
			t.Fatalf("finding %q is not quoted whole", f.Text)
		}
		quoted := words[start+len("“") : start+len("“")+len(f.Text)]
		if ClaimKey(quoted) != ClaimKey(f.Text) {
			t.Fatalf("the quoted words %q do not find the row %q", quoted, f.Text)
		}
	}
}

// ONLY A VERDICT THE LATEST ROUND STILL REPORTS BLOCKS (the owner's decision
// of 2026-11-17). On job 31's record the replaced changelog row neither
// pauses Auto, reads failing (what the proof approval counts), nor holds the
// automatic merge; the same row still reported by Follow PR's latest round
// does all three as before; and a record that cannot tell (a step that ran kept no result, or
// no result carries claims) blocks as before.
func TestOnlyAFindingTheLatestRoundStillReportsBlocks(t *testing.T) {
	mergeReady := func(it Item) Item {
		it.FollowUp.State = "watching"
		it.HeadCommit = it.PullRequest.HeadSHA
		it.PullRequest.Mergeable = MergeableYes
		it.MergePolicy = &MergePolicy{Auto: true}
		return it
	}
	pauses := func(it Item) bool {
		return strings.HasPrefix(AutonomyBlockReason(it), "Repository policy needs attention: FINDING: the changelog entry carries the issue number")
	}

	replaced := mergeReady(job31())
	if pauses(replaced) || len(FailingFindings(replaced)) != 0 {
		t.Fatalf("a replaced finding pauses Auto: %q", AutonomyBlockReason(replaced))
	}
	if v := MergeGate(replaced, MergeAsk{Auto: true}); !v.OK {
		t.Fatalf("a replaced finding holds the merge: %+v", v)
	}
	if !FindingReplaced(replaced, OverridePolicy, replaced.Policy[0]) || FindingReplaced(replaced, OverridePolicy, replaced.Policy[1]) {
		t.Fatal("only the failing row no latest round reports is replaced")
	}

	// THE SAME ROW, STILL REPORTED BY THE LATEST ROUND, BLOCKS AS BEFORE.
	live := mergeReady(job31())
	ph := &live.Stream.Phases[len(live.Stream.Phases)-1]
	ph.Result = &StageResult{Done: true, Findings: 1, Claims: append(append([]Claim(nil), ph.Result.Claims...), Claim{Text: live.Policy[0].Text, Evidence: "pr: 1758", Medium: "policy"})}
	if !pauses(live) || len(FailingFindings(live)) != 1 {
		t.Fatalf("a standing finding does not pause Auto: %q", AutonomyBlockReason(live))
	}
	if v := MergeGate(live, MergeAsk{Auto: true}); v.OK || !strings.Contains(v.Why, "every claim proven") {
		t.Fatalf("a standing finding does not hold the merge: %+v", v)
	}

	// A RECORD THAT CANNOT TELL BLOCKS AS BEFORE.
	for name, change := range map[string]func(*Item){
		"a step that ran kept no result": func(it *Item) { it.Stream.Phases[2].Result = nil },
		"no step kept a result": func(it *Item) {
			for i := range it.Stream.Phases {
				it.Stream.Phases[i].Result = nil
			}
		},
		"no result carries claims (a reader's copy)": func(it *Item) {
			for i := range it.Stream.Phases {
				if r := it.Stream.Phases[i].Result; r != nil {
					it.Stream.Phases[i].Result = &StageResult{Done: r.Done, Output: r.Output}
				}
			}
		},
		"no stream": func(it *Item) { it.Stream = nil },
	} {
		old := mergeReady(job31())
		change(&old)
		if FindingReplaced(old, OverridePolicy, old.Policy[0]) {
			t.Errorf("%s: the row reads replaced", name)
		}
		if old.Stream != nil && !pauses(old) {
			t.Errorf("%s: Auto does not pause: %q", name, AutonomyBlockReason(old))
		}
		if v := MergeGate(old, MergeAsk{Auto: true}); v.OK {
			t.Errorf("%s: the merge is not held: %+v", name, v)
		}
	}

	// THE PERSON'S OVERRIDE STILL COVERS A STANDING FINDING.
	live.Overrides = []CheckOverride{{Kind: OverridePolicy, Text: live.Policy[0].Text, By: "zeropoint95"}}
	if pauses(live) {
		t.Fatalf("an overridden finding pauses Auto: %q", AutonomyBlockReason(live))
	}
}
