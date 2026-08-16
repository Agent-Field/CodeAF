package prose

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Untrusted input and the width ceiling.
//
// Every byte this package renders was written by a model or a tool. These tests
// are the ones that would have caught this codebase's escape-blind-truncate
// incident, and they are written the way internal/sanitize's own are: by
// feeding the renderer the bytes an attacker or a confused model would send and
// asserting on what reaches a cell.

// TestControlBytesAreNeutralized walks the sequences that make a terminal DO
// something rather than print something. None of them may survive into a row,
// and none of them may move a column.
func TestControlBytesAreNeutralized(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"osc 52 clipboard", "before \x1b]52;c;aGVsbG8=\x07 after"},
		{"osc 0 title", "before \x1b]0;pwned\x1b\\ after"},
		{"osc 8 hyperlink", "\x1b]8;;https://evil.example\x07click\x1b]8;;\x07"},
		{"cursor movement", "before \x1b[2J\x1b[H\x1b[10A after"},
		{"raw sgr", "before \x1b[1;31mred\x1b[0m after"},
		{"dcs string", "before \x1bPq#0;2;0;0;0\x1b\\ after"},
		{"apc string", "before \x1b_Gf=100\x1b\\ after"},
		{"c1 bytes", "before \x9b31m \x9d0;x\x9c after"},
		{"bare c0", "before \x00\x01\x07\x08\x0b\x0c\x7f after"},
		{"lone escape", "before \x1b"},
		{"escape in a fence", "```\n\x1b[31mred\x1b[0m\n```"},
		{"escape in a table", "| a | b |\n| - | - |\n| \x1b]52;c;x\x07 | \x1b[7mv\x1b[0m |"},
		{"escape in a link", "[label\x1b[31m](https://x/\x1b]0;t\x07)"},
		{"invalid utf-8", "before \xff\xfe after"},
	}
	for _, c := range cases {
		for _, p := range profiles {
			rows := Render(c.src, Options{Width: 40, Styler: styler(p)})
			for i, row := range rows {
				flat := ansi.Strip(row)
				if strings.ContainsRune(flat, 0x1b) {
					t.Errorf("%s at %s: row %d still carries ESC: %q", c.name, p, i, row)
				}
				for _, r := range flat {
					if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
						t.Errorf("%s at %s: row %d carries control %U: %q", c.name, p, i, r, flat)
					}
				}
				if w := ansi.StringWidth(row); w > 40 {
					t.Errorf("%s at %s: row %d is %d cells", c.name, p, i, w)
				}
				if !utf8.ValidString(row) {
					t.Errorf("%s at %s: row %d is not valid UTF-8: %q", c.name, p, i, row)
				}
			}
			// At NoColor there is no legitimate escape at all, so the whole row
			// is testable without stripping.
			if p == tokens.NoColor {
				for i, row := range rows {
					if strings.ContainsRune(row, 0x1b) {
						t.Errorf("%s at NoColor: row %d emitted an escape: %q", c.name, i, row)
					}
				}
			}
		}
	}
}

// TestModelSGRCannotPaintTheSurface is the reason sanitizing alone is not
// enough here. A reply carrying its own colours must not get to paint a
// heading: this package resolves every colour from the markdown structure, so
// somebody else's SGR is stripped rather than honoured.
func TestModelSGRCannotPaintTheSurface(t *testing.T) {
	src := "\x1b[41;97;5mLOOK AT ME\x1b[0m and ordinary prose"
	rows := render(t, src, Options{Width: 60, Styler: styler(tokens.TrueColor)})
	all := strings.Join(rows, "\n")
	for _, banned := range []string{"\x1b[41m", "\x1b[97m", "\x1b[5m", "\x1b[41;97;5m"} {
		if strings.Contains(all, banned) {
			t.Errorf("a model's %q reached the frame: %q", banned, all)
		}
	}
	if !strings.Contains(ansi.Strip(all), "LOOK AT ME and ordinary prose") {
		t.Errorf("the words were lost with the colours: %q", ansi.Strip(all))
	}
}

// pathological is one document holding every shape that has ever broken a
// terminal renderer: deep nesting, an unclosed fence, a 200-column table, a
// word longer than any measure, wide runes, and markdown that is simply wrong.
var pathological = "" +
	"#### \x1b[31mheading with an escape\x1b[0m and a `code span`\n" +
	"\n" +
	"- level one with a very long line that will certainly need to wrap at any measure at all\n" +
	"  - level two\n" +
	"    - level three\n" +
	"      1. level four ordered\n" +
	"         2. level five ordered\n" +
	"            - level six\n" +
	"              > a quote inside a list inside a quote\n" +
	"              > | a | b | c |\n" +
	"              > | - | - | - |\n" +
	"              > | 1 | 2 | 3 |\n" +
	"\n" +
	"> > > triple nested quote with `code` and **bold** and a " +
	"https://example.com/a/very/long/url/that/will/not/fit/anywhere/at/all\n" +
	"\n" +
	"| " + strings.Repeat("wide column header | ", 12) + "\n" +
	"| " + strings.Repeat("--- | ", 12) + "\n" +
	"| " + strings.Repeat("値が広い日本語のセル | ", 12) + "\n" +
	"\n" +
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
	"\n" +
	"日本語の段落が続きます。これは折り返しの計算が二セル幅の文字でも正しく働くことを確かめるための文です。\n" +
	"\n" +
	"***\n" +
	"\n" +
	"| ragged |\n" +
	"| - | - | - |\n" +
	"| a | b |\n" +
	"\n" +
	"```go\n" +
	"func unclosed() {\n" +
	"\tfmt.Println(\"the fence below is never closed\")\n" +
	"\n" +
	"<div class=\"raw\">html block</div>\n" +
	"\n" +
	"a paragraph after an unclosed fence, which is inside it\n"

// TestPathologicalDocumentHolds is the one that has to keep passing. It renders
// the worst document anyone has managed to write at every width from 20 to 140
// and at every profile, and asserts the three properties a caller depends on:
// no panic, no row wider than the width, no newline inside a row.
func TestPathologicalDocumentHolds(t *testing.T) {
	for _, p := range profiles {
		for width := 20; width <= 140; width++ {
			rows := Render(pathological, Options{Width: width, Styler: styler(p)})
			if len(rows) == 0 {
				t.Fatalf("%s at width %d: rendered nothing at all", p, width)
			}
			for i, row := range rows {
				if got := ansi.StringWidth(row); got > width {
					t.Fatalf("%s at width %d: row %d is %d cells: %q", p, width, i, got, row)
				}
				if strings.ContainsAny(row, "\n\r") {
					t.Fatalf("%s at width %d: row %d contains a line break: %q", p, width, i, row)
				}
				if !utf8.ValidString(row) {
					t.Fatalf("%s at width %d: row %d is not valid UTF-8", p, width, i)
				}
			}
		}
	}
}

// TestWidthSweep is the general form of the same promise over the demo document
// — the one that holds every shape in a well-formed arrangement — plus the
// degenerate widths a resize can pass through on its way somewhere.
func TestWidthSweep(t *testing.T) {
	for _, p := range profiles {
		for width := 20; width <= 140; width++ {
			for i, row := range Demo(width, p) {
				if got := ansi.StringWidth(row); got > width {
					t.Fatalf("%s at width %d: row %d is %d cells: %q", p, width, i, got, row)
				}
			}
		}
	}
	for _, width := range []int{-10, 0, 1, 2, 3, 4, 5} {
		for _, p := range profiles {
			for i, row := range Demo(width, p) {
				if got := ansi.StringWidth(row); got > max(width, 1) {
					t.Errorf("%s at width %d: row %d is %d cells: %q", p, width, i, got, row)
				}
			}
		}
	}
}

// TestWideRunesNeverStraddleTheEdge: half a CJK glyph is not a cell, and a
// truncation that produced one would put the next row one column out for the
// rest of the frame.
func TestWideRunesNeverStraddleTheEdge(t *testing.T) {
	src := "| 見出し | 説明 |\n| --- | --- |\n| 値 | 日本語の長い説明文がここに入ります |\n"
	for width := 6; width <= 40; width++ {
		for _, row := range render(t, src, Options{Width: width}) {
			if got := ansi.StringWidth(row); got > width {
				t.Fatalf("width %d: row is %d cells: %q", width, got, row)
			}
		}
	}
}

// TestScrubIsTotal exercises the leaf-level cleaner directly, because it is the
// last thing standing between a control byte and a cell.
func TestScrubIsTotal(t *testing.T) {
	for _, in := range []string{
		"plain", "tab\there", "nl\nhere", "cr\rhere", "\x00\x01\x02", "\x7f", "ok\x1b[31m",
	} {
		got := scrub(in)
		for _, r := range got {
			if r < 0x20 || r == 0x7f {
				t.Errorf("scrub(%q) left %U", in, r)
			}
		}
	}
	if got := scrub("a\tb"); got != "a b" {
		t.Errorf("scrub tab: got %q, want %q", got, "a b")
	}
	if got := scrub("clean"); got != "clean" {
		t.Errorf("scrub allocated for clean input: %q", got)
	}
}

// TestConcurrentRenders backs the claim [markdown] makes about itself. A TUI
// renders from one goroutine today; a surface that wanted to pre-render a
// transcript off the frame loop should not discover a shared parser the hard
// way. Run under -race, this is the whole assertion.
func TestConcurrentRenders(t *testing.T) {
	want := Render(DemoSource, Options{Width: 80, Styler: styler(tokens.TrueColor)})
	done := make(chan []string, 8)
	for i := 0; i < 8; i++ {
		go func() {
			done <- Render(DemoSource, Options{Width: 80, Styler: styler(tokens.TrueColor)})
		}()
	}
	for i := 0; i < 8; i++ {
		got := <-done
		if len(got) != len(want) {
			t.Errorf("concurrent render %d produced %d rows, want %d", i, len(got), len(want))
			continue
		}
		for j := range got {
			if got[j] != want[j] {
				t.Errorf("concurrent render %d differs at row %d", i, j)
				break
			}
		}
	}
}
