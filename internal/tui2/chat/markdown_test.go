package chat

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// The markdown renderer is tested for two things and no third: that the reader
// sees WORDS rather than delimiters, and that no row it produces can break the
// layout it is handed. Everything else about it is a judgement call about which
// subset a model actually emits, and a test that pinned those choices would
// just be the implementation typed twice.

// plainProse renders at the NoColor profile, where every assertion below is
// about the characters a reader sees and nothing else.
func plainProse() prose {
	return prose{style: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal), base: tokens.TextPrimary}
}

func renderProse(t *testing.T, text string, width int) []string {
	t.Helper()
	return plainProse().rows(nil, text, width, 0)
}

// 13.1 item 1 opened on exactly this: bold arriving on screen as literal
// asterisks. A paired delimiter must never survive; an unpaired one must, since
// removing it would be editing what the author wrote.
func TestPairedEmphasisNeverReachesTheScreen(t *testing.T) {
	cases := []struct {
		name, in, want string
		gone           string
	}{
		{"bold", "make it **fast** now", "make it fast now", "**"},
		{"underscore bold", "make it __fast__ now", "make it fast now", "__"},
		{"italic", "make it *fast* now", "make it fast now", "*"},
		{"code", "run `go test` twice", "run go test twice", "`"},
		{"bold inside a bullet", "- **read** it", "• read it", "**"},
		{"bold inside a heading", "## **big**", "big", "**"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := strings.Join(renderProse(t, testCase.in, 60), "\n")
			if !strings.Contains(got, testCase.want) {
				t.Fatalf("rendered %q, want it to contain %q", got, testCase.want)
			}
			if strings.Contains(got, testCase.gone) {
				t.Fatalf("the %q delimiter survived to the screen: %q", testCase.gone, got)
			}
		})
	}
}

func TestUnpairedDelimitersAreLeftAlone(t *testing.T) {
	for _, text := range []string{"2 * 3 * is not emphasis", "a lone ** here", "snake_case_word"} {
		got := strings.Join(renderProse(t, text, 60), "\n")
		if got != text {
			t.Fatalf("unpaired delimiters were edited: %q became %q", text, got)
		}
	}
}

func TestCodeFencesAreNotReflowed(t *testing.T) {
	const source = "```go\nfunc main() { println(\"hi\") }\n```"
	rows := renderProse(t, source, 20)
	for _, row := range rows {
		if strings.Contains(row, "func main") && strings.Contains(row, "println") {
			t.Fatalf("the fenced line survived whole at width 20, so it was not cut: %q", row)
		}
	}
	// A wrapped code line would have produced a row starting with the tail of
	// the statement. Cutting produces exactly one row per source line.
	if len(rows) != 3 {
		t.Fatalf("a three-line fence rendered as %d rows: %q", len(rows), rows)
	}
}

func TestListsAndQuotesCarryTheirGutters(t *testing.T) {
	rows := renderProse(t, "- one\n- two\n\n1. first\n\n> quoted", 40)
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"• one", "• two", "1. first", "│ quoted"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

// A hanging indent is the whole point of a bullet: the second row of an item
// must line up under the first row's text, not under its glyph.
func TestBulletContinuationHangs(t *testing.T) {
	rows := renderProse(t, "- alpha beta gamma delta epsilon", 14)
	if len(rows) < 2 {
		t.Fatalf("the bullet did not wrap at width 14: %q", rows)
	}
	if !strings.HasPrefix(rows[0], "• ") {
		t.Fatalf("first row lost its bullet: %q", rows[0])
	}
	if !strings.HasPrefix(rows[1], "  ") {
		t.Fatalf("the continuation did not hang under the text: %q", rows[1])
	}
}

// A word longer than a row is information — a path, a URL, a hash — so it is
// broken where the row ends rather than ellipsized away.
func TestAWordLongerThanTheRowIsBrokenNotDropped(t *testing.T) {
	const word = "abcdefghijklmnopqrstuvwxyz0123456789"
	rows := renderProse(t, word, 10)
	if got := strings.Join(rows, ""); got != word {
		t.Fatalf("the long word lost characters: %q", got)
	}
}

// The layout contract: no row wider than the width it was given, at any width,
// through every branch of the renderer, in both contrast tiers.
func TestProseNeverOverrunsItsWidth(t *testing.T) {
	const source = "# A heading with **bold**\n\n" +
		"Ordinary prose with `code`, *emphasis* and a very-long-unbreakable-token-here.\n\n" +
		"- a bullet that runs on and on and on\n" +
		"1. an ordered item\n\n" +
		"> a quotation that also runs on\n\n" +
		"```sh\n  indented code --with --flags\n```\n"
	for _, profile := range []tokens.Profile{tokens.NoColor, tokens.TrueColor} {
		renderer := prose{style: tokens.NewStyler(profile, tokens.FocusNormal), base: tokens.TextPrimary}
		for width := 1; width <= 60; width++ {
			for _, indent := range []int{0, bodyIndent} {
				for _, row := range renderer.rows(nil, source, width, indent) {
					if strings.ContainsAny(row, "\n\r") {
						t.Fatalf("a rendered row carries a newline at width %d: %q", width, row)
					}
					if got := blocks.Width(row); got > width {
						t.Fatalf("row is %d cells at width %d: %q", got, width, ansi.Strip(row))
					}
				}
			}
		}
	}
}

// Under NoColor the renderer writes no escape sequence at all: emphasis is the
// one place it would reach past the token layer, and it must not.
func TestNoColorRendersNoEscapes(t *testing.T) {
	rows := renderProse(t, "**bold** and *italic* and `code`", 40)
	for _, row := range rows {
		if strings.ContainsRune(row, 0x1b) {
			t.Fatalf("an escape sequence reached a NoColor row: %q", row)
		}
	}
}

// Weight is 5.13's own axis and the token layer carries colour only, so bold
// and italic are the two SGR codes this package writes itself. They must
// actually appear once colour is available, or the hierarchy is only a comment.
func TestEmphasisCarriesWeightWhenColourIsAvailable(t *testing.T) {
	renderer := prose{style: tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal), base: tokens.TextPrimary}
	row := strings.Join(renderer.rows(nil, "**bold** and *slanted*", 40, 0), "")
	if !strings.Contains(row, sgrBold) {
		t.Fatalf("bold carried no weight: %q", row)
	}
	if !strings.Contains(row, sgrItalic) {
		t.Fatalf("italic carried no slant: %q", row)
	}
}

func TestBlankLinesSurviveAsBlankRows(t *testing.T) {
	rows := renderProse(t, "one\n\ntwo", 40)
	if len(rows) != 3 || rows[1] != "" {
		t.Fatalf("the paragraph break did not survive: %q", rows)
	}
}
