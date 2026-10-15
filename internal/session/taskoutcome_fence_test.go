package session

import "testing"

// A TASK OUTCOME SKIPS A LEADING FENCE AND KEEPS THE FIRST PROSE LINE. This is
// #931 at its source: taskOutcome writes TaskIndexEntry.Outcome, the field the
// tasks table, the home panels and the task record all quote, and a report that
// opens with a fenced block used to write three backticks into it while the
// landed card (fixed in #889) already showed the sentence. The reading now runs
// through reportline.FirstProse, the same door the card takes.
func TestTaskOutcomeSkipsAFence(t *testing.T) {
	const sentence = "the seven files are written and the suite passes"
	for _, tc := range []struct {
		name   string
		report string
		want   string
	}{
		{"a fenced block in front of the answer", "```\n" + sentence + "\n```", sentence},
		{"a fence with a language word", "```go\n" + sentence + "\n```", sentence},
		{"a report that is nothing but a fenced block keeps its first line", "```diff\n- old\n+ new\n```", "- old"},
		{"a report that opens with prose is unchanged", sentence, sentence},
		{"a report with nothing to say stays empty", "```\n```\n", ""},
	} {
		if got := taskOutcome(tc.report); got != tc.want {
			t.Errorf("%s: taskOutcome(%q) = %q, want %q", tc.name, tc.report, got, tc.want)
		}
	}
}
