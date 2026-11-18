package factory

import "testing"

// acceptedProof is a landed job whose proof step was taken as it stands with
// one claim shown and one not.
func acceptedProof() Item {
	shown := Claim{Text: "the ledger balances", OK: true, Evidence: "focused tests passed", Medium: "test"}
	missing := Claim{Text: "no secret is committed", OK: false, Evidence: "a key-shaped string in testdata", Medium: "test"}
	proof := Phase{Name: "proof", Kind: StageChat, State: PhaseDone, Round: 3, Result: &StageResult{Done: true, Claims: []Claim{shown, missing}}}
	AcceptStep(&proof, "the string is a test fixture")
	return Item{ID: 12, Kind: KindIssue, State: StateLanded, Cap: 10, Autonomy: AutonomyAuto, Assignee: "sam", Publish: "pr", HeadCommit: "abc",
		Proof:  []Claim{shown, missing},
		Stages: []Stage{{Name: "write", Kind: StageChat, On: true}, {Name: "proof", Kind: StageChat, On: true, Until: UntilProven}},
		Stream: &Stream{Cur: 1, Phases: []Phase{
			{Name: "write", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Done: true}},
			proof,
		}}}
}

// A CLAIM AN ACCEPTED STEP DID NOT SHOW IS ACCEPTED WITH THE STEP. It no
// longer stands: it is not among the findings the publish approval refuses
// for, Auto does not pause on it and repairs nothing. The row stays on the
// sheet, failing, for the approval and the pull request to show.
func TestAnAcceptedStepsUnshownClaimNoLongerStands(t *testing.T) {
	it := acceptedProof()
	missing := it.Proof[1]
	if !FindingAccepted(it, OverrideProof, missing) || FindingStands(it, OverrideProof, missing) {
		t.Fatal("the accepted step's unshown claim still stands")
	}
	if got := FailingFindings(it); len(got) != 0 {
		t.Fatalf("failing findings: %+v", got)
	}
	if why := AutonomyBlockReason(it); why != "" {
		t.Fatalf("Auto is paused: %s", why)
	}
	if _, due := AutoRepairDue(it); due || OffersAutoResume(it) {
		t.Fatal("Auto repairs, or offers to resume past, a finding that was accepted")
	}
	if it.Proof[1].OK || len(AcceptedSteps(it)) != 1 {
		t.Fatalf("the sheet or the acceptance was changed: %+v, %+v", it.Proof, AcceptedSteps(it))
	}
	// A claim that is shown needs no acceptance, and reads false.
	if FindingAccepted(it, OverrideProof, it.Proof[0]) {
		t.Fatal("a shown claim reads as accepted")
	}
}

// WHAT AN ACCEPTANCE DOES NOT COVER still stands: the same claim reported
// failing by a step nobody accepted, the step run again since, a claim no
// accepted step reports, and a repository policy row.
func TestAnAcceptanceCoversOnlyItsOwnStepsClaims(t *testing.T) {
	missing := Claim{Text: "no secret is committed", OK: false, Medium: "test"}
	cases := map[string]func(*Item){
		"another step reports it and was not accepted": func(it *Item) {
			it.Stream.Phases = append(it.Stream.Phases, Phase{Name: "review", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Done: true, Claims: []Claim{missing}}})
		},
		"the step ran again and reports it still": func(it *Item) {
			it.Stream.Phases[1].Result = &StageResult{Done: false, Claims: []Claim{missing}}
			it.Stream.Phases[1].State = PhaseWaiting
		},
		"the step ran again and was not accepted": func(it *Item) {
			it.Stream.Phases[1].Result = &StageResult{Done: true, Claims: []Claim{missing}}
		},
	}
	for name, change := range cases {
		it := acceptedProof()
		change(&it)
		if FindingAccepted(it, OverrideProof, missing) || !FindingStands(it, OverrideProof, missing) {
			t.Errorf("%s: the claim reads as accepted", name)
		}
	}
	it := acceptedProof()
	other := Claim{Text: "the migration is reversible", OK: false, Medium: "test"}
	if FindingAccepted(it, OverrideProof, other) {
		t.Error("a claim no accepted step reports reads as accepted")
	}
	// The accepted step itself reports the policy row: it stands all the same.
	policy := Claim{Text: "no secret is committed", OK: false, Medium: "policy"}
	it.Policy = []Claim{policy}
	it.Stream.Phases[1].Result.Claims = append(it.Stream.Phases[1].Result.Claims, policy)
	if FindingAccepted(it, OverridePolicy, policy) || !FindingStands(it, OverridePolicy, policy) {
		t.Error("a repository policy row reads as accepted")
	}
}
