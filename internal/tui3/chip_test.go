package tui3

// The chip is the roster row's smallest unit: a glyph, a title, and the cap
// that keeps one loud task from eating the column. The strip's own spans are
// gone with the strip (ISSUE-126); what is pinned here is what the rail still
// draws.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestChipTitleIsFittedNotWrapped(t *testing.T) {
	a := &app{}
	node := &taskNode{id: 1, title: strings.Repeat("a very long title ", 8)}
	row := a.railTitle(node, node.title)
	if strings.Contains(row, "\n") {
		t.Fatal("a roster title must be one line")
	}
	if got := ansi.StringWidth(stripANSI(a.railTitle(node, fit(node.title, 10)))); got > 10 {
		t.Fatalf("a fitted title drew %d cells, want at most 10", got)
	}
}

func TestChipGlyphFollowsTheState(t *testing.T) {
	a := &app{}
	cases := []struct {
		state session.TaskState
		want  string
	}{
		{session.TaskDone, glyphDone},
		{session.TaskFailed, glyphBad},
		{session.TaskQueued, glyphQueued},
	}
	for _, tc := range cases {
		node := &taskNode{id: 1, state: tc.state}
		if got := a.taskStateMark(node); !strings.Contains(stripANSI(got), tc.want) {
			t.Fatalf("state %v drew %q, want the %q mark", tc.state, got, tc.want)
		}
	}
}

func TestChipTitleCapLeavesRoomForTheGlyph(t *testing.T) {
	// The row is glyph + space + title; the title's budget is the column minus
	// what the glyph and the tree stems already spent.
	a := &app{}
	node := &taskNode{id: 1, state: session.TaskRunning, title: strings.Repeat("word ", 40)}
	e := railEntry{node: node}
	rows, _, _ := a.railEntryRows(e, 24)
	if len(rows) == 0 {
		t.Fatal("a running node must draw a row")
	}
	if got := ansi.StringWidth(stripANSI(rows[0])); got > 24 {
		t.Fatalf("the head row drew %d cells in a 24-cell column", got)
	}
}

func TestChipPadKeepsTheColumnRectangular(t *testing.T) {
	// Every row the rail draws is padded to the column's width, so the seam
	// and the hover band have a rectangle to land on.
	a := &app{}
	node := &taskNode{id: 1, state: session.TaskRunning, title: "short"}
	e := railEntry{node: node}
	rows, _, _ := a.railEntryRows(e, 30)
	for i, row := range rows {
		if got := ansi.StringWidth(stripANSI(row)); got > 30 {
			t.Fatalf("row %d drew %d cells in a 30-cell column", i, got)
		}
	}
}

// stripANSI is the test-side plain of a painted row: what a person reads, with
// the paint taken off, so a width assertion is about the words and not the
// escape codes that carry them.
func stripANSI(s string) string { return ansi.Strip(s) }
