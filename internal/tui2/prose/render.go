package prose

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The AST walk.
//
// One method per block kind, and every one of them ends by handing rows to
// [renderer.emit]. That is the whole discipline: there is exactly one function
// in this package that can put a string in the output, so the width ceiling,
// the gutter prefix and the painter are applied once each rather than once per
// block kind — which is how a table and a list item come out at the same right
// edge without either of them knowing about the other.

// prefix is the gutter a container hangs on every row inside it: a blockquote's
// bar, a list item's marker and its hanging indent.
//
// first and rest are the same WIDTH by construction — a marker is padded to the
// indent it opens — so the content width inside a container is one number and
// not two. pending is the first-row flag, and it survives across nesting: a
// list item whose first child is a nested list must still draw its own marker
// on that nested item's first row.
type prefix struct {
	first, rest []piece
	pending     bool
	width       int
}

type renderer struct {
	src  []byte
	opts Options
	p    painter
	out  []string

	pfx  prefix
	base style
}

// emit is the one door to the output. It applies the container gutter, trims
// the trailing whitespace no terminal should be asked to draw, and enforces the
// width ceiling on plain text before a single escape byte exists.
func (r *renderer) emit(row []piece) { r.emitRow(row, false) }

// emitRaw is [renderer.emit] for rows whose trailing spaces are ink: a code
// line's padding is what draws the raised ground out to the block's right edge,
// and trimming it would leave the block with a ragged plane.
func (r *renderer) emitRaw(row []piece) { r.emitRow(row, true) }

func (r *renderer) emitRow(row []piece, keepTrailing bool) {
	lead := r.pfx.rest
	if r.pfx.pending {
		lead = r.pfx.first
		r.pfx.pending = false
	}
	full := row
	if len(lead) > 0 {
		full = make([]piece, 0, len(lead)+len(row))
		full = append(full, lead...)
		full = append(full, row...)
	}
	if !keepTrailing {
		full = trimTrailing(full)
	}
	r.out = append(r.out, r.p.assemble(full, r.opts.Width))
}

// gap is the blank row between two blocks. Inside a container it is not blank:
// it carries the gutter, so a blockquote's bar is continuous down the side of
// the paragraphs it holds rather than dashed between them.
func (r *renderer) gap() { r.emit(nil) }

// trimTrailing drops the trailing whitespace from a row. Pieces on a raised
// ground are exempt: their spaces are the ground.
func trimTrailing(row []piece) []piece {
	for len(row) > 0 {
		last := row[len(row)-1]
		if last.st.ground {
			break
		}
		trimmed := strings.TrimRight(last.text, " ")
		if trimmed == last.text {
			break
		}
		if trimmed == "" {
			row = row[:len(row)-1]
			continue
		}
		row = append(row[:len(row)-1:len(row)-1], piece{text: trimmed, st: last.st})
		break
	}
	return row
}

// trimTrailingBlanks removes the blank rows a document's last gap left behind.
// A reply must not hand the transcript a run of empty rows to scroll past.
func (r *renderer) trimTrailingBlanks() []string {
	for len(r.out) > 0 && strings.TrimSpace(r.out[len(r.out)-1]) == "" {
		r.out = r.out[:len(r.out)-1]
	}
	return r.out
}

// proseWidth is the reading width inside the current gutter.
func (r *renderer) proseWidth() int { return atLeastOne(r.opts.Measure - r.pfx.width) }

// figureWidth is the full width inside the current gutter — what a table or a
// code block lays out in.
func (r *renderer) figureWidth() int { return atLeastOne(r.opts.Width - r.pfx.width) }

func atLeastOne(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// push opens a container gutter and returns the state to restore. The parent's
// pending first-row prefix is folded into the child's, so the marker of an
// outer list and the bar of an inner quote land on the same row when that is
// what the source said.
func (r *renderer) push(first, rest []piece) prefix {
	saved := r.pfx
	base := saved.rest
	if saved.pending {
		base = saved.first
	}
	r.pfx = prefix{
		first:   concat(base, first),
		rest:    concat(saved.rest, rest),
		pending: true,
		width:   saved.width + piecesWidth(rest),
	}
	return saved
}

// pop closes a container. If the container drew nothing, the parent's pending
// first prefix is still pending — an empty list item must not eat the marker of
// the row after it.
func (r *renderer) pop(saved prefix, drew bool) {
	if drew {
		saved.pending = false
	}
	r.pfx = saved
}

func concat(a, b []piece) []piece {
	switch {
	case len(a) == 0:
		return b
	case len(b) == 0:
		return a
	}
	out := make([]piece, 0, len(a)+len(b))
	return append(append(out, a...), b...)
}

func piecesWidth(ps []piece) int {
	n := 0
	for _, pc := range ps {
		n += cells(pc.text)
	}
	return n
}

// container renders a node's children in order, separating them with blank
// rows when gaps is true. A tight list is the one shape that asks for false:
// tightness is the source saying the items belong to one thought.
func (r *renderer) container(n ast.Node, gaps bool) {
	for c, i := n.FirstChild(), 0; c != nil; c, i = c.NextSibling(), i+1 {
		if i > 0 && gaps {
			r.gap()
		}
		r.block(c)
	}
}

func (r *renderer) block(n ast.Node) {
	switch n := n.(type) {
	case *ast.Heading:
		r.heading(n)
	case *ast.Paragraph:
		r.paragraph(n)
	case *ast.TextBlock:
		r.paragraph(n)
	case *ast.Blockquote:
		r.blockquote(n)
	case *ast.List:
		r.list(n)
	case *ast.ListItem:
		// Reached only for a malformed tree; a well-formed one goes through
		// [renderer.list], which owns the marker column.
		r.container(n, false)
	case *ast.FencedCodeBlock:
		r.code(n.Lines(), string(n.Language(r.src)))
	case *ast.CodeBlock:
		r.code(n.Lines(), "")
	case *ast.ThematicBreak:
		r.rule()
	case *ast.HTMLBlock:
		r.htmlBlock(n)
	case *extast.Table:
		r.table(n)
	default:
		// An unknown block is still somebody's words. Rendering its children is
		// strictly better than dropping them, and a leaf with no children
		// contributes nothing, which is the correct amount.
		r.container(n, true)
	}
}

// heading promotes by TIER (5.13). A terminal has one type size, so the whole
// of a heading's loudness is where it stands on the grey ramp, the bold on the
// top level, and the whitespace [renderer.container] already put around it.
// Level 3 and below step back DOWN through [tokens.Demote], because a document
// whose every heading was primary would have no hierarchy at all.
func (r *renderer) heading(n *ast.Heading) {
	st := r.base
	st.tok = tokens.TextPrimary
	for i := 3; i <= n.Level; i++ {
		st.tok = tokens.Demote(st.tok)
	}
	st.bold = n.Level == 1
	r.wrapInline(n, st, r.proseWidth())
}

func (r *renderer) paragraph(n ast.Node) {
	r.wrapInline(n, r.base, r.proseWidth())
}

// wrapInline is the shared tail of every prose block: walk the inline children
// into a wrapper at the given width and emit what it produces.
func (r *renderer) wrapInline(n ast.Node, st style, width int) {
	w := newWrapper(width, r.figureWidth(), r.emit)
	r.inline(n, w, st)
	w.flush()
}

// blockquote is a dim gutter bar and one step down the ramp — structure without
// boxes (5.21). Nesting demotes again, so a quote inside a quote recedes rather
// than growing a second border.
func (r *renderer) blockquote(n *ast.Blockquote) {
	bar := []piece{
		{text: tokens.GlyphProseQuote, st: style{tok: tokens.TextTertiary}},
		{text: " ", st: body},
	}
	saved := r.push(bar, bar)
	savedBase := r.base
	r.base.tok = tokens.Demote(r.base.tok)
	if r.base.tok == tokens.TextPrimary {
		r.base.tok = tokens.TextSecondary
	}
	before := len(r.out)
	r.container(n, true)
	r.base = savedBase
	r.pop(saved, len(r.out) > before)
}

// list draws the marker column and the hanging indent. The marker is padded to
// the column's width so continuation rows align under the item's first WORD and
// never under its bullet: a list whose wrapped lines slid back to the margin is
// a list that stops looking like one at the second line.
//
// An ordered list sizes its column to its widest number, so 9. and 10. share a
// right edge and the numbers read as a column of numbers.
func (r *renderer) list(n *ast.List) {
	gutter := 2
	if n.IsOrdered() {
		last := n.Start + n.ChildCount() - 1
		gutter = len(strconv.Itoa(atLeastOne(last))) + 2
	}
	indent := []piece{{text: spaces(gutter), st: body}}
	number := n.Start

	for c, i := n.FirstChild(), 0; c != nil; c, i = c.NextSibling(), i+1 {
		if i > 0 && !n.IsTight {
			r.gap()
		}
		mark := pad(tokens.GlyphProseBullet, gutter-1)
		if n.IsOrdered() {
			mark = padLeft(strconv.Itoa(number)+".", gutter-1)
			number++
		}
		first := []piece{
			{text: mark, st: style{tok: tokens.TextTertiary}},
			{text: " ", st: body},
		}
		saved := r.push(first, indent)
		before := len(r.out)
		r.container(c, !n.IsTight)
		// A marker with no item body is still source text. Emit the pending
		// prefix so a reply such as "32." cannot collapse to zero rows.
		if len(r.out) == before {
			r.emit(nil)
		}
		r.pop(saved, len(r.out) > before)
	}
}

// rule is the faint hairline of a thematic break. It spans the measure and not
// the terminal, so it ends where the sentences above it end.
//
// A thematic break is the AUTHOR's rule, drawn because the document asked for
// one, and it never carries a title — a word set into it would be this renderer
// speaking over the text it is rendering.
func (r *renderer) rule() {
	r.emit([]piece{{text: hairline(r.proseWidth()), st: style{tok: tokens.TextTertiary}}})
}

// htmlBlock draws raw HTML as what it is: literal source the reader was handed,
// at the chrome tier and one row per line. Rendering it would mean growing an
// HTML engine; dropping it would mean silently losing content a model chose to
// send. Showing it quietly is the honest third answer.
func (r *renderer) htmlBlock(n *ast.HTMLBlock) {
	width := r.figureWidth()
	st := style{tok: tokens.TextTertiary}
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		line := scrub(strings.TrimRight(string(seg.Value(r.src)), "\r\n"))
		r.emit([]piece{{text: truncate(line, width), st: st}})
	}
}
