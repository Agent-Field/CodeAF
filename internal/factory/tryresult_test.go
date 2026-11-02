package factory

import "testing"

// EACH FINISHED TRY SAYS HOW IT ENDED IN ONE WORD AND WHY IN A FEW, from the
// step's rule and its structured report only. The word is the rule met
// (clean, green, proven, done) or "not" it; the reason is counted from
// findings, exit, tests and claims, never read from what the step wrote.
func TestATrysResultWordAndReasonComeFromItsRuleAndReport(t *testing.T) {
	shown := Claim{Text: "retries survive a restart", OK: true, Evidence: "go test ./..."}
	unshown := Claim{Text: "batch path", OK: false}
	cases := []struct {
		name         string
		stage        Stage
		res          StageResult
		word, reason string
	}{
		{"review clean", Stage{Until: UntilClean}, StageResult{Done: true}, "clean", "no findings"},
		{"review with findings", Stage{Until: UntilClean}, StageResult{Done: true, Findings: 2, Output: "looks fine to me"}, "not clean", "2 findings"},
		{"review one finding", Stage{Until: UntilClean}, StageResult{Done: true, Findings: 1}, "not clean", "1 finding"},
		{"test green with counts", Stage{Kind: StageCheck, Until: UntilGreen}, StageResult{Done: true, Tests: &TestCounts{Passed: 12}}, "green", "12 passed"},
		{"test green without counts", Stage{Kind: StageCheck, Until: UntilGreen}, StageResult{Done: true}, "green", ""},
		{"test red", Stage{Kind: StageCheck, Until: UntilGreen}, StageResult{Done: true, Exit: 1, Tests: &TestCounts{Passed: 10, Failed: 2}}, "not green", "2 failed"},
		{"test red without counts", Stage{Kind: StageCheck, Until: UntilGreen}, StageResult{Done: true, Exit: 2}, "not green", "exit 2"},
		{"proof proven", Stage{Until: UntilProven}, StageResult{Done: true, Claims: []Claim{shown}}, "proven", "1 of 1 claims shown"},
		{"proof short", Stage{Until: UntilProven}, StageResult{Done: true, Claims: []Claim{shown, unshown}}, "not proven", "1 of 2 claims shown"},
		{"proof with no claims", Stage{Until: UntilProven}, StageResult{Done: true}, "not proven", "no claims"},
		{"plain step done", Stage{}, StageResult{Done: true, Output: "wrote it"}, "done", ""},
		{"no report", Stage{Until: UntilClean}, StageResult{}, "not clean", "no result"},
		{"blocked is not a failed try", Stage{Until: UntilGreen}, StageResult{ExecutionBlocked: "sandbox"}, "blocked", ""},
		{"a checkpoint is no finished try", Stage{Until: UntilDone}, StageResult{Checkpoint: true, Done: true}, "", ""},
	}
	for _, c := range cases {
		word, reason := TryResult(c.stage, c.res)
		if word != c.word || reason != c.reason {
			t.Errorf("%s: got %q · %q, want %q · %q", c.name, word, reason, c.word, c.reason)
		}
	}
}

// A LINE THAT SAYS A TRY WAS REJECTED NEVER KEEPS A WORD THAT SAID IT PASSED:
// a rerun's failure note for a try folded "clean" drops the word, while a note
// that only carries the try again keeps it.
func TestARoundsWordSurvivesOnlyANoteThatAgreesWithIt(t *testing.T) {
	ph := &Phase{}
	NoteRound(ph, 5, RoundLine{Round: 1, Chat: "c1", Result: "clean", Reason: "no findings"})
	NoteRound(ph, 5, RoundLine{Round: 1, Chat: "c1", Summary: "carried again"})
	if ph.Rounds[0].Result != "clean" || ph.Rounds[0].Reason != "no findings" {
		t.Fatalf("a note that only carries the try lost its word: %+v", ph.Rounds[0])
	}
	NoteRound(ph, 5, RoundLine{Round: 1, Chat: "c1", Rejected: true, Summary: "publish failed"})
	if ph.Rounds[0].Result != "" || ph.Rounds[0].Reason != "" {
		t.Fatalf("a rejected try kept the word of a passed one: %+v", ph.Rounds[0])
	}
}
