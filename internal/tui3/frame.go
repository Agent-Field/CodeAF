package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE ONE FRAME ───────────────────────────────────────────────────────────
//
// docs/DESIGN-LANGUAGE.md refused borders — "one continuous surface: zones are
// made of typography, alignment and at most a hairline rule" — and three dim
// rounded boxes shipped anyway, each with a written argument for being the
// exception and each with its own copy of the six pieces: the `/folder` chooser
// (contextmodal.go), the `alt+k` switcher card (hop.go) and the onboarding
// panel (onboarding.go). The owner's ruling of 2026-09-11 made a fourth kind of
// thing framed on purpose — a QUESTION hangs above the box as one object — and
// said the fourth must not be a fourth copy. So there is one frame, this one:
//
// THE QUESTION, THE SWITCHER CARD AND THE ONBOARDING PANEL ARE ON IT. The
// `/folder` chooser is NOT, yet, and it is the one box still carrying its own
// pieces: it grew a degradation ladder of its own the same week (a two-row
// terminal drops the head, a three-row one drops the box, and this frame always
// draws both edges), and moving it is a change to that ladder rather than a
// change of drawing. It is named here rather than left to be discovered.
//
//	╭─ ? Which storage for the session index? ──────────── model asks ─╮
//	│ ▸ 1  SQLite         one file beside the conversation  ◆ recommended │
//	│   2  JSONL          append-only, no new dependency                  │
//	╰─ ↑↓ choose · enter take it · esc later ──────────────── c change ─╯
//
// FOUR DECISIONS, AND EACH IS WHY IT IS ONE TYPE.
//
//   - THE EDGE IS DIM, ALWAYS. A frame is a claim about what a thing IS — one
//     object, apart from the conversation — and never emphasis: it does not light,
//     thicken or follow a cursor (THE EMPHASIS LAW, amended in the same ruling).
//     What is inside it is painted by its owner; the frame paints only itself.
//   - THE TOP EDGE CARRIES A TITLE AND AN ASIDE, THE BOTTOM EDGE KEYS AND AN
//     ASIDE. Words written INTO an edge cannot be mistaken for content, which is
//     the one thing a frame is better at than whitespace. An edge too narrow for
//     both gives up its aside first and cuts its title last.
//   - EVERY ROW IS SET TO ONE WIDTH, so the right edge lands in one column on
//     every row. A row measured its own way is a frame with a notch in it.
//   - A TERMINAL REFUSED BOX DRAWING GETS TWO PLAIN RULES AND NO SIDES. The
//     pieces are vocabulary slots ([tokens.GFrameTopLeft] and the rest), and they
//     are geometry, so the ASCII tier keeps their plain byte; the ASCII spelling
//     of a grid is a RUN of characters the grid's owner draws (tokens' own
//     rule), and this is the owner. The geometry does not move: a side becomes
//     one blank cell, not zero, so a row reads in the same column either way.

// framed is one framed object: what its edges say, and the ground its edge cells
// stand on. Every field is already painted by the caller except the edge
// itself — the frame does not know whether a title is a question's ink head or
// a chooser's bold name, and it must not.
type framed struct {
	// title is the top edge's left words, painted.
	title string
	// aside is the top edge's right words, painted (a caller paints them dim).
	aside string
	// keys is the bottom edge's left words, painted.
	keys string
	// keysAside is the bottom edge's right words, painted.
	keysAside string
	// ground paints the frame's own cells onto a raised surface where the
	// owner stands on one (the switcher card's); nil leaves the terminal's own
	// ground, which is REST and is not a colour.
	ground func(string, int) string
}

// frameInner is how many cells a frame of this width lays its rows in: the
// width less its two sides. It is the ONE answer, asked by every owner before it
// lays out a row and by [framed.draw] when it sets one.
func frameInner(width int) int { return max(width-2, 0) }

// frameEdgeRoom is how many cells an edge has for words: the width less the
// two corners, the rule cell and the space either side of the words, and the
// one cell of rule that always follows them. An owner that composes a key line
// to fit the bottom edge asks this.
func frameEdgeRoom(width int) int { return max(width-7, 0) }

// framePieces is the six pieces this terminal draws the frame with.
type framePieces struct{ tl, tr, bl, br, edge, side string }

// framePiecesOf answers the six pieces at whichever floor this terminal stands
// on: the vocabulary's own slots, or the frame's ASCII run.
func framePiecesOf(p palette) framePieces {
	if p.ascii {
		// TWO PLAIN RULES AND NO SIDES. A `+` in a corner would be a box drawn
		// in punctuation, which reads as a table; a rule of `-` above and below
		// is the hairline the design language already allows everywhere.
		return framePieces{tl: "-", tr: "-", bl: "-", br: "-", edge: "-", side: " "}
	}
	return framePieces{
		tl: p.glyph(tokens.GFrameTopLeft), tr: p.glyph(tokens.GFrameTopRight),
		bl: p.glyph(tokens.GFrameBottomLeft), br: p.glyph(tokens.GFrameBottomRight),
		edge: p.glyph(tokens.GFrameEdge), side: p.glyph(tokens.GFrameSide),
	}
}

// draw lays painted rows inside the frame at width — the top edge, one row per
// row, the bottom edge — and reports the cells the bottom edge's aside landed
// in, counted from the frame's own left edge, for an owner whose aside is a
// target (the chooser's `esc · cancel`). A row wider than [frameInner] is cut
// at the edge and a narrower one padded to it.
//
// A frame too narrow to hold its own corners draws its rows bare: an edge of
// two corners and nothing between them says less than the rows alone.
func (f framed) draw(p palette, width int, rows []string) ([]string, hudSpan) {
	if width < 4 {
		return rows, hudSpan{}
	}
	pieces := framePiecesOf(p)
	inner := frameInner(width)
	paint := func(s string, w int) string {
		if f.ground != nil {
			return f.ground(s, w)
		}
		return s
	}
	out := make([]string, 0, len(rows)+2)
	top, _ := f.edge(p, pieces, pieces.tl, pieces.tr, f.title, f.aside, width)
	out = append(out, paint(top, width))
	side := paint(p.dim(pieces.side), 1)
	for _, row := range rows {
		out = append(out, side+frameSet(row, inner)+side)
	}
	bottom, span := f.edge(p, pieces, pieces.bl, pieces.br, f.keys, f.keysAside, width)
	out = append(out, paint(bottom, width))
	return out, span
}

// rule is the frame FOLDED FLAT: one titled rule, drawn with the same pieces
// and the same arithmetic as an edge, with no corners and nothing under it.
//
//	── ? Which storage for the session index? · 3 answers · ◆ SQLite ── space open ──
//
// A question put off is still the same object, one row tall — so it is the same
// frame said in one row rather than a second drawing with a grammar of its own.
func (f framed) rule(p palette, width int) string {
	pieces := framePiecesOf(p)
	row, _ := f.edge(p, pieces, pieces.edge, pieces.edge, f.title, f.aside, width)
	return row
}

// edge is one horizontal edge with its words written into it: the corner, a
// rule cell and a space, the left words, a run of rule, the right words, a
// space and a rule cell, the corner. The run between the two is at least one
// cell, because two sets of words touching read as one sentence.
func (f framed) edge(p palette, pieces framePieces, left, right, words, aside string, width int) (string, hudSpan) {
	room := frameEdgeRoom(width)
	wordsW, asideW := ansi.StringWidth(words), ansi.StringWidth(aside)
	// THE ASIDE IS GIVEN UP FIRST AND THE WORDS ARE CUT LAST. The words are what
	// the edge is for — a question's head, the keys that answer it — and the
	// aside is the second fact beside them.
	if aside != "" && wordsW+asideW+2 > room {
		aside, asideW = "", 0
	}
	if wordsW > room {
		words, wordsW = fitWidth(words, room)
	}
	lead := p.dim(left + pieces.edge)
	if words == "" && aside == "" {
		return lead + p.dim(strings.Repeat(pieces.edge, max(width-3, 0))+right), hudSpan{}
	}
	var b strings.Builder
	b.WriteString(lead)
	used := 2
	if words != "" {
		b.WriteString(" ")
		b.WriteString(words)
		b.WriteString(" ")
		used += wordsW + 2
	}
	// The run fills to where the aside starts: the aside, its space, the rule
	// cell and the corner are the last cells of the edge.
	tail := 2
	if aside != "" {
		tail += asideW + 2
	}
	run := max(width-used-tail, 1)
	b.WriteString(p.dim(strings.Repeat(pieces.edge, run)))
	span := hudSpan{}
	if aside != "" {
		b.WriteString(" ")
		span = hudSpan{from: used + run + 1, to: used + run + 1 + asideW}
		b.WriteString(aside)
		b.WriteString(" ")
	}
	b.WriteString(p.dim(pieces.edge + right))
	return b.String(), span
}

// frameSet sets one painted row to exactly the inner width, measuring through
// the escape sequences rather than around them.
func frameSet(painted string, room int) string {
	if room < 1 {
		return ""
	}
	if ansi.StringWidth(painted) > room {
		// AND A TRUNCATION IS PADDED TOO. A cut lands BEFORE a cell it cannot
		// halve, so a row ending in a wide rune comes back one column short of
		// the room asked for — and one short row puts a notch in the right edge,
		// which is the one thing "EVERY ROW IS SET TO ONE WIDTH" is for. The pad
		// below is the same pad, measured again after the cut.
		painted = ansi.Truncate(painted, room, "")
	}
	return painted + strings.Repeat(" ", max(room-ansi.StringWidth(painted), 0))
}
