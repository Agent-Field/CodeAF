package factory

import "testing"

func TestCheckpointCannotSatisfyAnyCompletionRule(t *testing.T) {
	for _, until := range []string{"", UntilDone, UntilGreen, UntilClean, UntilProven} {
		r := StageResult{Checkpoint: true, Done: true, Claims: []Claim{{Text: "holds", OK: true, Evidence: "observed", Medium: "test"}}}
		if Met(Stage{Until: until}, r) {
			t.Fatalf("checkpoint met %q", until)
		}
	}
}
