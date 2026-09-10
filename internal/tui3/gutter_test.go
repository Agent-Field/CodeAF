package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// THE READING GUTTER (gutter.go). These are the questions the pass has to keep
// answering: that the air is there, that the row is no wider for it, that a
// frame with no columns to spare does not buy it, and — the whole reason the
// pass is more than a `"  " +` — that every target a click resolves BY COLUMN
// moved with the words it names.
//
// Every column assertion below reads the span back against THE DRAWN ROW. A
// test that read a card's span table and compared it with itself would agree
// with any gutter at all, including a wrong one: that is exactly how a shift
// like this ships green and answers clicks two columns to the left.

// gutterLab is an ordinary conversation at a width that can afford the gutter,
// with one thing the person said and one thing the model answered.
func gutterLab(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 100, 24
	a.entries = []entry{
		{kind: entryUser, text: "why is the suite red"},
		{kind: entryAssistant, text: "The parser guard was reverted. I put it back.", settled: true},
	}
	a.touch()
	return a
}

// cellsOf is the text a span actually covers on the row it was recorded
// against — the one reading that can tell a moved span from a stale one.
func cellsOf(text string, from, to int) string { return ansi.Cut(ansi.Strip(text), from, to) }

// THE AIR IS THERE, AND THE ROW IS NO WIDER FOR IT. Both halves matter: a
// gutter that did not also narrow the layout would be a transcript two columns
// wider than the column it is drawn in, which [app.railJoin] cuts back with an
// ellipsis at the right edge.
func TestTheTranscriptIsReadTwoColumnsIn(t *testing.T) {
	a := gutterLab(t)
	body := a.bodyWidth()
	said := false
	for _, r := range rows(a) {
		flat := plain(r.text)
		if strings.TrimSpace(flat) == "" {
			continue
		}
		said = true
		if !strings.HasPrefix(flat, strings.Repeat(" ", spacingConversationLead)) {
			t.Fatalf("a transcript row opens flush against the frame: %q", flat)
		}
		if got := ansi.StringWidth(flat); got > body {
			t.Fatalf("a row is %d cells in a %d-cell column: %q", got, body, flat)
		}
	}
	if !said {
		t.Fatalf("the fixture drew nothing:\n%s", strings.Join(plainRows(a), "\n"))
	}
}

// A BLANK ROW STAYS BLANK. Two spaces on a row with nothing on it is trailing
// whitespace on every gap in the transcript, which a yank takes with it.
func TestAGapInTheTranscriptCarriesNoGutter(t *testing.T) {
	a := gutterLab(t)
	gaps := 0
	for _, r := range rows(a) {
		if strings.TrimSpace(plain(r.text)) != "" {
			continue
		}
		gaps++
		if r.text != "" {
			t.Fatalf("a blank row carries %d cells of whitespace: %q", ansi.StringWidth(r.text), r.text)
		}
	}
	if gaps == 0 {
		t.Fatalf("the fixture has no gap in it, so this case tests nothing:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
}

// A PHONE IN A TERMINAL PAYS NOTHING, and the floor is stated against the width
// the rows are LAID OUT at rather than the width the frame has — so the gutter
// can never be the thing that pushes a frame down a tier and collapses the
// indent law underneath it (gutter.go's [textGutterCols], workfold.go's
// [workIndent]).
func TestATinyFrameBuysNoGutterAndKeepsItsIndent(t *testing.T) {
	for _, tc := range []struct {
		width, want int
	}{
		{width: 40, want: 0},
		{width: 59, want: 0},
		{width: 61, want: 0},
		{width: 62, want: spacingConversationLead},
		{width: 100, want: spacingConversationLead},
	} {
		if got := textGutterCols(tc.width); got != tc.want {
			t.Fatalf("at %d columns the gutter is %d, want %d", tc.width, got, tc.want)
		}
		// AND THE TWO LAWS AGREE AT EVERY WIDTH. Wherever the gutter is bought,
		// the rows under it are still laid out above the phone floor, so the work
		// indent is still bought too.
		if tc.want > 0 && workIndent(gutterInner(tc.width)) == "" {
			t.Fatalf("at %d columns the gutter cost the indent law its two cells", tc.width)
		}
	}

	// AND THE FRAME AGREES WITH THE FUNCTION. The model's own prose carries no
	// lead of its own, so on a frame this narrow it opens on column zero.
	a := gutterLab(t)
	a.width = 59
	a.touch()
	if got := textGutterCols(a.bodyWidth()); got != 0 {
		t.Fatalf("a 59-column frame bought %d cells of gutter", got)
	}
	said := false
	for _, r := range rows(a) {
		if !strings.Contains(plain(r.text), "parser guard") {
			continue
		}
		said = true
		if strings.HasPrefix(plain(r.text), " ") {
			t.Fatalf("a phone tier frame indented the answer anyway: %q", plain(r.text))
		}
	}
	if !said {
		t.Fatalf("the answer is not on the narrow frame:\n%s", strings.Join(plainRows(a), "\n"))
	}
}

// A LINK'S COLUMNS NAME THE WORDS IT WAS CUT FROM. This is the assertion the
// whole pass exists for: the span is read back against the row the frame drew,
// so a gutter applied to the text and not to the span fails here.
func TestATaskLinksColumnsMoveWithItsWords(t *testing.T) {
	a := linkLab(t, "The parser guard landed under task 7 and the suite is green.")
	r, _, ok := linkedRow(a)
	if !ok {
		t.Fatalf("nothing on the frame carries a link:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if textGutterCols(a.bodyWidth()) == 0 {
		t.Fatalf("the fixture is too narrow to have a gutter at all")
	}
	link := r.links[0]
	if got := cellsOf(r.text, link.span.from, link.span.to); got != "task 7" {
		t.Fatalf("the link's columns cover %q, not the words it names:\n%q", got, plain(r.text))
	}
}

// AND SO DO A SETTLED TASK'S ANSWERS. They are the most-pressed columns on this
// surface and they live on a card rather than on the row, which is why they need
// a pass of their own ([app.gutterCards]).
//
// THE CARD IS THE SETTLE ONE because it is the only block left in the transcript
// with answers on it. The task proposal's went to the question block above the
// box (task.go), and the standing card's followed them (standing.go) — and the
// chrome is drawn at the frame's own width with no gutter to pay, which is
// exactly why this pass has to be tested against a card that has one.
func TestACardsAnswersCoverTheChipsThatWereDrawn(t *testing.T) {
	card := &taskDone{chips: []settleChip{
		{span: hudSpan{from: 0, to: 9}},
		{span: hudSpan{from: 12, to: 20}},
	}}
	gutDoneCard(card, 2)
	if card.gut != 2 {
		t.Fatalf("the card carries %d cells of gutter, the pass bought 2", card.gut)
	}
	if card.chips[0].span != (hudSpan{from: 2, to: 11}) || card.chips[1].span != (hudSpan{from: 14, to: 22}) {
		t.Fatalf("the answers landed at %+v, two cells right of where they were drawn is %+v",
			[]hudSpan{card.chips[0].span, card.chips[1].span},
			[]hudSpan{{from: 2, to: 11}, {from: 14, to: 22}})
	}
}

// THE GUTTER IS PAID ONCE, HOWEVER MANY FRAMES GO BY. A card keeps its own
// geometry across a pass that did not redraw it, so a pass that ADDED two cells
// rather than settling a difference would walk its answers off the end of the
// row one frame at a time (gutter.go's [app.gutterCards]).
func TestTheGutterIsPaidOnceHoweverManyFramesGoBy(t *testing.T) {
	card := &taskDone{chips: []settleChip{{span: hudSpan{from: 0, to: 9}}}}
	gutDoneCard(card, 2)
	first := card.chips[0].span
	for range 5 {
		gutDoneCard(card, 2)
	}
	if card.chips[0].span != first {
		t.Fatalf("the answer drifted from %+v to %+v over five passes", first, card.chips[0].span)
	}
	// AND A WIDTH THAT CHANGED SETTLES THE DIFFERENCE rather than adding to it.
	gutDoneCard(card, 5)
	if card.chips[0].span != (hudSpan{from: 5, to: 14}) {
		t.Fatalf("a wider gutter put the answer at %+v, want it five cells in from where it was drawn", card.chips[0].span)
	}
}

// A TASK'S PAGE IS A TRANSCRIPT AND IS READ AS ONE. It went flush to the left
// edge for the same reason the conversation did (room.go's [app.roomRows]).
func TestATaskPageIsReadTwoColumnsInToo(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"put the parser guard back"}`,
		`{"type":"message","role":"assistant","content":"The guard is back and the suite is green."}`)
	a.openRoom(7, "Fix the nil-map crash")
	a.touch()
	if textGutterCols(a.bodyWidth()) == 0 {
		t.Fatalf("the fixture is too narrow to have a gutter at all")
	}
	said := false
	for _, r := range a.roomRows(a.bodyWidth()) {
		flat := plain(r.text)
		if strings.TrimSpace(flat) == "" {
			continue
		}
		said = true
		if !strings.HasPrefix(flat, strings.Repeat(" ", spacingConversationLead)) {
			t.Fatalf("a row of a task's page opens flush against the frame: %q", flat)
		}
		if got := ansi.StringWidth(flat); got > a.bodyWidth() {
			t.Fatalf("a page row is %d cells in a %d-cell column: %q", got, a.bodyWidth(), flat)
		}
	}
	if !said {
		t.Fatalf("the page drew nothing:\n%s", roomText(a))
	}
}

// A YANK PASTES WHAT WAS SAID AND NOT THE FRAME IT WAS SAID IN. The gutter is
// furniture; the indent under it is the block's own hierarchy and stays
// (copymode.go's [copyClean]).
func TestAYankLiftsTheGutterAndKeepsTheIndent(t *testing.T) {
	if got := copyClean("  the parser guard is back", spacingConversationLead); got != "the parser guard is back" {
		t.Fatalf("a yank of a guttered row pasted %q", got)
	}
	if got := copyClean("    ok  ", spacingConversationLead); got != "  ok" {
		t.Fatalf("a yank of a guttered WORK row pasted %q, losing its hierarchy", got)
	}
	if got := copyClean("  the parser guard is back", 0); got != "  the parser guard is back" {
		t.Fatalf("a yank on a frame with no gutter lifted %q anyway", got)
	}
}
