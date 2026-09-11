package tui3

// ── THE QUOTED HALF OF A LANDED CARD ────────────────────────────────────────
//
// The card's second row quotes what the work came to, and the report it quotes
// is the model's own markdown. A model that opens with a fenced block — a diff,
// a command's output, a JSON result — spends that row on the fence marker itself
// unless the reading skips it, and `"```"` is the row saying nothing at all
// (#889). These pin every road the reading can take.

import (
	"strings"
	"testing"
	"time"
)

// THE FIRST PROSE LINE, OVER THE TABLE. A leading fence is skipped whether a
// language word follows it or not, blank lines are skipped, a report that is
// nothing but a fence quotes the first line inside it, and a report with no
// prose at all returns nothing — which is the emptiness law's case, not this
// reader's.
func TestFirstProseLineSkipsFencesBlankLinesAndWrappers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report string
		want   string
	}{
		{"a report that opens with prose", "the seven files are written and the suite passes", "the seven files are written and the suite passes"},
		{"a leading fence is skipped", "```\nthe seven files are written and the suite passes\n```", "the seven files are written and the suite passes"},
		{"a fence's language word is skipped too", "```go\npackage main\n```\nthe suite passes", "package main"},
		{"a tilde fence is skipped too", "~~~\nthe suite passes\n~~~", "the suite passes"},
		{"a leading blank line is skipped", "\n\nthe suite passes", "the suite passes"},
		{"blank lines inside a fence are skipped", "```\n\nthe suite passes\n```", "the suite passes"},
		{"a report that is nothing but a fence quotes its first line", "```\n7 files changed, 15 insertions(+)\n```", "7 files changed, 15 insertions(+)"},
		{"a closed fence with nothing inside falls on", "```\n```\nthe suite passes", "the suite passes"},
		{"an unclosed fence with nothing yet inside falls on", "```go\n\nthe suite passes", "the suite passes"},
		{"an indented fence marker is still a fence", "  ```\nthe suite passes", "the suite passes"},
		{"an empty report says nothing", "", ""},
		{"a report of blank lines says nothing", "\n \n", ""},
		{"a report of fence markers alone says nothing", "```\n", ""},
	} {
		if got := firstProseLine(tc.report); got != tc.want {
			t.Errorf("%s: firstProseLine(%q) = %q, want %q", tc.name, tc.report, got, tc.want)
		}
	}
}

// A DONE CARD SKIPS A FENCE AND QUOTES THE FIRST PROSE LINE, which is #889
// whole: a quick task lands with a fenced block in front of its answer and the
// row is spent on the answer rather than on the punctuation around it.
func TestADoneCardSkipsAFenceAndQuotesTheFirstProseLine(t *testing.T) {
	a, _, _ := taskApp(t)
	began := a.now().Add(-10 * time.Minute)
	card := &taskDone{
		title:   "Fix nil-map crash",
		outcome: firstProseLine("```\nthe seven files are written and the suite passes\n```"),
		started: began,
	}
	under := plain(a.doneUnder(card, 120))
	if !strings.Contains(under, `"the seven files are written and the suite passes"`) {
		t.Fatalf("the card's second row reads\n\t%q\nand it should quote the first prose line, not the fence:\n\t%q", under, `"the seven files are written and the suite passes"`)
	}
	if strings.Contains(under, "```") {
		t.Fatalf("the card's second row still quotes a fence marker:\n\t%q", under)
	}
	if !strings.Contains(under, "started "+began.Format("15:04")) {
		t.Fatalf("the card's second row is missing its start stamp:\n\t%q", under)
	}
}

// A DONE CARD THAT LANDS WITH NO PROSE TO QUOTE DRAWS THE TAIL ALONE. The
// emptiness law owns this case — no empty pair of quotation marks claiming the
// work said something, and no fence marker standing in for a sentence either.
func TestADoneCardWithNoProseDrawsNoEmptyQuotes(t *testing.T) {
	a, _, _ := taskApp(t)
	began := a.now().Add(-10 * time.Minute)
	card := &taskDone{title: "Fix nil-map crash", outcome: firstProseLine("```\n"), started: began}
	under := plain(a.doneUnder(card, 120))
	if strings.Contains(under, `""`) {
		t.Fatalf("a card with nothing to quote drew an empty pair of quotes:\n\t%q", under)
	}
	if !strings.Contains(under, "started "+began.Format("15:04")) {
		t.Fatalf("the card's second row lost its start stamp:\n\t%q", under)
	}
}
