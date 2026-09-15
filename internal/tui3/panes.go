package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── TWO PANES, SIDE BY SIDE ─────────────────────────────────────────────────
//
// ONE WIDTH DECIDES WHETHER A FRAME HOLDS TWO COLUMNS, AND THIS SURFACE ALREADY
// HAD IT. The task column stands beside the conversation from [railSlimFloor]
// up and under it "the transcript is the thing a person came for" (task.go);
// the owner's picks of 2026-09-11 put a question's answers beside the evidence
// of the one the pointer is on at the same width (preview A, page A, "≥100
// cols") and fold that evidence under its row below it (preview-narrow B). So
// there is one reading of it, [besideFits], and one way to lay two columns out,
// [besides] — the panel and the page both ask here, and neither measures its
// own. (The asker's own two-pane layout block decides by its CONTENT instead:
// its panes stand side by side wherever both fit whole, which is the reason a
// fixed width was only ever standing in for — questionblocks.go.)
//
// A SEAM, NOT A BOX. The two panes are one object split in two, so the line
// between them is the frame's own side ([tokens.GFrameSide]) and the rules
// above and below meet it with the frame's junctions ([tokens.GFrameTeeDown],
// [tokens.GFrameTeeUp]). A terminal refused box drawing gets the frame's ASCII
// run — a rule of `-` and a blank seam — for the frame's own reason: a column
// of `|` down every row is punctuation a screen reader reads aloud on every
// line, and a blank column already says where one pane ends.

// besideFits reports whether a frame of this width has room for two panes
// side by side.
func besideFits(width int) bool { return width >= railSlimFloor }

// besideSplit is where the seam falls in a frame of `width` cells whose left
// pane would like `want` of them.
//
// THE LEFT PANE GETS WHAT ITS WIDEST ROW NEEDS AND NEVER MORE THAN HALF. The
// right pane is what the split exists to show — the evidence — so it is always
// given at least as much as the list beside it; and the list is given exactly
// what its widest row needs, so no answer's word is cut while the evidence has
// room to give. The seam itself is one cell.
func besideSplit(width, want int) (left, right int) {
	half := (width - 1) / 2
	left = min(max(want, 1), half)
	return left, max(width-left-1, 0)
}

// besides lays two columns of painted rows side by side at one width: the left
// set to `left` cells, the seam, the right set to the rest. The shorter column
// is padded with blank rows so the seam runs the height of the taller.
func besides(p palette, lefts, rights []string, left, width int) []string {
	right := max(width-left-1, 0)
	seam := p.dim(framePiecesOf(p).side)
	tall := max(len(lefts), len(rights))
	out := make([]string, 0, tall)
	for i := range tall {
		l, r := "", ""
		if i < len(lefts) {
			l = lefts[i]
		}
		if i < len(rights) {
			r = rights[i]
		}
		out = append(out, frameSet(l, left)+seam+frameSet(r, right))
	}
	return out
}

// besideRule is a full-width rule that meets a seam at column `at` with the
// junction `tee`, and carries words written into it the way the frame writes
// words into an edge ([framed.edge]). `at` below zero is a rule with no seam
// to meet.
//
// THE WORDS START TWO CELLS IN, and where the seam falls inside them they move
// past it, so a junction never lands in the middle of a word and a rule over a
// narrow left pane still says what it carries.
func besideRule(p palette, width, at int, tee tokens.GlyphID, words string) string {
	if width <= 0 {
		return ""
	}
	pieces := framePiecesOf(p)
	junction := pieces.edge
	if at >= 0 && !p.ascii {
		junction = p.glyph(tee)
	}
	edge := func(n int) string { return strings.Repeat(pieces.edge, max(n, 0)) }
	wordsW := ansi.StringWidth(words)
	if words == "" {
		if at < 0 || at >= width {
			return p.dim(edge(width))
		}
		return p.dim(edge(at) + junction + edge(width-at-1))
	}
	// Where the words would cross the seam they start one rule cell past it.
	start := 2
	if at >= 0 && at < width && start+wordsW+1 > at {
		start = at + 2
	}
	room := max(width-start-2, 0)
	if wordsW > room {
		words, wordsW = fitWidth(words, room)
	}
	if wordsW == 0 {
		return besideRule(p, width, at, tee, "")
	}
	var b strings.Builder
	lead := start - 1
	if at >= 0 && at < lead {
		b.WriteString(p.dim(edge(at) + junction + edge(lead-at-1)))
	} else {
		b.WriteString(p.dim(edge(lead)))
	}
	b.WriteString(" ")
	b.WriteString(words)
	b.WriteString(" ")
	used := lead + wordsW + 2
	if at >= used && at < width {
		b.WriteString(p.dim(edge(at-used) + junction + edge(width-at-1)))
	} else {
		b.WriteString(p.dim(edge(width - used)))
	}
	return b.String()
}
