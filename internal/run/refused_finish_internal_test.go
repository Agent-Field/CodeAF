package run

import "testing"

func TestOnlyARefusedOnlyFinishIsIncomplete(t *testing.T) {
	cases := []struct {
		name     string
		past     []Step
		refused  bool
		executed bool
	}{
		{
			name:    "a refusal with nothing else run",
			past:    []Step{{Command: "rm -rf .git", NotRun: true, Refused: true, Observation: "is outside your copy"}},
			refused: true,
		},
		{
			name: "a refusal followed by real recovery work",
			past: []Step{
				{Command: "rm -rf .git", NotRun: true, Refused: true},
				{Command: "printf x > add.py", ExitCode: new(int)},
			},
			refused: true, executed: true,
		},
		{
			name: "a direct read-only completion",
			past: []Step{{Command: "plandb done root --agent root --result ok", ExitCode: new(int)}},
		},
		{
			name:     "a compound finish carries another action",
			past:     []Step{{Command: "printf x > add.py && plandb done root --agent root --result ok", ExitCode: new(int)}},
			executed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refused, executed, _ := attemptedWork(tc.past)
			if refused != tc.refused || executed != tc.executed {
				t.Fatalf("attemptedWork = refused %v, executed %v; want %v, %v", refused, executed, tc.refused, tc.executed)
			}
		})
	}
}

func TestOnlyPlanFinishRecognizesNoOtherShellAction(t *testing.T) {
	for _, command := range []string{
		"plandb done root --agent root --result ok",
		"plandb done root --agent root --result 'words with spaces'",
		"plandb done root --agent root --result 'research R&D; no change'",
	} {
		if !onlyPlanFinish(command) {
			t.Errorf("%q was not recognized as only a finish", command)
		}
	}
	for _, command := range []string{
		"printf x > add.py && plandb done root --agent root --result ok",
		"plandb done root --agent root --result ok; echo x",
		"echo plandb done root",
		"plandb wait root --agent root",
	} {
		if onlyPlanFinish(command) {
			t.Errorf("%q was read as only a finish", command)
		}
	}
}
