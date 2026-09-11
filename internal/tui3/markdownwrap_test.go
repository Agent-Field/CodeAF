package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// P5: markdown and code at phone width.
//
// What is proved here is the LAW, not the layout: a wrapped code line is marked
// as wrapped, an unbreakable token is broken rather than lost, a table stacks,
// and none of it happens one cell above the phone floor. The bytes a heading is
// painted in belong to internal/tui2/prose and its suite; a test here that
// pinned them would fail the day that palette was tuned.

// phoneCols is a phone frame: under [layoutTier]'s 60-cell floor, and the width
// the port's other slices assert against.
const phoneCols = 44

// plainRows renders at a stated width with no colour at all, so a row's bytes
// are the cells a reader sees. Every width assertion in this file measures the
// painted rendering too — see mdWidthCeiling.
func mdPlainRows(t *testing.T, src string, width int) []string {
	t.Helper()
	return renderMarkdownWith(tokens.NewStyler(tokens.NoColor, tokens.FocusNormal), src, width)
}

const mdCodeSample = "before\n\n```go\nfunc handle(ctx context.Context, req *Request, out chan<- Result) error {\n\treturn nil\n}\n```\n\nafter\n"

func TestPhoneCodeWrapsWithHangingMarker(t *testing.T) {
	rows := mdPlainRows(t, mdCodeSample, phoneCols)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, mdContMark) {
		t.Fatalf("no continuation marker in a wrapped block:\n%s", joined)
	}

	var marked, plainLead int
	for _, row := range rows {
		if !strings.Contains(row, tokens.GlyphCodeGutter) {
			continue
		}
		switch {
		case strings.HasPrefix(row, mdContMark):
			marked++
		case strings.HasPrefix(row, mdContLead):
			plainLead++
		default:
			t.Fatalf("code row opens on neither margin: %q", row)
		}
	}
	if marked == 0 || plainLead == 0 {
		t.Fatalf("want both marked and unmarked code rows, got %d/%d:\n%s", marked, plainLead, joined)
	}

	// The signature the model wrote is longer than the column, and none of it
	// may be lost to an ellipsis.
	flat := strings.Join(strings.Fields(strings.ReplaceAll(joined, mdContMark, " ")), " ")
	for _, want := range []string{"func handle(ctx", "chan<- Result)", "error {"} {
		if !strings.Contains(flat, want) {
			t.Errorf("wrapped code dropped %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "…") {
		t.Errorf("a phone-tier fence was truncated:\n%s", joined)
	}

	// The prose around it is still prose, in order.
	if b, a := indexRow(rows, "before"), indexRow(rows, "after"); b < 0 || a < 0 || b > a {
		t.Errorf("prose around the fence was lost or reordered: %d/%d\n%s", b, a, joined)
	}
	mdWidthCeiling(t, mdCodeSample, phoneCols)
}

func TestPhoneCodeHardWrapsUnbrokenToken(t *testing.T) {
	const url = "https://example.com/a/very/long/path/that/never/breaks/anywhere?token=0123456789abcdef0123456789abcdef"
	src := "```\n" + url + "\n```\n"
	rows := mdPlainRows(t, src, phoneCols)
	if len(rows) < 2 {
		t.Fatalf("an unbroken token did not wrap: %#v", rows)
	}
	// Every cell of the token survives, in order, once the margin and the
	// gutter are taken off.
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(mdCodeCell(row))
	}
	if got := b.String(); got != url {
		t.Errorf("hard wrap lost or reordered cells:\n got %q\nwant %q", got, url)
	}
	mdWidthCeiling(t, src, phoneCols)
}

func TestPhoneTableStacks(t *testing.T) {
	const src = "| Task | Model | Cost |\n| --- | --- | --- |\n| refactor the parser | opus | $1.20 |\n| write the tests | sonnet | $0.04 |\n"
	rows := mdPlainRows(t, src, phoneCols)
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"Task: refactor the parser", "Model: opus", "Cost: $1.20", "Task: write the tests", "Cost: $0.04"} {
		if !strings.Contains(joined, want) {
			t.Errorf("stacked table missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "…") {
		t.Errorf("a phone-tier table was fitted rather than stacked:\n%s", joined)
	}
	// Records are separated, and a header is never repeated as a row of its own.
	if strings.Contains(joined, "Task: Task") {
		t.Errorf("header row rendered as a record:\n%s", joined)
	}
	mdWidthCeiling(t, src, phoneCols)
}

// TestPhoneTableOddRows is the shapes a model actually sends: a row short of
// cells, a row over, an empty cell, and a pipe that was escaped rather than
// meant. None of them may invent a field or drop a named one.
func TestPhoneTableOddRows(t *testing.T) {
	const src = "| a \\| b | c | d |\n| --- | --- | --- |\n| short |\n| p |  | r | dropped |\n"
	joined := strings.Join(mdPlainRows(t, src, phoneCols), "\n")
	for _, want := range []string{"a | b: short", "a | b: p", "d: r"} {
		if !strings.Contains(joined, want) {
			t.Errorf("stacked table missing %q:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"dropped", "c:"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("stacked table invented %q:\n%s", unwanted, joined)
		}
	}
	mdWidthCeiling(t, src, phoneCols)
}

func TestPhoneKeepsQuoteBarAndListMarkers(t *testing.T) {
	const src = "> a quoted sentence long enough that it has to wrap at a phone width\n\n1. the first item is also long enough to wrap across two rows at least\n2. second\n"
	rows := mdPlainRows(t, src, phoneCols)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, tokens.GlyphProseQuote) {
		t.Errorf("blockquote lost its bar:\n%s", joined)
	}
	if !strings.Contains(joined, "1.") || !strings.Contains(joined, "2.") {
		t.Errorf("list lost its markers:\n%s", joined)
	}
	mdWidthCeiling(t, src, phoneCols)
}

// TestPhoneRewrapIsIdempotent is the streaming and resize contract: the rows are
// a pure function of content and width, so rendering the same document twice —
// and rendering it at a width it was already rendered at — produces the same
// bytes.
func TestPhoneRewrapIsIdempotent(t *testing.T) {
	st := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	for _, width := range []int{phoneCols, 52, 38, phoneCols} {
		first := strings.Join(renderMarkdownWith(st, mdCodeSample, width), "\n")
		second := strings.Join(renderMarkdownWith(st, mdCodeSample, width), "\n")
		if first != second {
			t.Fatalf("re-render at %d differs from the first:\n%s\n---\n%s", width, first, second)
		}
	}
	// A resize away and back lands on the same rows it left.
	before := strings.Join(renderMarkdownWith(st, mdCodeSample, phoneCols), "\n")
	renderMarkdownWith(st, mdCodeSample, 100)
	if after := strings.Join(renderMarkdownWith(st, mdCodeSample, phoneCols), "\n"); after != before {
		t.Errorf("a resize round trip changed the rows:\n%s\n---\n%s", before, after)
	}
}

// TestAFenceHasNoTierBoundaryAndATableStillDoes replaces a test that
// pinned the OPPOSITE law: it asserted that every tier above the phone rendered
// exactly what prose renders, byte for byte, and that 59 cells took a separate
// "phone path". That was true when a long line inside a fence was CUT at wide
// widths and only wrapped on a narrow one — and cutting it was the defect,
// because there is no horizontal scroll anywhere on this surface, so at 160
// columns the tail of a line of code was simply gone. The tier boundary went
// with it — FOR FENCES. A TABLE still has one, and should: at 59 cells a grid
// cannot be drawn as a grid, so it stacks into `Task: refactor` rows, and that
// is a real judgement about tables rather than a leftover of the old code path.
// So the law worth pinning is the one that is actually true: a fence is wrapped
// at every width, and a table keeps the narrow treatment it always had.
func TestAFenceHasNoTierBoundaryAndATableStillDoes(t *testing.T) {
	st := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	const table = "| Task | Model |\n| --- | --- |\n| refactor | opus |\n"

	// PROSE THAT IS NOT CODE IS UNTOUCHED. The wave changed fences and nothing
	// else, so at every width from the table tier up, a table must still come
	// out of the markdown path byte for byte as prose draws it.
	for _, width := range []int{60, 61, 80, 100, 120, 200} {
		want := strings.Join(proseRows(st, table, width), "\n")
		if got := strings.Join(renderMarkdownWith(st, table, width), "\n"); got != want {
			t.Fatalf("a table at %d cells no longer renders as prose does:\n%s\n---\n%s", width, got, want)
		}
	}
	// And one cell under it the table stacks, which is the boundary that stays.
	if got := strings.Join(renderMarkdownWith(st, table, 59), "\n"); !strings.Contains(got, "Task: refactor") {
		t.Fatalf("a table at 59 cells did not stack:\n%s", got)
	}

	// AND A FENCE IS WRAPPED AT EVERY WIDTH, with no width at which the tail of
	// a line goes missing. `mdCodeSample`'s first line is 73 cells, so it has a
	// remainder to lose at 60 and none at 200 — both are checked, and the row
	// that carries a remainder says so.
	for _, width := range []int{59, 60, 61, 80, 100, 120, 200} {
		rows := mdPlainRows(t, mdCodeSample, width)
		joined := strings.Join(rows, "\n")
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("at %d cells a row is %d wide:\n%q", width, ansi.StringWidth(row), row)
			}
		}
		// Nothing is dropped: every word of the source survives somewhere in
		// the drawn block, which is the whole point of wrapping over cutting.
		flat := strings.Join(strings.Fields(strings.ReplaceAll(joined, mdContMark, " ")), " ")
		for _, word := range strings.Fields("func handle(ctx context.Context, req *Request, out chan<- Result) error {") {
			if !strings.Contains(flat, word) {
				t.Fatalf("at %d cells the fence lost %q:\n%s", width, word, joined)
			}
		}
		// A cut would have left an ellipsis where the tail used to be.
		if strings.Contains(joined, "…") {
			t.Fatalf("at %d cells a fence line was cut rather than wrapped:\n%s", width, joined)
		}
	}
}

// TestPhoneCodeSurvivesTint is the colour half: the wrap is applied to a painted
// rendering too, the marker is painted at the chrome tier and NOT at the ink the
// code around it uses, and the row still measures at most the column.
func TestPhoneCodeSurvivesTint(t *testing.T) {
	st := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	rows := renderMarkdownWith(st, mdCodeSample, phoneCols)
	var sawMarker, sawColour bool
	for _, row := range rows {
		if ansi.StringWidth(row) > phoneCols {
			t.Errorf("painted row over the column: %d cells in %q", ansi.StringWidth(row), row)
		}
		if strings.HasPrefix(ansi.Strip(row), strings.TrimRight(mdContMark, " ")) {
			sawMarker = true
			if !strings.HasPrefix(row, "\x1b") {
				t.Errorf("continuation marker is unpainted: %q", row)
			}
		}
		if strings.Contains(ansi.Strip(row), "func handle") && strings.Count(row, "\x1b") < 2 {
			t.Errorf("syntax tinting did not survive the wrap: %q", row)
		}
		if strings.Contains(row, "\x1b") {
			sawColour = true
		}
	}
	if !sawMarker || !sawColour {
		t.Fatalf("want a painted marker and painted code, got %v/%v", sawMarker, sawColour)
	}
}

// TestPhoneUnterminatedFenceWraps is the streaming case: a fence that is still
// being typed has no closing line, and it must wrap while it grows rather than
// wait for a delimiter that has not arrived.
func TestPhoneUnterminatedFenceWraps(t *testing.T) {
	src := "```go\nfunc handle(ctx context.Context, req *Request, out chan<- Result) error {"
	rows := mdPlainRows(t, src, phoneCols)
	if len(rows) < 2 || !strings.Contains(strings.Join(rows, "\n"), mdContMark) {
		t.Errorf("an open fence did not wrap:\n%s", strings.Join(rows, "\n"))
	}
	mdWidthCeiling(t, src, phoneCols)
}

// TestPhoneNestedFenceStillRenders guards the deliberate gap: a fence inside a
// list item is not pulled out of it, and the list must still render with its
// marker and inside the column.
func TestPhoneNestedFenceStillRenders(t *testing.T) {
	const src = "- an item\n\n  ```go\n  x := 1\n  ```\n"
	rows := mdPlainRows(t, src, phoneCols)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "an item") || !strings.Contains(joined, "x := 1") {
		t.Errorf("nested fence lost content:\n%s", joined)
	}
	mdWidthCeiling(t, src, phoneCols)
}

// mdWidthCeiling is the one assertion every case makes: NO row, painted or not,
// is wider than the column it was rendered for. It is the failure this whole
// slice exists to prevent, and the one that breaks the frame around it.
func mdWidthCeiling(t *testing.T, src string, width int) {
	t.Helper()
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.ANSI256, tokens.TrueColor} {
		st := tokens.NewStyler(profile, tokens.FocusNormal)
		for _, row := range renderMarkdownWith(st, src, width) {
			if w := ansi.StringWidth(row); w > width {
				t.Errorf("row over the column at profile %v: %d cells in %q", profile, w, row)
			}
			if strings.ContainsAny(row, "\n\r") {
				t.Errorf("row carries a newline: %q", row)
			}
		}
	}
}

// mdCodeCell is what a code row actually SAYS: the margin, the gutter and the
// ground taken off, leaving the source the model wrote.
func mdCodeCell(row string) string {
	row = ansi.Strip(row)
	row = strings.TrimPrefix(strings.TrimPrefix(row, mdContMark), mdContLead)
	row = strings.TrimPrefix(row, tokens.GlyphCodeGutter)
	return strings.TrimSpace(row)
}

// indexRow is the first row whose text contains needle, or -1.
func indexRow(rows []string, needle string) int {
	for i, row := range rows {
		if strings.Contains(ansi.Strip(row), needle) {
			return i
		}
	}
	return -1
}

// ── A LINE OF CODE IS NEVER CUT, AT ANY WIDTH ───────────────────────────────

// THE DEFECT: a fenced line longer than the frame was truncated and there was no
// way to see the rest — no door, no key, no horizontal scroll, and at 80 columns
// not even an ellipsis to say bytes were missing. The same answer was whole at
// 160 and cut at 120, which is the kind of difference that sends somebody
// hunting a bug in their own code that is not there.
//
// The wrap the phone tier has always had now runs at every width
// (markdown.go's [segmentedMarkdown]), so the test is the same sentence asked of
// four frames: every byte of the source is on the screen, and the rows that
// carry the remainder say so with [mdContMark].
func TestALineOfCodeIsWrappedRatherThanCutAtEveryWidth(t *testing.T) {
	const long = "func (a *app) servedRiderAt(width int) string { room := width; if room >= 0 { room -= ansi.StringWidth(riderLead) } }"
	doc := "Here is the change.\n\n```go\n" + long + "\nshort()\n```\n\nAnd after."

	for _, width := range []int{160, 120, 80, 60, 44} {
		rows := renderMarkdown(doc, width)
		var code []string
		for _, row := range rows {
			plainRow := plain(row)
			if !copyCodeRow(plainRow) {
				continue
			}
			trimmed := strings.TrimLeft(plainRow, " ")
			trimmed = strings.TrimPrefix(trimmed, mdContMark)
			code = append(code, strings.TrimSpace(strings.TrimPrefix(trimmed, tokens.GlyphCodeGutter)))
		}
		joined := strings.Join(code, " ")
		if strings.Contains(joined, glyphMore) {
			t.Fatalf("at %d columns the fence is still truncated:\n%s", width, strings.Join(code, "\n"))
		}
		// EVERY WORD OF THE SOURCE IS ON THE SCREEN. Joined back on the wrap
		// points, the drawn rows say what was written.
		for _, word := range strings.Fields(long) {
			if !strings.Contains(joined, word) {
				t.Fatalf("at %d columns the fence lost %q:\n%s", width, word, strings.Join(code, "\n"))
			}
		}
		// AND NO ROW OVERSHOOTS THE FRAME it was drawn for.
		for _, row := range rows {
			if got := ansi.StringWidth(plain(row)); got > width {
				t.Fatalf("at %d columns a row is %d cells wide: %q", width, got, plain(row))
			}
		}
	}
}

// AND A ROW THAT CARRIES THE REST OF A LINE SAYS SO, so a reader can tell one
// long line from two short ones — which is the whole of what a wrap owes the
// person reading source.
func TestAWrappedCodeRowIsMarkedAndAnUnwrappedOneIsNot(t *testing.T) {
	doc := "```go\nx := 1\n```"
	for _, row := range renderMarkdown(doc, 120) {
		if strings.Contains(plain(row), mdContMark) {
			t.Fatalf("a line that fitted was marked as a continuation: %q", plain(row))
		}
	}

	long := "x := " + strings.Repeat("aVeryLongIdentifier + ", 12) + "1"
	marked := 0
	for _, row := range renderMarkdown("```go\n"+long+"\n```", 120) {
		if strings.Contains(plain(row), mdContMark) {
			marked++
		}
	}
	if marked == 0 {
		t.Fatalf("a line too long for 120 columns was drawn with no continuation marker at all:\n%s",
			strings.Join(renderMarkdown("```go\n"+long+"\n```", 120), "\n"))
	}
}

// AND COPY MODE STILL SEES ONE BLOCK. `a` selects the run of code rows around
// the cursor, and it read that run off the gutter alone — so a wrapped line
// ENDED the run and the yank took the top half of the block. The paste carries
// the source and neither the hairline nor the marker.
func TestCopyModeTakesAWrappedFenceWholeAndPastesNoMarkers(t *testing.T) {
	long := "x := " + strings.Repeat("aVeryLongIdentifier + ", 12) + "1"
	rows := renderMarkdown("```go\n"+long+"\ny := 2\n```", 120)
	text := make([]string, len(rows))
	for i, row := range rows {
		text[i] = plain(row)
	}
	c := &copyMode{text: text}

	at := -1
	for i, row := range text {
		if strings.Contains(row, mdContMark) {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("nothing wrapped, so this case tests nothing:\n%s", strings.Join(text, "\n"))
	}
	from, to, ok := c.fenceAt(at)
	if !ok {
		t.Fatalf("a wrapped code row is not read as part of a fence: %q", text[at])
	}
	if !strings.Contains(text[to], "y := 2") {
		t.Fatalf("the block was cut at the wrapped row: rows %d-%d end on %q", from, to, text[to])
	}
	var pasted []string
	for _, row := range text[from : to+1] {
		// These rows came straight from [renderMarkdown] and never went through
		// the transcript's pass, so there is no reading gutter on them to lift.
		pasted = append(pasted, copyClean(row, 0))
	}
	joined := strings.Join(pasted, "")
	if strings.Contains(joined, mdContMark) || strings.Contains(joined, tokens.GlyphCodeGutter) {
		t.Fatalf("the paste carries the frame's own marks:\n%q", joined)
	}
}
