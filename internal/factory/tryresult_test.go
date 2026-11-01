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
