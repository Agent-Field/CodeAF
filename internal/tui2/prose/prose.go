package prose

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"

	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// DefaultMeasure is the reading length prose wraps to when a caller states no
// other. It is the low end of the classic typographic measure — 45 to 90
// characters a line, past which the eye loses the return sweep — and it is a
// CEILING rather than a target: a 60-column terminal wraps at 60.
//
// The number is here rather than in tokens because it is a property of reading,
// not of this palette: tokens' breakpoints describe how many panes fit on a
// screen, and this describes how long a sentence may be inside one.
const DefaultMeasure = 88

// Options is everything a render depends on. It is a value, and [Render] is a
// pure function of it and the source: the same source at the same width through
// the same Styler produces the same bytes, every time, which is what lets a
// caller cache rows per (width, version) the way the block engine already does.
type Options struct {
	// Width is the hard ceiling in printable cells. EVERY row [Render] returns
	// is at most this wide — not usually, not for well-formed input, always.
	// A Width below 1 is clamped to 1.
	Width int
	// Measure is the reading length paragraphs, headings, lists, quotes and
	// rules wrap to, clamped to Width. Zero means [DefaultMeasure]; a caller
	// that genuinely wants prose to fill the pane sets Measure equal to Width.
	//
	// Tables and code blocks ignore it and take the full Width, because a
	// figure is looked at rather than read along: narrowing a table to a
	// sentence's length is how a table becomes soup.
	//
	// AND SO DOES A TOKEN THAT CANNOT BE BROKEN — a URL, a path, a hash. It is
	// given the whole Width before it is cut, for the same reason and on the same
	// terms ([wrapper.commit]): a link is copied rather than read along, and a
	// link broken at the measure while the column stood half empty was a link
	// that did not work.
	Measure int
	// Styler paints every cell. Nil renders unpainted text — the honest
	// headless default, and exactly what a NoColor profile would draw anyway.
	Styler *tokens.Styler
	// PlainCodeSpan reports whether an inline code span already has a visible
	// mark supplied by the caller and must therefore stay off the raised plane.
	// Nil means every inline code span uses prose's ordinary plane.
	PlainCodeSpan func(string) bool
}

func (o Options) normalized() Options {
	if o.Width < 1 {
		o.Width = 1
	}
	if o.Measure <= 0 {
		o.Measure = DefaultMeasure
	}
	if o.Measure > o.Width {
		o.Measure = o.Width
	}
	return o
}

// markdown is the parser, built once. It is goldmark's PARSER only — no
// renderer is ever attached, because attaching one would be handing a second
// colour authority a seat on this surface (see the package comment).
//
// GFM is on for its tables and strikethrough; a model writes GFM whether or not
// the flavour was agreed on. Autolinks are on for the same reason. It is
// concurrency-safe: goldmark.Markdown holds no per-parse state, and every parse
// gets its own reader.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

// sanitizeChokepoint is the same table the chat engine, the rail and the homes
// line plug in: model- and tool-authored text is cleaned in one place, with the
// token layer's ANSI-16 remap (12.4.4).
//
// Prose then goes one step further than any of them and strips what survives.
// The remap exists so someone else's colours COHERE with ours; here there is
// nothing for them to cohere with, because this package resolves every colour
// on the row itself from the markdown structure. A reply that arrived carrying
// its own SGR would otherwise get to paint a heading, and the surface's own
// hierarchy would be one model's whim away from meaning nothing.
var sanitizeChokepoint = sanitize.Table(tokens.ANSI16Remap)

// sanitizeSource is the door every byte enters through. Sanitizing first is not
// interchangeable with stripping: sanitize NEUTRALIZES the sequences that make
// a terminal do something (OSC 52 to the clipboard, DCS, the C1 range) and is
// the audited place that judgement lives, while [ansi.Strip] merely removes
// escapes it recognizes. The strip is the second, narrower step.
func sanitizeSource(s string) string {
	return ansi.Strip(sanitize.TextWithPalette(s, sanitizeChokepoint))
}

// Render turns markdown into styled terminal rows.
//
// Each returned string is one screen row: no newlines, and never more than
// [Options.Width] printable cells. An empty source returns no rows, not one
// empty row — a reply that said nothing must not push the transcript down.
func Render(src string, opts Options) []string {
	return RenderTo(nil, src, opts)
}

// RenderTo is [Render] appending into a caller's slice, for a surface that
// re-renders on every resize and would rather keep its buffer.
func RenderTo(dst []string, src string, opts Options) []string {
	opts = opts.normalized()
	clean := sanitizeSource(src)
	if strings.TrimSpace(clean) == "" {
		return dst
	}
	source := []byte(clean)
	doc := markdown.Parser().Parse(text.NewReader(source))

	r := &renderer{src: source, opts: opts, p: newPainter(opts.Styler), out: dst, base: body}
	r.container(doc, true)
	return r.trimTrailingBlanks()
}
