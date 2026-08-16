package prose

import "github.com/Agent-Field/aforge-v2/internal/tui2/tokens"

// DemoSource exercises every shape this package knows how to draw, in the order
// a reviewer would want to see them. It is a const so a test can render it at
// forty widths without a fixture file, and it is deliberately a document a model
// might plausibly have written rather than a syntax showcase.
const DemoSource = "# Rendering model prose\n" +
	"\n" +
	"A reply arrives as **markdown** whether or not anyone asked for it, so the\n" +
	"surface has to *read* it. Inline `code spans` sit on a raised ground, and a\n" +
	"link like [the audit](audit-notes/chat-rebuild.md) keeps its address.\n" +
	"\n" +
	"## What changed\n" +
	"\n" +
	"1. Paragraphs wrap to the measure, not to the terminal.\n" +
	"2. Tables truncate instead of wrapping.\n" +
	"3. Fenced code is highlighted where the profile can carry it.\n" +
	"\n" +
	"- unordered items hang under their own first word, which is the whole point\n" +
	"  of a hanging indent\n" +
	"- nested lists indent once more\n" +
	"  - like this\n" +
	"\n" +
	"> A blockquote recedes one tier and grows a gutter bar.\n" +
	"> It is structure without a box.\n" +
	"\n" +
	"| column | meaning | width |\n" +
	"| --- | --- | ---: |\n" +
	"| measure | how long a sentence may be | 88 |\n" +
	"| width | the hard ceiling, always obeyed | 120 |\n" +
	"\n" +
	"```go\n" +
	"// wrap greedily, because greedy is the only rule under which\n" +
	"// appending text cannot rewrite a row that is already on screen.\n" +
	"func Wrap(dst []string, text string, width int) ([]string, int) {\n" +
	"\tif width < 1 {\n" +
	"\t\twidth = 1\n" +
	"\t}\n" +
	"\treturn dst, 0\n" +
	"}\n" +
	"```\n" +
	"\n" +
	"---\n" +
	"\n" +
	"That is the whole vocabulary.\n"

// Demo renders [DemoSource] at a width and a profile. It is the fastest way to
// look at this package's output — from a test, from a scratch main, or from a
// settings sheet's live preview when one exists — without a caller having to
// build a Styler and remember which focus a preview is drawn at.
func Demo(width int, p tokens.Profile) []string {
	return Render(DemoSource, Options{
		Width:  width,
		Styler: tokens.NewStyler(p, tokens.FocusNormal),
	})
}
