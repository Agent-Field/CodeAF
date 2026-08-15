package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// 13.3.2: pipe tables rendered as soup. The three rungs of the ladder, and the
// invariant that there is no fourth.

const sampleTable = "| model | cost | note |\n" +
	"|-------|------|------|\n" +
	"| K3 | $8.65 | the long one that has to wrap somewhere sensible |\n" +
	"| flash | $0.02 | cheap |\n"

func tableRows(t *testing.T, text string, width, indent int) []string {
	t.Helper()
	rows := prose{base: tokens.TextPrimary}.rows(nil, text, width, indent)
	for i := range rows {
		rows[i] = ansi.Strip(rows[i])
		if got := ansi.StringWidth(rows[i]); got > width {
			t.Fatalf("row %d is %d cells wide at width %d: %q", i, got, width, rows[i])
		}
	}
	return rows
}

// Rung 1: it fits, so it is a grid — aligned columns, a hairline under the
// header, and no pipes left anywhere.
func TestATableThatFitsRendersAsAGrid(t *testing.T) {
	rows := tableRows(t, sampleTable, 100, 0)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "|") {
		t.Fatalf("a pipe survived into the rendering:\n%s", joined)
	}
	if !strings.Contains(rows[0], "model") || !strings.Contains(rows[0], "cost") {
		t.Fatalf("the header row is %q", rows[0])
	}
	if !strings.Contains(rows[1], tokens.GlyphTreeDash) {
		t.Fatalf("the header/body seam has no rule: %q", rows[1])
	}
	// The columns line up: `cost` and every value under it start in the same
	// cell. Continuation rows of a wrapped cell carry no money and are not part
	// of the claim.
	at := strings.Index(rows[0], "cost")
	found := 0
	for _, row := range rows[2:] {
		cell := strings.Index(row, "$")
		if cell < 0 {
			continue
		}
		found++
		if cell != at {
			t.Fatalf("the money column starts at %d, not %d: %q", cell, at, row)
		}
	}
	if found != 2 {
		t.Fatalf("the grid has %d money cells, not 2:\n%s", found, joined)
	}
}

// Rung 2: the widest column gives cells back and its content WRAPS inside the
// column, so one record becomes several screen rows and nothing is lost.
func TestANarrowTableWrapsInsideItsCells(t *testing.T) {
	rows := tableRows(t, sampleTable, 46, 0)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "|") {
		t.Fatalf("a pipe survived into the rendering:\n%s", joined)
	}
	// Every word of the long cell is still on screen somewhere.
	for _, word := range []string{"the", "long", "one", "wrap", "sensible"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("wrapping lost %q:\n%s", word, joined)
		}
	}
	if len(rows) <= 4 {
		t.Fatalf("a squeezed table did not wrap at all:\n%s", joined)
	}
}

// Rung 3: below the point where every column can hold a readable stub, the grid
// stops being a grid and each record becomes its own labelled group. This is
// the designed degradation, and it is why 24 columns still reads correctly.
func TestAVeryNarrowTableDegradesToLabelledRecords(t *testing.T) {
	rows := tableRows(t, sampleTable, 18, 0)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "|") {
		t.Fatalf("a pipe survived into the rendering:\n%s", joined)
	}
	if !strings.Contains(joined, "model") || !strings.Contains(joined, "K3") {
		t.Fatalf("the degraded rendering lost its labels or values:\n%s", joined)
	}
	// A record group puts its label and its value on the same row, which is what
	// makes it readable without columns.
	found := false
	for _, row := range rows {
		if strings.Contains(row, "model") && strings.Contains(row, "K3") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no record row carried a label beside its value:\n%s", joined)
	}
}

// The recognizer needs the delimiter row. A sentence about shell pipes is prose
// and must be left exactly as it was written.
func TestProseWithPipesIsNotATable(t *testing.T) {
	rows := tableRows(t, "use a | b to pipe one into the other", 60, 0)
	if len(rows) != 1 || !strings.Contains(rows[0], "|") {
		t.Fatalf("a sentence with a pipe was reformatted: %q", rows)
	}
}

// Inside a fence, a table is preformatted content and stays as written — a
// reflowed code block is a code block that no longer runs.
func TestATableInsideAFenceIsLeftAlone(t *testing.T) {
	rows := tableRows(t, "```\n| a | b |\n|---|---|\n| 1 | 2 |\n```", 60, 0)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "| a | b |") {
		t.Fatalf("a fenced table was reformatted:\n%s", joined)
	}
}

// The invariant the whole ladder exists for: no width, at any indent, produces
// a row wider than the terminal or a panic.
func TestTablesSurviveEveryWidth(t *testing.T) {
	for width := 1; width <= 120; width++ {
		for _, indent := range []int{0, bodyIndent} {
			tableRows(t, sampleTable, width, indent)
		}
	}
}

// And it survives ragged input: a row with fewer cells than the header, a row
// with more, and an escaped pipe inside a cell.
func TestRaggedTablesDoNotLoseOrInventCells(t *testing.T) {
	ragged := "| a | b | c |\n|---|---|---|\n| 1 |\n| 1 | 2 | 3 | 4 |\n| x \\| y | 2 | 3 |\n"
	rows := tableRows(t, ragged, 60, 0)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "x | y") {
		t.Fatalf("the escaped pipe did not survive as a literal:\n%s", joined)
	}
	if strings.Contains(joined, "| 4") {
		t.Fatalf("an extra cell leaked through as prose:\n%s", joined)
	}
}

// End to end: a table in a real journaled reply is dressed, not dumped.
func TestATableInAJournaledReplyIsDressed(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "Here is the ledger:\n\n" + sampleTable})
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	out := whole(t, app, 100)
	if strings.Contains(out, "|---") {
		t.Fatalf("the delimiter row reached the transcript:\n%s", out)
	}
	if !strings.Contains(out, "$8.65") {
		t.Fatalf("the table's values are missing:\n%s", out)
	}
}
