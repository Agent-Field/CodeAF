package tui3

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// These tests are about the ADAPTER, not about prose: internal/tui2/prose has
// its own suite for how a table fits and where a heading stands. What is proved
// here is that this surface reaches that renderer with a real Styler, at the
// right profile, once — and that the rows which come back are rows, because the
// frame this package draws around them measures every one.

func mdStyler(p tokens.Profile) *tokens.Styler {
	return tokens.NewStyler(p, tokens.FocusNormal)
}

// mdSGR is every SGR sequence in s, in order. A colour is deliberately never
// compared to a literal here: which byte a token resolves to belongs to
// internal/tui2/tokens and its contrast tests, and a test in this package that
// pinned one would fail the day the palette was tuned, for no reader's benefit.
func mdSGR(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			i++
			continue
		}
		end := strings.IndexByte(s[i:], 'm')
		if end < 0 {
			break
		}
		out = append(out, s[i:i+end+1])
		i += end + 1
	}
	return out
}

// mdInkAt is the SGR run in force where needle starts: everything opened since
// the last reset, which is what actually paints that word.
func mdInkAt(row, needle string) string {
	at := strings.Index(row, needle)
	if at < 0 {
		return ""
	}
	var ink string
	for i := 0; i < at; {
		if row[i] != 0x1b {
			i++
			continue
		}
		end := strings.IndexByte(row[i:], 'm')
		if end < 0 {
			break
		}
		ink = row[i : i+end+1]
		i += end + 1
	}
	return ink
}

func mdRowWith(rows []string, want string) string {
	for _, r := range rows {
		if strings.Contains(ansi.Strip(r), want) {
			return r
		}
	}
	return ""
}

const mdGoFence = "Here is the loop:\n" +
	"\n" +
	"```go\n" +
	"// count walks the batch.\n" +
	"func count(items []string) int {\n" +
	"\ttotal := 0\n" +
	"\tfor _, s := range items {\n" +
	"\t\ttotal += len(s) + 1\n" +
	"\t}\n" +
	"\treturn total\n" +
	"}\n" +
	"```\n"

// A fenced block is lexed and painted where the profile can carry the ramp, and
// is drawn flat where it cannot — the 5.20 rule that a lying colour is worse
// than no colour. Both halves are asserted, because "it emitted escapes" alone
// would also be true of a block painted one uniform grey.
func TestMarkdownFencedCodeIsChromaHighlighted(t *testing.T) {
	rows := renderMarkdownWith(mdStyler(tokens.TrueColor), mdGoFence, 72)
	body := mdRowWith(rows, "func count(")
	if body == "" {
		t.Fatalf("no row carries the func line:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(body, "\x1b[") {
		t.Fatalf("code row carries no escape at all: %q", body)
	}

	// The lexer ran: `func` is a keyword and `count` is a function name, and the
	// two do not share ink. If chroma had been skipped the whole line would be
	// one run.
	keyword, name := mdInkAt(body, "func"), mdInkAt(body, "count(")
	if keyword == "" || keyword == name {
		t.Fatalf("keyword and name share ink %q — the lexer did not run: %q", keyword, body)
	}

	// And the ramp is a ramp: comment, keyword, string/number and body text are
	// four distinct answers across the block, not one colour applied evenly.
	inks := map[string]bool{}
	for _, r := range rows {
		if strings.Contains(ansi.Strip(r), tokens.GlyphCodeGutter) {
			for _, seq := range mdSGR(r) {
				inks[seq] = true
			}
		}
	}
	if len(inks) < 4 {
		t.Fatalf("only %d distinct sequences in the fence, expected a ramp: %v", len(inks), inks)
	}

	// Below 256 colours the ramp does not exist, so nothing is lexed and the
	// line keeps one voice.
	flat := renderMarkdownWith(mdStyler(tokens.ANSI16), mdGoFence, 72)
	line := mdRowWith(flat, "func count(")
	if line == "" {
		t.Fatalf("no row carries the func line at 16 colours:\n%s", strings.Join(flat, "\n"))
	}
	if k, n := mdInkAt(line, "func"), mdInkAt(line, "count("); k != n {
		t.Fatalf("16 colours highlighted anyway: %q vs %q in %q", k, n, line)
	}

	// The source survives the round trip: highlighting adds escapes, never
	// printable bytes.
	var code []string
	for _, r := range rows {
		if plain := ansi.Strip(r); strings.Contains(plain, tokens.GlyphCodeGutter) {
			code = append(code, strings.TrimRight(strings.TrimPrefix(plain, tokens.GlyphCodeGutter+" "), " "))
		}
	}
	if got := strings.Join(code, "\n"); !strings.Contains(got, "func count(items []string) int {") {
		t.Fatalf("fence lost its source:\n%s", got)
	}
}

// A heading promotes by tier and h1 also takes bold — the whole of "louder" in
// a terminal that has one type size.
func TestMarkdownHeadingCarriesBoldAndAccent(t *testing.T) {
	st := mdStyler(tokens.TrueColor)
	rows := renderMarkdownWith(st, "# What changed\n\nA sentence.\n", 60)
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	head := rows[0]
	if plain := ansi.Strip(head); plain != "What changed" {
		t.Fatalf("h1 row is %q, want the bare title", plain)
	}
	if !strings.Contains(head, "\x1b[1m") {
		t.Fatalf("h1 is not bold: %q", head)
	}
	accent := tokens.TextPrimary.Fg(tokens.TrueColor, tokens.FocusNormal)
	if accent == "" || !strings.Contains(head, accent) {
		t.Fatalf("h1 does not carry the promoted tier %q: %q", accent, head)
	}

	// The body below it is the same tier without the weight, so bold is the
	// heading's own signal and not the document's.
	body := mdRowWith(rows, "A sentence.")
	if strings.Contains(body, "\x1b[1m") {
		t.Fatalf("body text came back bold: %q", body)
	}

	// A deeper heading steps DOWN the ramp, so hierarchy exists at all.
	deep := renderMarkdownWith(st, "#### Detail\n", 60)
	if len(deep) == 0 {
		t.Fatal("no rows for h4")
	}
	if strings.Contains(deep[0], "\x1b[1m") {
		t.Fatalf("h4 took bold: %q", deep[0])
	}
	if strings.Contains(deep[0], accent) {
		t.Fatalf("h4 stands on the same tier as h1 instead of stepping down: %q", deep[0])
	}
}

const mdTable = "| task | owner | cost |\n" +
	"| --- | --- | ---: |\n" +
	"| alpha | ada | 12 |\n" +
	"| beta | lin | 340 |\n" +
	"| gamma | wu | 7 |\n"

// The failure this whole seam exists to end: "tables wrap to soup". A record is
// one row at every width, including widths far too narrow for it.
func TestMarkdownTableKeepsOneRowPerRecord(t *testing.T) {
	for _, width := range []int{80, 40, 24, 12} {
		rows := renderMarkdownWith(mdStyler(tokens.TrueColor), mdTable, width)
		for _, record := range []string{"alpha", "beta", "gamma"} {
			seen := 0
			for _, r := range rows {
				if strings.Contains(ansi.Strip(r), record) {
					seen++
				}
			}
			// At the narrowest widths a cell is truncated away entirely, which is
			// the designed answer; what must never happen is a record spread over
			// two rows.
			if seen > 1 {
				t.Fatalf("width %d: %q appears on %d rows:\n%s", width, record, seen, strings.Join(rows, "\n"))
			}
		}
		if width >= 24 {
			if mdRowWith(rows, "alpha") == "" {
				t.Fatalf("width %d: the alpha record vanished:\n%s", width, strings.Join(rows, "\n"))
			}
		}
	}
}

// Emphasis becomes weight, and the punctuation that asked for it does not reach
// a cell — the reader sees the answer, not the model's typing.
func TestMarkdownEmphasisIsWeightNotPunctuation(t *testing.T) {
	rows := renderMarkdownWith(mdStyler(tokens.TrueColor), "A **loud** and *quiet* word with `code`.\n", 60)
	if len(rows) != 1 {
		t.Fatalf("want one row, got %d: %q", len(rows), rows)
	}
	row := rows[0]
	if plain := ansi.Strip(row); plain != "A loud and quiet word with code." {
		t.Fatalf("markers leaked into the cells: %q", plain)
	}
	if !strings.Contains(row, "\x1b[1m") {
		t.Fatalf("bold lost its weight: %q", row)
	}
	if !strings.Contains(row, "\x1b[3m") {
		t.Fatalf("italic lost its slant: %q", row)
	}
	// An inline code span is set apart by its ground, not by its punctuation:
	// the raised plane where the profile has one.
	if sheet := tokens.Sheet.Bg(tokens.TrueColor, tokens.FocusNormal); !strings.Contains(row, sheet) {
		t.Fatalf("the code span is not raised onto the sheet: %q", row)
	}
}

// Unpaired delimiters are text. A model writing "2 * 3" or an unclosed fence
// must not have its arithmetic silently eaten, and an unterminated emphasis run
// must not swallow the rest of the reply.
func TestMarkdownUnpairedDelimitersRenderLiterally(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"2 * 3 = 6\n", "2 * 3 = 6"},
		{"**unclosed bold keeps its stars\n", "**unclosed bold keeps its stars"},
		{"a_b_c and snake_case_name\n", "snake_case_name"},
		{"an `unclosed span\n", "an `unclosed span"},
		{"5 < 6 > 4 & true\n", "5 < 6 > 4 & true"},
	} {
		rows := renderMarkdownWith(mdStyler(tokens.TrueColor), tc.src, 60)
		got := strings.Join(mdStripAll(rows), "\n")
		if !strings.Contains(got, tc.want) {
			t.Fatalf("src %q rendered %q, want it to contain %q", tc.src, got, tc.want)
		}
	}

	// An unclosed fence is still a fence: its content is drawn as code rather
	// than dropped with the block that never ended.
	open := renderMarkdownWith(mdStyler(tokens.TrueColor), "```go\nfunc main() {\n", 60)
	if mdRowWith(open, "func main() {") == "" {
		t.Fatalf("an unclosed fence lost its body:\n%s", strings.Join(open, "\n"))
	}
}

func mdStripAll(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

// The 20 documents are the shapes a model actually sends, plus the ones it
// sends by accident: CRLF from a pasted file, a fence it forgot to close, a URL
// longer than any terminal, and an escape sequence it copied out of a log.
var mdCorpus = []string{
	prose.DemoSource,
	"# Title\n\nA paragraph that is comfortably longer than eighty-eight cells so that the wrapper has to make at least one decision about where a line ends.\n",
	"- one\n- two\n  - two point one\n    - two point one point one\n      - and deeper still, with a sentence long enough to wrap under its own first word\n- three\n",
	"1. first\n2. second\n...\n9. ninth\n10. tenth, whose marker column is wider than the others\n",
	"See https://example.com/a/very/long/path/that/keeps/going/and/going?query=1&other=2#fragment-name-that-is-also-long for details.\n",
	"```go\nfunc main() {\n\tprintln(\"never closed\")\n",
	"Line one\r\nLine two\r\n\r\n| a | b |\r\n| --- | --- |\r\n| 1 | 2 |\r\n",
	"| a very wide column indeed | another very wide column | and a third one too |\n| --- | --- | --- |\n| with long content in it | and more long content | and yet more content |\n",
	"# h1\n## h2\n### h3\n#### h4\n##### h5\n###### h6\n",
	"> quoted\n> > nested quote\n> > > and deeper\n",
	"Mixed `inline code`, **bold**, *italic*, ~~struck~~ and [a link](https://example.com/x).\n",
	"日本語のテキストと絵文字 🎉🚀 が混ざった段落で、幅の計算が正しいかどうかを確かめます。\n",
	"\x1b[31mred text a model copied out of a log\x1b[0m\n\x1b]52;c;cGF5bG9hZA==\x07\n",
	"| 名前 | 値 |\n| --- | --- |\n| 幅の広い文字 | 🎉 |\n",
	"---\n\n***\n\n___\n\ntext between rules\n",
	"<div class=\"raw\">\n  <span>html a model sent</span>\n</div>\n",
	strings.Repeat("Supercalifragilisticexpialidocious", 12) + "\n",
	"\tan indented code block\n\twith a second line\n",
	"- [ ] unchecked task\n- [x] checked task, with a trailing sentence that will need to wrap somewhere sensible\n",
	"Paragraph one.\n\n\n\n\nParagraph two after a run of blank lines.\n\n\t\t\n",
}

// The contract, on every document at every width: a row is one row, and it fits.
// This is the one invariant the frame in view.go depends on absolutely — an
// over-wide row does not look wrong, it pushes every row below it sideways.
func TestMarkdownRowsAlwaysFitTheirWidth(t *testing.T) {
	profiles := []tokens.Profile{tokens.NoColor, tokens.ANSI16, tokens.ANSI256, tokens.TrueColor}
	widths := []int{1, 2, 3, 7, 13, 24, 40, 72, 120, 200}
	for i, doc := range mdCorpus {
		for _, p := range profiles {
			for _, w := range widths {
				for _, row := range renderMarkdownWith(mdStyler(p), doc, w) {
					if strings.ContainsAny(row, "\n\r") {
						t.Fatalf("doc %d profile %v width %d: row contains a newline: %q", i, p, w, row)
					}
					if got := ansi.StringWidth(row); got > w {
						t.Fatalf("doc %d profile %v width %d: row is %d cells: %q", i, p, w, got, row)
					}
					if strings.ContainsRune(ansi.Strip(row), '\x00') {
						t.Fatalf("doc %d profile %v width %d: control byte reached a cell: %q", i, p, w, row)
					}
				}
			}
		}
	}
}

// A width the caller got wrong must not panic or produce a negative-width row:
// the surface is resized by a human dragging a corner.
func TestMarkdownAbsurdWidthsAreClamped(t *testing.T) {
	for _, w := range []int{-100, -1, 0} {
		for _, row := range renderMarkdown("# Title\n\nsome **text** here\n", w) {
			if ansi.StringWidth(row) > 1 {
				t.Fatalf("width %d produced %q", w, row)
			}
		}
	}
}

// A reply that said nothing must not push the transcript down by a row.
func TestMarkdownEmptySourceDrawsNothing(t *testing.T) {
	for _, src := range []string{"", "   ", "\n\n\n", "\r\n"} {
		if rows := renderMarkdown(src, 60); len(rows) != 0 {
			t.Fatalf("src %q drew %d rows: %q", src, len(rows), rows)
		}
	}
}

// The Styler is a value worth holding, not a result worth recomputing: this
// runs a few times a second while a reply streams.
func TestMarkdownStylerIsBuiltOnceForTheTerminal(t *testing.T) {
	first := markdownStyler()
	if first == nil {
		t.Fatal("no styler")
	}
	if second := markdownStyler(); second != first {
		t.Fatalf("styler rebuilt: %p then %p", first, second)
	}
	if want := tokens.DetectProfile(os.Getenv); first.Profile() != want {
		t.Fatalf("styler profile is %v, want the detected %v", first.Profile(), want)
	}
	if first.Focus() != tokens.FocusNormal {
		t.Fatalf("styler focus is %v, want normal", first.Focus())
	}
}

// Under NoColor — a pipe, TERM=dumb, NO_COLOR — the rows are their own bytes.
// Every distinction colour carried survives as shape, which is what makes that
// profile a degradation rather than a failure.
func TestMarkdownNoColorDrawsNoEscapes(t *testing.T) {
	for i, doc := range mdCorpus {
		for _, row := range renderMarkdownWith(mdStyler(tokens.NoColor), doc, 72) {
			if strings.ContainsRune(row, 0x1b) {
				t.Fatalf("doc %d painted under NoColor: %q", i, row)
			}
		}
	}
	rows := renderMarkdownWith(mdStyler(tokens.NoColor), "# Title\n\n- item\n", 40)
	if mdRowWith(rows, "Title") == "" || mdRowWith(rows, "item") == "" {
		t.Fatalf("NoColor lost the content:\n%s", strings.Join(rows, "\n"))
	}
}
