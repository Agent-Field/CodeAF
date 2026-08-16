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

// TestWideTiersUnchanged is the no-regression half: every tier above the phone
// renders exactly what prose renders, byte for byte.
func TestWideTiersUnchanged(t *testing.T) {
	st := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	const table = "| Task | Model |\n| --- | --- |\n| refactor | opus |\n"
	for _, src := range []string{mdCodeSample, table} {
		for _, width := range []int{60, 61, 80, 100, 120, 200} {
			want := proseRows(st, src, width)
			got := renderMarkdownWith(st, src, width)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("width %d no longer renders as prose does:\n%s\n---\n%s",
					width, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		}
	}
	// 60 is the floor and it belongs to the tier above: the phone path starts
	// one cell under it.
	if got := renderMarkdownWith(st, mdCodeSample, 59); strings.Join(got, "\n") == strings.Join(proseRows(st, mdCodeSample, 59), "\n") {
		t.Errorf("59 cells did not take the phone path")
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
