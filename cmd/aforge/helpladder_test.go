package main

import (
	"strings"
	"testing"
)

// THE LADDER IS PRINTED ONCE PER GROUP, NOT ONCE PER COMMAND.
//
// One source of truth was already satisfied in the CODE — all three verbs
// interpolated `exitLadderHelp` rather than spelling their own numbers — and
// the page was still wrong, because a reader does not see the code. `do`,
// `exec` and `run` each carried the same four lines verbatim, twelve of the
// page's hundred and ten spent saying one thing three times, in the same
// document whose own defect row was that more than half of it was an
// environment table. The three verbs end the same way, so the page says so
// once, under the group, where the sentence that distinguishes them already
// lived.
func TestTheHelpPageStatesTheExitLadderOncePerGroupAndNotOncePerCommand(t *testing.T) {
	page := usageText
	// The LONGEST rung's words, taken from [exitLadder] so this cannot drift
	// when a rung is respelled — and longest because the first rung's `Short` is
	// the word `done`, which appears three times in this page as ordinary
	// English. A needle a reader would never notice is a needle that counts
	// prose, and this test failed for exactly that reason before it passed.
	needle := exitLadder[0].Short
	for _, rung := range exitLadder {
		if len(rung.Short) > len(needle) {
			needle = rung.Short
		}
	}
	switch n := strings.Count(page, needle); {
	case n == 0:
		t.Fatalf("the help page never states the exit ladder, so a script's author has "+
			"nowhere to read what a number means:\n%s", page)
	case n > 1:
		t.Fatalf("the help page states the exit ladder %d times — a reader who has read it "+
			"under `do` skips it under `exec` and `run`, and %d rows are spent being "+
			"skipped:\n%s", n, (n-1)*4, page)
	}
	// And it is still THERE — a page that simply dropped the ladder would pass
	// a count of one just as happily as a count of zero would fail it, so the
	// rungs are checked against the table that defines them.
	for _, rung := range exitLadder {
		if !strings.Contains(page, rung.Short) {
			t.Fatalf("the help page never says what exit %d means (%q):\n%s", rung.Code, rung.Short, page)
		}
	}
}
