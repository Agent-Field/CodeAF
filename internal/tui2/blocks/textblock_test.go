package blocks

import (
	"strconv"
	"strings"
	"testing"
)

// The settled head is a promise: rows below it can move, rows above it never do.
func TestSettledHeadIsByteStable(t *testing.T) {
	b := NewText("turn", Header{Glyph: "◐", Title: "aforge"})
	const width = 30

	var head []string
	for i := 0; i < 60; i++ {
		b.Write("word" + strconv.Itoa(i) + " ")
		settled := b.SettledRows(width)
		rows := b.Rows(width)
		if settled > len(rows) {
			t.Fatalf("chunk %d: settled %d rows of a %d-row block", i, settled, len(rows))
		}
		for r := 0; r < len(head) && r < settled; r++ {
			if rows[r] != head[r] {
				t.Fatalf("chunk %d: settled row %d changed:\n was %q\n now %q", i, r, head[r], rows[r])
			}
		}
		head = append(head[:0], rows[:settled]...)
	}
	if len(head) < 5 {
		t.Fatalf("60 words at width 30 settled only %d rows", len(head))
	}
}

// AppendRowsFrom must produce exactly the tail of Rows.
func TestAppendRowsFromMatchesRows(t *testing.T) {
	b := NewText("turn", Header{Glyph: "◐", Title: "aforge", Desc: "thinking"})
	b.Write(strings.Repeat("some words that will need to wrap a few times ", 6))
	const width = 28
	full := append([]string(nil), b.Rows(width)...)
	for start := 0; start <= len(full); start++ {
		got := b.AppendRowsFrom(nil, width, start)
		want := full[start:]
		if len(got) != len(want) {
			t.Fatalf("start %d: %d rows, want %d", start, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("start %d row %d: %q != %q", start, i, got[i], want[i])
			}
		}
	}
}

// The truncation law, end to end: a turn cut by an output cap renders visibly
// cut — in its header AND under its body.
func TestTruncatedTurnRendersVisiblyCut(t *testing.T) {
	tr := New(60, 12)
	turn := NewText("turn", Header{Glyph: "◐", Title: "aforge"})
	turn.Write("<svg viewBox=\"0 0 100 100\"><rect x=\"1\" y=\"1\"")
	tr.Append(turn)
	tr.Frame(base)

	turn.Finalize(EndTruncatedByCap)
	frame := tr.Frame(base)
	text := rowsOf(frame)

	if !strings.Contains(text, "cut off — output cap") {
		t.Fatalf("a capped turn did not say so:\n%s", text)
	}
	if strings.Count(text, "cut off") < 2 {
		t.Fatalf("the cut is marked only once; the header badge and the rule are both required:\n%s", text)
	}
	if !strings.Contains(text, CutMark) {
		t.Fatalf("no cut rule under the body:\n%s", text)
	}
	if turn.End() != EndTruncatedByCap {
		t.Fatalf("end state %v", turn.End())
	}

	// A clean turn carries no marks at all.
	clean := NewText("clean", Header{Glyph: "✓", Title: "aforge"})
	clean.Write("done")
	clean.Finalize(EndCompleted)
	if strings.Contains(strings.Join(clean.Rows(60), "\n"), "cut off") {
		t.Fatal("a completed turn was marked as cut")
	}
}

func TestFinalizeFreezesTheElapsedAndBumpsVersion(t *testing.T) {
	b := NewText("turn", Header{Title: "aforge"})
	b.StartClock(base)
	b.Elapsed().Sample(base.Add(90000000000)) // 90s
	before := b.Version()
	b.Finalize(EndCompleted)
	if b.Version() == before {
		t.Fatal("Finalize did not bump the version")
	}
	if !b.Elapsed().Frozen() {
		t.Fatal("Finalize did not freeze the elapsed cell")
	}
	if !b.IsFinalized() {
		t.Fatal("Finalize did not finalize")
	}
	b.Write("more text")
	if strings.Contains(strings.Join(b.Rows(40), "\n"), "more text") {
		t.Fatal("a finalized block accepted a write")
	}
}

func TestWrapAtNarrowWidths(t *testing.T) {
	for width := 1; width <= 12; width++ {
		b := NewText("t", Header{Glyph: "◐", Title: "aforge", Desc: "x"})
		b.Write("supercalifragilistic words and\nan explicit break")
		for _, row := range b.Rows(width) {
			if w := Width(row); w > width {
				t.Fatalf("width %d: row %q is %d cells", width, row, w)
			}
			if strings.Contains(row, "\n") {
				t.Fatalf("width %d: a row carries a newline", width)
			}
		}
	}
}

func TestWrapKeepsWordsAndHonoursBreaks(t *testing.T) {
	rows, _ := Wrap(nil, "the quick brown fox jumps", 10)
	want := []string{"the quick", "brown fox", "jumps"}
	if len(rows) != len(want) {
		t.Fatalf("rows %q, want %q", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("rows %q, want %q", rows, want)
		}
	}
	rows, _ = Wrap(nil, "one\ntwo", 40)
	if len(rows) != 2 || rows[0] != "one" || rows[1] != "two" {
		t.Fatalf("explicit breaks lost: %q", rows)
	}
	rows, last := Wrap(nil, "aaaa bbbb", 4)
	if rows[len(rows)-1] != "bbbb" || last != 5 {
		t.Fatalf("rows %q last %d", rows, last)
	}
}

// Depth is a real narrowing, not a shove: an indented block wraps inside the
// room it has left, so it never overflows the terminal it was drawn in.
func TestIndentNarrowsRatherThanOverflows(t *testing.T) {
	const width = 40
	for _, indent := range []int{0, 1, 2, 4, 9} {
		b := NewText("turn", Header{Glyph: "◐", Title: "aforge", Desc: "streaming a reply"})
		b.Indent = indent
		b.Write("the quick brown fox jumps over the lazy dog and keeps going for a while")
		b.Finalize(EndTruncatedByCap)
		rows := b.Rows(width)
		if len(rows) < 3 {
			t.Fatalf("indent %d: %d rows, expected a header, a body and a cut rule", indent, len(rows))
		}
		for i, row := range rows {
			if w := Width(row); w > width {
				t.Fatalf("indent %d row %d is %d cells: %q", indent, i, w, row)
			}
			if indent > 0 && !strings.HasPrefix(row, strings.Repeat(" ", indent)) {
				t.Fatalf("indent %d row %d does not sit at depth: %q", indent, i, row)
			}
		}
		if n := b.SettledRows(width); n != len(rows) {
			t.Fatalf("indent %d: a finalized block settled %d of %d rows", indent, n, len(rows))
		}
	}
}

// The whole reason the field exists (P1 seam 1): a streamed preview and the
// journaled twin it becomes must occupy the same columns, or the text jumps
// sideways at the moment the stream finalizes and reads as a different thing.
func TestIndentedPreviewLinesUpWithItsTwin(t *testing.T) {
	const (
		width  = 50
		indent = 3
		body   = "an answer that wraps across more than one row so the depth shows"
	)
	preview := NewText("preview", Header{Glyph: "◐", Title: "aforge"})
	preview.Indent = indent
	preview.Write(body)

	twin := NewText("journaled", Header{Glyph: "✓", Title: "aforge"})
	twin.Indent = indent
	twin.Write(body)
	twin.Finalize(EndCompleted)

	live, settled := preview.Rows(width), twin.Rows(width)
	if len(live) != len(settled) {
		t.Fatalf("preview has %d rows, its twin %d", len(live), len(settled))
	}
	for i := 1; i < len(live); i++ { // row 0 is the header; its glyph differs
		if live[i] != settled[i] {
			t.Fatalf("body row %d moved when the stream finalized:\n live %q\n twin %q",
				i, live[i], settled[i])
		}
	}
}

// Zero is not a special case with a shortcut — it is the old behaviour, byte
// for byte, and this is the test that says so.
func TestZeroIndentIsTheOldBehaviour(t *testing.T) {
	build := func(indent int) []string {
		b := NewText("t", Header{Glyph: "◐", Title: "aforge", Desc: "x"})
		b.Indent = indent
		b.Write("the quick brown fox jumps over the lazy dog")
		b.Finalize(EndInterrupted)
		return append([]string(nil), b.Rows(36)...)
	}
	flush, negative := build(0), build(-4)
	if len(flush) != len(negative) {
		t.Fatalf("a negative indent changed the row count: %d vs %d", len(flush), len(negative))
	}
	for i := range flush {
		if flush[i] != negative[i] {
			t.Fatalf("a negative indent moved row %d:\n %q\n %q", i, flush[i], negative[i])
		}
		if strings.HasPrefix(flush[i], " ") {
			t.Fatalf("a zero indent padded row %d: %q", i, flush[i])
		}
	}
}

// An indent wider than the terminal is clamped, not obeyed: something honest
// beats nothing at all, and no row may ever exceed the width it was given.
func TestIndentIsClampedNotObeyed(t *testing.T) {
	for width := 1; width <= 20; width++ {
		b := NewText("t", Header{Glyph: "◐", Title: "aforge"})
		b.Indent = 30
		b.Write("body text that has to go somewhere")
		rows := b.Rows(width)
		if len(rows) == 0 {
			t.Fatalf("width %d: an over-indented block rendered nothing", width)
		}
		for _, row := range rows {
			if w := Width(row); w > width {
				t.Fatalf("width %d: row %q is %d cells", width, row, w)
			}
		}
		if n := b.SettledRows(width); n > len(rows) {
			t.Fatalf("width %d: settled %d of %d rows", width, n, len(rows))
		}
	}
}

// The incremental door has to agree with the whole-block door at depth too,
// or a streaming frame paints a different indent than a rebuild does.
func TestIndentedAppendRowsFromMatchesRows(t *testing.T) {
	b := NewText("t", Header{Glyph: "◐", Title: "aforge"})
	b.Indent = 4
	b.Write("a body long enough to wrap several times at this width, several times over")
	rows := b.Rows(28)
	for start := 0; start <= len(rows); start++ {
		got := b.AppendRowsFrom(nil, 28, start)
		if len(got) != len(rows)-start {
			t.Fatalf("start %d: %d rows, want %d", start, len(got), len(rows)-start)
		}
		for i := range got {
			if got[i] != rows[start+i] {
				t.Fatalf("start %d row %d:\n got %q\nwant %q", start, i, got[i], rows[start+i])
			}
		}
	}
}

func TestEmptyTextBlockHasNoPhantomRow(t *testing.T) {
	b := NewText("t", Header{Glyph: "◐", Title: "aforge"})
	if got := len(b.Rows(40)); got != 1 {
		t.Fatalf("an empty body rendered %d rows, want just the header", got)
	}
	bare := NewText("bare", Header{})
	if got := len(bare.Rows(40)); got != 0 {
		t.Fatalf("a header-less empty block rendered %d rows", got)
	}
}

func TestStaticBlockVersionsItsChanges(t *testing.T) {
	s := NewStatic("s", "one", "two")
	if !s.IsFinalized() || s.Version() != 0 {
		t.Fatal("a static block starts finalized at version 0")
	}
	rows := s.Rows(40)
	if len(rows) != 2 || rows[0] != "one" {
		t.Fatalf("rows %q", rows)
	}
	s.SetRows("three")
	if s.Version() != 1 {
		t.Fatalf("version %d after SetRows", s.Version())
	}
	s.SetEnd(EndInterrupted)
	if len(s.Rows(40)) != 2 {
		t.Fatal("the cut rule did not join the rows")
	}
}
