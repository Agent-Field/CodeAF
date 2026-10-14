package tui3

// ── THE QUOTED HALF OF A LANDED CARD ────────────────────────────────────────
//
// The card's second row quotes what the work came to, and the report it quotes
// is the model's own markdown. A model that opens with a fenced block — a diff,
// a command's output, a JSON result — spends that row on the fence marker itself
// unless the reading skips it, and `"```"` is the row saying nothing at all
// (#889). The exhaustive table of that reading lives in internal/reportline;
// these pin that the card is wired to it.

import (
	"strings"
	"testing"
	"time"
)

// THE FIRST PROSE LINE'S RULE — a leading fence is skipped, blank lines are
// skipped, a fence-only report quotes the line inside it, an empty report says
// nothing — is pinned once in internal/reportline, which firstProseLine now wraps.
// The two cards below prove the surface is wired to that reading.

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
