package tui3

// ── THE JOB PAGE AND THE RECORD CARD DROP WHOLE HINTS ───────────────────────
//
// Both feet were painted through a plain [fit], which is a character ruler with
// no idea what a clause is, so a narrow terminal ended them `· m puts it in
// yo…` — a sheet that names a key and then eats it. The structural law in
// narrow_test.go now covers every foot on the surface with an empty ledger;
// these are the two behaviours behind it, said as a person meets them.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A NARROW FOOT LOSES A KEY, NEVER HALF OF ONE — AND NEVER THE WAY OUT. The way
// out is the last clause on both feet precisely so that [hintFit], which keeps
// its final clause to the last cell there is, spends it last.
func TestTheJobPageAndTheRecordCardKeepTheWayOutOnANarrowFoot(t *testing.T) {
	for _, tc := range []struct{ name, keys, out string }{
		{"a running job", jobPageKeysRun, jobPageBackWord},
		{"a settled job", jobPageKeysOver, jobPageBackWord},
		{"a task's record card", taskCardKeys, taskCardBackWord},
	} {
		if !strings.HasSuffix(tc.keys, tc.out) {
			t.Fatalf("%s's foot is\n\t%q\nand the clause that says how to leave has to be last, because that is the one the fitter keeps: %q",
				tc.name, tc.keys, tc.out)
		}
		for _, width := range []int{160, 120, 80, 60, 40, 24} {
			line := hintFit(tc.keys, width-2)
			if ansi.StringWidth(line) > width-2 {
				t.Fatalf("%s's foot at %d columns is %d cells:\n\t%q", tc.name, width, ansi.StringWidth(line), line)
			}
			if strings.Contains(line, glyphMore) {
				t.Fatalf("%s's foot at %d columns is cut mid-clause:\n\t%q\nhints drop whole clauses", tc.name, width, line)
			}
			if width > 24 && !strings.Contains(line, tc.out) {
				t.Fatalf("%s's foot at %d columns is\n\t%q\nand it gave away the way out, %q, before the keys in front of it",
					tc.name, width, line, tc.out)
			}
			// EVERY CLAUSE THAT SURVIVED IS ONE THAT WAS THERE. A foot that
			// re-spelled a key at a narrow width would be two sheets to learn.
			for _, part := range strings.Split(line, railSep) {
				if !strings.Contains(tc.keys, part) {
					t.Fatalf("%s's foot at %d columns says %q, which is not a clause of\n\t%q", tc.name, width, part, tc.keys)
				}
			}
		}
	}
}

// AND THE RANK IS THE ONE THE PAGE ARGUES FOR: a job that is still running keeps
// `x stop it` longest of the four, because stopping something is the one thing on
// that page a person cannot reach from anywhere else.
func TestARunningJobsFootKeepsStopLongestOfItsFourKeys(t *testing.T) {
	line := hintFit(jobPageKeysRun, 44)
	if !strings.Contains(line, "x stop it") {
		t.Fatalf("a running job's foot at 46 columns is\n\t%q\nand it dropped %q before the keys behind it", line, "x stop it")
	}
	if strings.Contains(line, "m puts it in your message") {
		t.Fatalf("a running job's foot at 46 columns is\n\t%q\nand it is wider than the clauses it should have dropped", line)
	}
	if !strings.HasSuffix(line, jobPageBackWord) {
		t.Fatalf("a running job's foot at 46 columns is\n\t%q\nand it should still end in %q", line, jobPageBackWord)
	}
}
