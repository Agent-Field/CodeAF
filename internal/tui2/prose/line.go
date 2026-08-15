package prose

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The line builder. It is this package's own, deliberately: internal/tui2/blocks
// has the same measurements, and importing them would close the tokens → blocks
// edge into a cycle the first time a block wanted to draw prose. What is
// duplicated is thirty lines of arithmetic over one shared ruler; what would be
// duplicated by sharing is a dependency direction.
//
// The ruler is x/ansi, the same one blocks measures with, so a row built here
// and a row built there agree about what a cell is.

// cells is the printable width of s: escape sequences count zero, wide runes
// count two. The ASCII fast path is worth having because most prose is ASCII
// and this runs once per piece per row.
func cells(s string) int {
	if s == "" {
		return 0
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] == 0x1b {
			return ansi.StringWidth(s)
		}
	}
	return len(s)
}

// truncate cuts s to width cells, marking the cut with the static overflow
// ellipsis. It is "…" and not [tokens.GlyphCut]: an ellipsis says "there is
// more, ask for it", which is the truth about a table cell; a cut says "this
// stopped and should not have", which is not (12.5.2).
func truncate(s string, width int) string {
	if width <= 0 || s == "" {
		return ""
	}
	if cells(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(s, width, "…")
}

// pad right-fills s with spaces to exactly width cells, or cuts it if it is
// over. A code row and a table cell are both width-stable by construction, so
// nothing to their right ever dances (5.21).
func pad(s string, width int) string {
	w := cells(s)
	switch {
	case width <= 0:
		return ""
	case w == width:
		return s
	case w > width:
		return truncate(s, width)
	default:
		return s + spaces(width-w)
	}
}

// padLeft is [pad] for a right-aligned cell: the ordered-list marker column and
// every numeric table column. Numbers that share a right edge read as a column
// of numbers; numbers that share a left edge read as a column of strings.
func padLeft(s string, width int) string {
	w := cells(s)
	switch {
	case width <= 0:
		return ""
	case w == width:
		return s
	case w > width:
		return truncate(s, width)
	default:
		return spaces(width-w) + s
	}
}

var (
	spaceRun = strings.Repeat(" ", 256)
	ruleRun  = strings.Repeat(tokens.GlyphTreeDash, 256)
)

// spaces returns n spaces, slicing a preallocated run so an indent never
// allocates.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(spaceRun) {
		return spaceRun[:n]
	}
	return strings.Repeat(" ", n)
}

// hairline returns n cells of the horizontal rule glyph. It is the same run
// trick: a rule under a table header is drawn on most frames that draw a table.
func hairline(n int) string {
	if n <= 0 {
		return ""
	}
	const w = len(tokens.GlyphTreeDash)
	if n*w <= len(ruleRun) {
		return ruleRun[:n*w]
	}
	return strings.Repeat(tokens.GlyphTreeDash, n)
}

// style is everything a run of prose can say about how it is drawn. It is a
// comparable value with no pointer in it, so a wrap decision can compare two
// styles with == and a piece costs one word more than its string.
//
// The two colour fields are not alternatives to each other by accident: tok is
// what the run is painted at when syntax colour does not exist (16 colours and
// below, and every run outside a code fence), and slot is the syntax pastel
// that replaces it when it does. Keeping both means a degraded profile needs no
// second pass — it just never reads slot.
type style struct {
	tok  tokens.Token
	slot tokens.CodeSlot
	// code marks a run inside a fence or a code span, which is the only kind of
	// run allowed to read slot.
	code bool
	// ground raises the run onto [tokens.Sheet], the surface's one subtle
	// raised plane. It is honoured only where [tokens.Profile.SheetGround] says
	// a raised background exists at all.
	ground                  bool
	bold, italic, underline bool
	strike                  bool
}

// body is the style of ordinary prose: the primary tier, unadorned. 5.13 calls
// the primary tier "speech", and a model's answer is the surface's speech.
var body = style{tok: tokens.TextPrimary}

// piece is a run of plain text with one style. Pieces are always UNPAINTED
// while they are being measured and wrapped — painting is the last thing that
// happens to a row, so every width decision is made on bytes a terminal would
// actually show.
type piece struct {
	text string
	st   style
}

// painter turns pieces into bytes. It is the only place in this package that
// writes an escape sequence.
type painter struct {
	styler  *tokens.Styler
	profile tokens.Profile
	focus   tokens.Focus
}

func newPainter(s *tokens.Styler) painter {
	if s == nil {
		return painter{profile: tokens.NoColor, focus: tokens.FocusNormal}
	}
	return painter{styler: s, profile: s.Profile(), focus: s.Focus()}
}

// plain reports whether this painter writes no bytes of its own — a pipe, a
// dumb terminal, NO_COLOR, or a caller that passed no Styler. Callers use it to
// skip work, never to change what the row SAYS: every distinction colour
// carries here also survives as shape, which is why NoColor is a degradation
// and not a failure.
func (p painter) plain() bool { return p.profile == tokens.NoColor }

// paint wraps text in the escape sequences its style asks for. It obeys the
// blocks contract that painting must not change printable width: only escapes
// are added, never a printable byte.
//
// The simple case — a foreground and nothing else — goes through
// [tokens.Styler.PaintToken], so a run that is a single glyph still gets the
// glyph tier's automatic upgrade (12.7). The decorated cases are assembled
// here, because the Styler seam has no vocabulary for bold, and inventing one
// there would put a text attribute in the colour layer.
func (p painter) paint(text string, st style) string {
	if text == "" || p.plain() {
		return text
	}
	fg := st.tok.Fg(p.profile, p.focus)
	if st.code && tokens.CodeHighlighting(p.profile) {
		fg = st.slot.Fg(p.profile, p.focus)
	}
	raised := st.ground && p.profile.SheetGround()
	if !raised && !st.bold && !st.italic && !st.underline && !st.strike && p.styler != nil && !st.code {
		return p.styler.PaintToken(text, st.tok)
	}

	var b strings.Builder
	b.Grow(len(text) + 48)
	var closers string
	if raised {
		b.WriteString(tokens.Sheet.Bg(p.profile, p.focus))
		closers += "49;"
	}
	if fg != "" {
		b.WriteString(fg)
		closers += "39;"
	}
	if st.bold {
		b.WriteString("\x1b[1m")
		closers += "22;"
	}
	if st.italic {
		b.WriteString("\x1b[3m")
		closers += "23;"
	}
	if st.underline {
		b.WriteString("\x1b[4m")
		closers += "24;"
	}
	if st.strike {
		b.WriteString("\x1b[9m")
		closers += "29;"
	}
	b.WriteString(text)
	if closers != "" {
		// One SGR with every attribute this run opened, and only those: a full
		// reset here would clear a caller's own state, which is the reason
		// tokens' own painter spells 39 and 49 out rather than writing SGR 0.
		b.WriteString("\x1b[" + closers[:len(closers)-1] + "m")
	}
	return b.String()
}

// assemble paints a finished row. It is the last step for every row this
// package emits, and the ONLY step that may produce escape bytes, so the width
// ceiling is enforced here on plain text one line above where colour arrives.
//
// Adjacent pieces in the same style are COALESCED first, and that is not a
// micro-optimization — it is the difference between a sentence costing one
// escape pair and a sentence costing one escape pair per word. The wrapper
// splits prose at every space because that is where a line may break; nothing
// downstream should have to pay for a boundary that turned out not to be one.
func (p painter) assemble(row []piece, width int) string {
	row = fit(row, width)
	if len(row) == 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, pc := range row {
		n += len(pc.text)
	}
	if p.plain() {
		// No escapes exist at this profile, so the row is its own bytes.
		if len(row) == 1 {
			return row[0].text
		}
		b.Grow(n)
		for _, pc := range row {
			b.WriteString(pc.text)
		}
		return b.String()
	}
	b.Grow(n + 32)
	var run strings.Builder
	cur := row[0].st
	for _, pc := range row {
		if pc.st != cur {
			b.WriteString(p.paint(run.String(), cur))
			run.Reset()
			cur = pc.st
		}
		run.WriteString(pc.text)
	}
	b.WriteString(p.paint(run.String(), cur))
	return b.String()
}

// fit is the width ceiling made unconditional. Every row that leaves this
// package passes through it, so an arithmetic slip inside a table or a list
// gutter costs a truncated cell rather than a wrapped frame — the failure the
// old chat surface shipped, where one over-wide row pushed every row below it
// sideways.
func fit(row []piece, width int) []piece {
	if width <= 0 {
		return nil
	}
	total := 0
	for _, pc := range row {
		total += cells(pc.text)
	}
	if total <= width {
		return row
	}
	head, _ := cut(row, width)
	// The last surviving piece takes the ellipsis, so the cut is visible rather
	// than silent. It is re-truncated to the width it already had, which is what
	// makes the mark cost a cell of its own content and not a cell of the row.
	if n := len(head); n > 0 {
		last := head[n-1]
		head[n-1].text = truncate(last.text+"…", cells(last.text))
	}
	return head
}

// cut splits a row of pieces at n printable cells. A wide rune straddling the
// boundary stays with the tail rather than being halved — half a rune is not a
// cell, and ansi.Cut is the ruler that decides.
func cut(row []piece, n int) (head, tail []piece) {
	if n <= 0 {
		return nil, row
	}
	used := 0
	for i, pc := range row {
		w := cells(pc.text)
		if used+w <= n {
			head = append(head, pc)
			used += w
			continue
		}
		want := n - used
		left := ansi.Cut(pc.text, 0, want)
		if left == "" && used == 0 {
			// One cell left and a two-cell grapheme in front of it: nothing fits,
			// and a cut that consumed nothing is a cut that never ends —
			// [wrapper.commit] loops on the tail until the process dies. A cluster
			// cannot be halved, so the row takes the whole of it and goes one cell
			// over; [fit] is the ceiling and trims the row before it is drawn. This
			// only ever fires when the head is empty, so the advance is real.
			for over := want + 1; over <= want+2 && left == ""; over++ {
				left = ansi.Cut(pc.text, 0, over)
			}
		}
		if left != "" {
			head = append(head, piece{text: left, st: pc.st})
		}
		right := ansi.Cut(pc.text, cells(left), w)
		if right != "" {
			tail = append(tail, piece{text: right, st: pc.st})
		}
		tail = append(tail, row[i+1:]...)
		return head, tail
	}
	return head, nil
}

// scrub neutralizes the control bytes that survive sanitizing. internal/sanitize
// keeps '\n' and '\t' on purpose — they are structure to a parser — and this
// package has already given both to goldmark by the time a leaf's text arrives
// here, so what reaches a CELL must have neither.
//
// A tab becomes one space rather than a tab stop: a tab is a cursor movement,
// and a row whose width depends on where the cursor happened to be is a row no
// ruler can measure. Everything else below 0x20, plus DEL, is dropped. C1 bytes
// and every escape are already gone at the chokepoint (see [sanitizeSource]).
func scrub(s string) string {
	clean := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
