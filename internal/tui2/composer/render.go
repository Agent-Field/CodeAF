package composer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The paint, and the one place the composer's geometry is decided.
//
// # Everything the composer owns grows UPWARD
//
// The draft's rows and its completion lists are drawn ABOVE the prompt's anchor
// row, not below it. That is 8's rule, and the reason is geometry rather than
// taste: this region is welded to the bottom edge of the frame, so the only
// direction with room in it is up. A list that opened downward at the bottom of
// a terminal opens into the status line, which is where the user-reported bug
// came from.
//
// The order within the block above the draft is relevance, nearest first: the
// completion list is furthest away, the chips that describe what the SEND will
// carry sit against the draft, and the highlighted candidate is the row the
// draft's own prompt is one line under. The eye travels the least distance to
// the thing it is about to choose.
//
// # One plan, four readers
//
// Render, [Model.ClickCaret], [Model.ClickHint] and [Model.HoverHint] all read
// the same [Model.plan]. Nothing is recorded during a paint (Part 2's
// anti-pattern 14) and nothing is computed twice with two spellings, so there
// is no draft, width or completion state under which a click can land on a row
// the paint did not draw.

// maxDraftRows caps how tall a draft may push the composer. Eight rows is a
// paragraph — long enough that nobody composing a message hits the ceiling, and
// short enough that the conversation the message is about is still on screen.
// Beyond it the draft scrolls inside its own rectangle, tail-anchored, so the
// row being typed is always the last one drawn.
//
// The cap lives here rather than being handed down as "half the pane" because a
// pane is told its rectangle and never asks about anyone else's (tui2/pane.go).
// The layout is the other half of the same guard: [tui2.Shell.GrowComposer]
// treats [Model.GrowRows] as a REQUEST, so a short window gives back less.
const maxDraftRows = 8

// THE COMPOSER'S LEFT EDGE, in §20's numbers.
//
// §20 states one geometry for every surface: a two-cell GUTTER holding the
// block's marker and one space, and a CONTENT EDGE at the cell after it that
// every sentence on the surface hangs from. §19 adds the one rule an input
// surface has of its own: text never touches the FIELD's own edge — one cell of
// inner padding, left and right, inside the ground the field is drawn on.
//
// Composed, those two laws give the composer exactly four cells before a letter,
// and each one is a different thing:
//
//	col 0   the hug edge (`▍`), the field's own edge worn as state colour
//	col 1   §19's inner padding — the field's ground, and nothing on it
//	col 2   §20's gutter: the prompt glyph, or the spinner while a reply streams
//	col 3   §20's gutter: the one space that separates a marker from its content
//	col 4   the CONTENT EDGE — typed text, ghost hint, candidate rows, chips
//
// and one cell of the same padding is held back at the right, so the wrap
// measure is the padded width rather than the rectangle's.
//
// THIS IS THE DEFECT. It shipped without cols 1 and the right pad, so the prompt
// sat welded to the edge block and a wrapped line ran into the field's last
// column — reported as "does not seem to be proper padding for input text nor
// thinking part etc.. it seems to be really bad". The ghost hint, the streaming
// prompt and the completion rows all hung from the unpadded edge for the same
// reason: every one of them is measured from these constants.
const (
	// hugEdgeCells is the state edge at column 0. It is chrome at the field's
	// own edge (§19: "chrome sits at the left edge"), so the padding goes INSIDE
	// it rather than before it — an edge indented off its own field would be an
	// edge of nothing.
	hugEdgeCells = 1
	// gridGutter is §20's G: the marker and one space. Two cells, on every
	// surface in the product.
	gridGutter = 2
	// innerPad is §19's one cell, spent on each side of the field's content.
	innerPad = 1
	// promptGutter is the edge and the gutter together — everything left of the
	// content edge that is not padding. It is still 3, which is why nothing about
	// the shedding ladder below changed.
	promptGutter = hugEdgeCells + gridGutter
)

// padAt is the inner padding this width can afford, per side.
//
// The padding is the FIRST thing a narrowing composer sheds, ahead of the
// separator and the prompt, because it is the only one of the four cells that
// carries no meaning: it is rhythm, and rhythm is what a terminal too narrow to
// hold a word cannot pay for. Below the threshold the composer draws exactly the
// row it drew before this law existed.
func padAt(width int) int {
	if width < promptGutter+2*innerPad+1 {
		return 0
	}
	return innerPad
}

// innerWidth is the field's content rectangle: the row less both pads. Every
// measurement the composer makes — the wrap, the chrome rows, the ghost hint's
// budget — is taken against this rather than against the row, which is what
// makes the right-hand pad real instead of a comment.
func innerWidth(width int) int { return width - 2*padAt(width) }

// usable is the draft's own text width: the field's content rectangle less the
// gutter. It is a function rather than three lines inside Render because the
// pointer has to wrap the draft the same way the paint does, and two spellings
// of one number is how a click lands on the wrong row.
//
// The two narrow cases are the gutter shedding from the right, in the order the
// cells earn their place: the space goes first (a separator with nothing to
// separate), then the prompt (the edge already says the surface is live and
// what it is doing, which is the more load-bearing half). The edge is never
// dropped — a row with no gutter at all is a row that has stopped being a
// composer.
func usable(width int) int {
	inner := innerWidth(width)
	return inner - gutterAt(inner)
}

// gutterAt is the gutter this width can afford. Its argument is the field's
// content rectangle, never the row — the pad is already spent by the time the
// gutter is asked what it costs.
func gutterAt(width int) int {
	switch {
	case width < 2:
		return 1
	case width < promptGutter:
		return 2
	}
	return promptGutter
}

// plan is the composer's row layout at one rectangle: which rows the chrome
// above the draft takes, and which of the draft's own rows are on screen.
type plan struct {
	// above are the chrome rows, top to bottom, drawn before the draft.
	above chrome
	// rows is the whole draft, hard-wrapped.
	rows []span
	// first is the index of the topmost visible draft row, and visible is how
	// many of them there are.
	first, visible int
	// cursorRow is the draft row the caret sits on.
	cursorRow int
}

// draftTop is the screen row the draft's first visible row is drawn at.
func (p plan) draftTop() int { return len(p.above.rows) }

// plan solves the rectangle. The DRAFT is served first and the chrome takes
// what is left: a list that pushed the words being typed off their own surface
// would have the priority exactly backwards, and the region has already asked
// the layout to grow by exactly the rows both of them want ([Model.GrowRows]).
func (m *Model) plan(sty *tokens.Styler, width, height int) plan {
	var p plan
	if width <= 0 || height <= 0 {
		return p
	}
	p.rows = layoutRows(m.value, usable(width))
	p.cursorRow = rowOf(p.rows, m.cursor)

	want := min(len(p.rows), maxDraftRows)
	want = min(want, height)
	// The chrome is measured against the FIELD's rectangle, not the row's: a
	// candidate list that ran to the last column would be the one part of this
	// region ignoring the padding the draft above it keeps.
	p.above = m.chromeRows(sty, innerWidth(width), height-want)

	drafted := height - len(p.above.rows)
	p.visible = min(drafted, len(p.rows))
	if len(p.rows) > drafted {
		// Tail-anchored: the row the caret is on is the last one drawn, which is
		// the row directly above the chrome-free bottom edge of the region.
		p.first = p.cursorRow - (drafted - 1)
		if p.first < 0 {
			p.first = 0
		}
		if maxTop := len(p.rows) - drafted; p.first > maxTop {
			p.first = maxTop
		}
	}
	return p
}

// GrowRows is how many rows beyond its anchor row the composer wants at this
// width: the draft's own wrapped rows, capped by [maxDraftRows], plus whatever
// an open completion list is asking for.
//
// The region cannot see either number any other way. The composer draws inside
// one rectangle (tui2/pane.go) and the layout budgets that rectangle for a
// single draft row and two strips, so the wiring asks this, asks the shell for
// that many extra rows, and both the draft and its list have somewhere to be —
// see [tui2.Shell.GrowComposer], which treats the answer as a request the
// layout may refuse rather than as a size. When it is refused, plan gives the
// draft its rows first and the list scrolls.
//
// It counts no chip. Attachment chips and the dispatch chip are steady chrome
// the metric table already pays for; a region that grew for them would move the
// transcript every time a path was typed, which is 5.21's dancing.
func (m *Model) GrowRows(width int) int {
	rows := 1
	if width > 0 {
		rows = min(len(layoutRows(m.value, usable(width))), maxDraftRows)
	}
	return rows - 1 + m.HintRows()
}

func (m *Model) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	sty := m.activeStyler()
	p := m.plan(sty, width, height)

	// The chrome rows carry no edge glyph of their own, so their padding is a
	// plain left offset; the draft's rows wear the edge at column 0 and take the
	// pad INSIDE it (see [Model.renderRow]). Both land on the same content edge,
	// which is the property §20 asks for and the one a reader actually sees.
	//
	// The right-hand pad needs no cells written: every row is measured short and
	// the strip's own ground fills out to the edge (chat's seamStrip).
	lead := strings.Repeat(" ", padAt(width))
	lines := make([]string, 0, len(p.above.rows)+p.visible)
	for _, row := range p.above.rows {
		lines = append(lines, lead+row)
	}
	for i := p.first; i < p.first+p.visible; i++ {
		lines = append(lines, m.renderRow(sty, p.rows[i], i, i == p.cursorRow, width))
	}
	return strings.Join(lines, "\n")
}

// renderRow draws one display row: the prompt (row 0 of the whole draft) or
// an aligned indent (every other row), the row's text, and — when this row
// holds the cursor and the pane is focused — the accent caret marking it.
func (m *Model) renderRow(sty *tokens.Styler, sp span, rowIdx int, hasCursor bool, width int) string {
	inner := innerWidth(width)
	pad := padAt(width)
	gutter := gutterAt(inner)
	prefixWidth := pad + gutter
	glyph, tok := m.promptCell(sty)
	// The EDGE runs down every drafted row and the prompt marks only the first.
	// A draft that wrapped to four lines is still one thing being typed, and an
	// edge that stopped after the first row would have said the rows below it
	// belonged to something else; a prompt repeated on each of them would have
	// said the opposite — four sentences waiting to be sent. One mark for the
	// surface, one mark for where it starts.
	//
	// THE PAD SITS AFTER THE EDGE, and that ordering is the decision rather than
	// an accident of concatenation. The edge is the field's own boundary worn as
	// state colour, and a boundary indented off the thing it bounds is a boundary
	// of nothing; §19 puts chrome at the left edge and the padding inside it. So
	// the row reads edge · pad · marker · space, and the marker lands in §20's
	// gutter measured from the padded edge rather than from the frame's.
	cell := tokens.GlyphHugEdge + strings.Repeat(" ", pad)
	if rowIdx == 0 && gutter >= 2 {
		cell += glyph
	}
	prefix := paint(sty, padCells(cell, prefixWidth), tok)

	var body string
	switch {
	case len(m.value) == 0 && rowIdx == 0:
		// The ghost hint's budget is the DRAFT's measure, so the hint stops one
		// cell short of the field's edge exactly as a typed sentence does — §19's
		// "the hint/streaming line hangs from the same inner edge as the typed
		// text", kept on both edges rather than only on the left.
		body = m.renderEmptyBody(sty, usable(width))
	case hasCursor && m.focused:
		body = m.renderCursorBody(sty, sp)
	default:
		body = paint(sty, string(m.value[sp.Start:sp.End]), tokens.TextPrimary)
	}

	return ansi.Truncate(prefix+body, pad+inner, "")
}

// promptCell is the one cell that says what this surface is and whether it is
// awake: the prompt glyph in its tier, accented while the composer holds the
// keyboard — and the braille spinner, on the shared clock, while a reply is
// being written (8, 11).
//
// The spinner takes the prompt's cell rather than a cell beside it. Both are
// one column under both rulers, so nothing on the row moves when the answer
// starts or stops arriving; and a live signal that displaced a character would
// be motion the layout has to pay for, which 5.21's width-stability rule bans.
// The ladder is precedence, most-live first, and each rung is a thing that is
// TRUE RIGHT NOW rather than a mode someone selected: a reply arriving outranks
// a question waiting, which outranks a send that failed a moment ago, which
// outranks the resting prompt. The colour rides with the glyph — see
// [State.cell] — so the edge one column to the left says the same thing without
// being read.
func (m *Model) promptCell(sty *tokens.Styler) (string, tokens.Token) {
	if m.streaming {
		return tokens.Spinner(m.frame), tokens.Cyan
	}
	if id, tok, replaces := m.state.cell(); replaces {
		return sty.Glyph(id), tok
	}
	tok := tokens.TextTertiary
	if m.focused {
		tok = tokens.Cyan
	}
	return m.prompt.glyph(sty), tok
}

// renderEmptyBody draws the empty draft: the caret where typing will land, and
// — only while this composer holds the keyboard — the ghost hint after it.
//
// An UNFOCUSED empty composer says nothing at all. That is the change from the
// placeholder it replaces: a label sat there whether or not anyone could type
// into it, which is a surface talking to a reader who is looking somewhere else.
func (m *Model) renderEmptyBody(sty *tokens.Styler, room int) string {
	if !m.focused {
		return ""
	}
	caret := m.caretCell(sty, "")
	if m.typed {
		// 8: it vanishes on the first keystroke and never reappears mid-draft.
		return caret
	}
	hint := ghostHint(m.hintNow().cells(m.hintDetail), room-cellsOf(caret)-1)
	if hint == "" {
		return caret
	}
	return caret + paint(sty, " "+hint, tokens.TextTertiary)
}

// cellsOf is the printable width of an already-painted span. The caret is one
// cell when it is drawn and none when the host owns the terminal's own, and the
// hint's budget has to know which.
func cellsOf(painted string) int { return ansi.StringWidth(painted) }

// ghostHint joins as many hint cells as fit in room cells, in order. It sheds
// from the RIGHT and never cuts a cell in half: half a chord teaches a key that
// does not exist, and `?` — the door to every other door — is therefore the
// last cell standing on a narrow terminal.
func ghostHint(cells []string, room int) string {
	if room <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	for _, cell := range cells {
		next := ansi.StringWidth(cell)
		if b.Len() > 0 {
			next += ansi.StringWidth(hintSep)
		}
		if width+next > room {
			break
		}
		if b.Len() > 0 {
			b.WriteString(hintSep)
		}
		b.WriteString(cell)
		width += next
	}
	return b.String()
}

// renderCursorBody draws a non-empty row that holds the cursor: the text
// before it, the caret cell, and the text after it.
func (m *Model) renderCursorBody(sty *tokens.Styler, sp span) string {
	col := m.cursor - sp.Start
	before := string(m.value[sp.Start : sp.Start+col])
	var at, after string
	if m.cursor < sp.End {
		at = string(m.value[m.cursor])
		after = string(m.value[m.cursor+1 : sp.End])
	}
	return paint(sty, before, tokens.TextPrimary) +
		m.caretCell(sty, at) +
		paint(sty, after, tokens.TextPrimary)
}

// caretCell paints the caret over the character at it, which may be nothing at
// all when the caret sits past the last character of its row.
//
// The caret is an accent BLOCK, and it is drawn two ways for one reason: a
// block is a character, and a character cannot be laid over a letter without
// eating it. So an empty cell gets the block itself, and an occupied one keeps
// its letter and takes the accent as its colour on the selection band — the
// same "raised ground, own tier" idiom 5.16 gives selection everywhere else.
//
// [tokens.Cyan] is the accent 12 permits here: it is the alive hue, and the
// palette names the caret in its own definition ("alive: working glyphs, stream
// caret, thinking pulse"). It is a legal foreground over [tokens.Band] and a
// legal foreground on the ground, so the contrast gate has measured both.
func (m *Model) caretCell(sty *tokens.Styler, at string) string {
	if m.hostCursor {
		// The shell is putting the terminal's real cursor on this cell
		// ([Model.CaretAt]), so the cell keeps its character and nothing else.
		return paint(sty, at, tokens.TextPrimary)
	}
	if at == "" || at == " " {
		return paint(sty, caretBlock, tokens.Cyan)
	}
	return paintOn(sty, at, tokens.Cyan, tokens.Band)
}

// padCells right-pads s with spaces until it occupies n cells. Used only for
// the prompt glyph against a prefixWidth of 2 (glyph + one space) — at
// prefixWidth 1 (width == 1) it is a no-op, which is exactly the degraded
// "glyph only, no room for the separator" case doc.go describes.
func padCells(s string, n int) string {
	w := ansi.StringWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}
