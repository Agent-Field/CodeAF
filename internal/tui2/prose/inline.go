package prose

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Inline rendering: the AST's leaf text, carrying whatever emphasis it inherited
// on the way down.
//
// Style is passed DOWN by value rather than pushed on a stack, so `**bold with
// *italic* inside**` composes by construction and no unwind can leave an
// attribute switched on. The wrapper below receives runs, never rows: where a
// line ends is a decision about width, and nothing in this file knows the width.

func (r *renderer) inline(n ast.Node, w *wrapper, st style) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			w.push(scrub(string(c.Segment.Value(r.src))), st)
			switch {
			case c.HardLineBreak():
				w.hardBreak()
			case c.SoftLineBreak():
				// A soft break is where the AUTHOR's line ended, not where the
				// reader's must: it becomes a word boundary and the text
				// reflows. That is the whole reason a terminal renderer exists
				// rather than a pager.
				w.space()
			}
		case *ast.String:
			w.push(scrub(string(c.Value)), st)
		case *ast.CodeSpan:
			r.codeSpan(c, w, st)
		case *ast.Emphasis:
			next := st
			// CommonMark counts delimiters, not names: one is stress, two is
			// strong, three is both.
			if c.Level == 1 || c.Level >= 3 {
				next.italic = true
			}
			if c.Level >= 2 {
				next.bold = true
			}
			r.inline(c, w, next)
		case *extast.Strikethrough:
			next := st
			next.strike = true
			r.inline(c, w, next)
		case *ast.Link:
			r.link(c, w, st, r.inlineText(c), string(c.Destination))
		case *ast.Image:
			// An image is a link whose text is its alt text. A terminal cannot
			// show the picture, and the honest substitute for a picture is the
			// caption the author wrote for it plus where it lives.
			r.link(c, w, st, r.inlineText(c), string(c.Destination))
		case *ast.AutoLink:
			url := scrub(string(c.URL(r.src)))
			r.link(c, w, st, scrub(string(c.Label(r.src))), url)
		case *ast.RawHTML:
			// A tag is markup, not content. Its text has already been emitted
			// as siblings, so drawing `<em>` here would show the reader the
			// author's punctuation twice.
		default:
			r.inline(c, w, st)
		}
	}
}

// inlineText flattens a node's inline children to plain text — a link's label,
// a table cell, an image's alt. It is deliberately style-blind: these are
// places where a run of bold inside a cell that is about to be truncated buys
// nothing and costs a style boundary in the middle of an ellipsis.
func (r *renderer) inlineText(n ast.Node) string {
	var b strings.Builder
	r.collect(n, &b)
	return scrub(b.String())
}

func (r *renderer) collect(n ast.Node, b *strings.Builder) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(r.src))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		case *ast.AutoLink:
			b.Write(c.Label(r.src))
		case *ast.RawHTML:
		default:
			r.collect(c, b)
		}
	}
}

// codeSpan draws inline code on the surface's one raised plane.
//
// Where there is no raised plane to draw on — [tokens.Profile.SheetGround] is
// false at 16 colours and below, for the reason stated there — the backticks
// come BACK. That is not a regression to "renders raw": it is the affordance
// refusing to lie (5.20). A span with no ground and no mark is a span the
// reader cannot tell from the sentence around it, and `len(x)` read as prose is
// worse than `len(x)` read as source.
func (r *renderer) codeSpan(n *ast.CodeSpan, w *wrapper, st style) {
	text := r.inlineText(n)
	if text == "" {
		return
	}
	next := st
	next.tok = tokens.TextPrimary
	if !r.p.profile.SheetGround() {
		next.tok = tokens.TextSecondary
		w.push("`"+text+"`", next)
		return
	}
	next.ground = true
	w.push(text, next)
}

// link draws the text underlined and the destination dim beside it — the
// reader gets the words the author chose AND the address they resolve to,
// because a terminal has no status bar to reveal the second on hover.
//
// When the two are the same string — an autolink, a bare URL — the address is
// drawn once. Repeating it would be the surface saying the same thing twice and
// spending a third of the measure to do it.
func (r *renderer) link(n ast.Node, w *wrapper, st style, label, dest string) {
	dest = scrub(dest)
	if label == "" {
		label = dest
	}
	if label == "" {
		return
	}
	linked := st
	linked.underline = true
	w.push(label, linked)
	if dest == "" || dest == label {
		return
	}
	w.space()
	w.push("("+dest+")", style{tok: tokens.TextTertiary})
}
