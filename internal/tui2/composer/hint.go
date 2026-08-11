package composer

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The composer's own chrome under the draft: the open `@` filter's candidate
// list, or — once a mention token exists — the dispatch chip.
//
// Both live BELOW the draft rows and above whatever the wiring welds to the
// region's bottom edge (internal/tui2/chat draws a place line above and a meta
// strip below; it hands this package the rows in between). Below rather than
// above is deliberate: a list that opened above the draft would push the line
// the user is typing down the screen on every '@', and a composer whose prompt
// moves while you type is the kind of dancing 5.21's width-stability rule bans
// in the small.
//
// Neither ever takes the draft's last row. A composer that hid what you were
// writing to show you what you could address would have the priority exactly
// backwards.

const (
	// historyGroup is the dim group heading settled targets sort under (5.18).
	historyGroup = "history"
	// historyGroupNote says, in the only place a user is looking when it
	// matters, what selecting a settled row will actually do. 5.18 forbids
	// direct injection into a settled thread and 12.5/5.20 forbid an affordance
	// that looks like it can address something it cannot — so the group names
	// its own limit rather than leaving the reader to discover it after
	// sending.
	historyGroupNote = "about, not to"
	// noMatch is what an open filter says when nothing matches. An empty list
	// would read as a rendering bug; a sentence reads as an answer.
	noMatch = "no task by that name"

	// The dispatch chip (5.22's registry-fix checklist). The chords are named
	// exactly as the checklist writes them, because the chip's whole job is to
	// teach the accelerator that 5.18 otherwise leaves invisible.
	chipStay   = "↵ stay"
	chipFollow = "⌃↵ follow"
	// chipTo and chipAbout are the two things a dispatch can be. The words are
	// the difference between speaking INTO a thread and speaking ABOUT one, and
	// they are never interchangeable (5.18).
	chipTo    = "→ "
	chipAbout = "about "

	// hintSep is the telemetry separator (5.17).
	hintSep = " " + tokens.GlyphSeparator + " "
	// hintIndent aligns chrome under the draft's TEXT rather than under its
	// prompt glyph — the same two cells the meta strip indents by.
	hintIndent = "  "
)

// hintRows returns the rows drawn under the draft, at most budget of them.
//
// It returns nil — and, before that, does not so much as look at the draft —
// for a composer built without [Options.Targets]. That is the byte-identity
// guarantee: no targets, no extra rows, no arithmetic done differently, so
// Render's output is what it was before this file existed.
func (m *Model) hintRows(sty *tokens.Styler, width, budget int) []string {
	if budget <= 0 || width <= 0 || m.targets == nil {
		return nil
	}
	if m.filter.open {
		return m.filterRows(sty, width, budget)
	}
	if mn, ok := m.firstMention(); ok {
		if row := m.chipRow(sty, mn.target, width); row != "" {
			return []string{row}
		}
	}
	return nil
}

// -- the candidate list -------------------------------------------------------

// hintItem is one drawable row of the filter: a hit, or the history group's
// heading. The heading is an item rather than a special case at draw time so
// the scroll window can count it — a group heading that scrolled away would
// leave settled rows looking like live ones, which is the one confusion this
// list may not create.
type hintItem struct {
	hit    int
	header bool
}

func (m *Model) filterRows(sty *tokens.Styler, width, budget int) []string {
	f := &m.filter
	if budget > maxFilterRows {
		budget = maxFilterRows
	}
	if len(f.hits) == 0 {
		return []string{m.plainRow(sty, noMatch, width)}
	}

	items := make([]hintItem, 0, len(f.hits)+1)
	headed := false
	for i, h := range f.hits {
		if !headed && int(h.idx) >= f.live {
			items = append(items, hintItem{header: true})
			headed = true
		}
		items = append(items, hintItem{hit: i})
	}

	// Scroll so the highlighted row is always on screen, tail-anchored the way
	// the draft itself is.
	selAt := 0
	for i, it := range items {
		if !it.header && it.hit == f.sel {
			selAt = i
			break
		}
	}
	start := 0
	if selAt >= budget {
		start = selAt - budget + 1
	}
	end := start + budget
	if end > len(items) {
		end = len(items)
	}

	pos := make([]int32, 0, 16)
	rows := make([]string, 0, end-start)
	for _, it := range items[start:end] {
		if it.header {
			rows = append(rows, m.groupRow(sty, width))
			continue
		}
		var row string
		row, pos = m.candidateRow(sty, f.rows[f.hits[it.hit].idx], it.hit == f.sel, width, pos)
		rows = append(rows, row)
	}
	return rows
}

// candidateRow draws one target: a selection accent in its identity hue, its
// state glyph, its word with the typed characters lit, and as much of its title
// as the row has left.
func (m *Model) candidateRow(sty *tokens.Styler, r filterRow, selected bool, width int, pos []int32) (string, []int32) {
	l := hintLine{sty: sty, max: width}
	glyph, glyphTok := r.target.glyphToken()

	accent := " "
	accentTok := tokens.TextTertiary
	if selected {
		// 5.16 makes selection a background band; this list is at most six rows
		// inside another pane's rectangle, so it carries the same idea in the
		// form 5.21 gives for grouping a card's lines — a left accent rail in
		// the identity hue. The row keeps its own tiers either way, which is
		// the property that mattered.
		accent = tokens.GlyphAccentRail
		accentTok = r.target.hue()
		if r.target.Settled {
			accentTok = tokens.TextSecondary
		}
	}
	l.add(accent, accentTok)
	l.add(glyph, glyphTok)
	l.add(" ", tokens.TextTertiary)

	pos = pos[:0]
	pos = appendPositions(pos, r.lowerWord, m.filter.query)
	l.addMatched(r.target.Word, r.lowerWord, pos, r.target.wordToken())

	if r.target.Title != "" && l.room() > 3 {
		l.add("  ", tokens.TextTertiary)
		l.addClipped(r.target.Title, tokens.TextTertiary)
	}
	return l.String(), pos
}

// groupRow draws the dim `history` heading, with its own limit spelled out when
// the row is wide enough to say it.
func (m *Model) groupRow(sty *tokens.Styler, width int) string {
	l := hintLine{sty: sty, max: width}
	l.add(hintIndent, tokens.TextTertiary)
	l.add(historyGroup, tokens.TextTertiary)
	if l.room() >= len(hintSep)+len(historyGroupNote) {
		l.add(hintSep, tokens.TextTertiary)
		l.add(historyGroupNote, tokens.TextTertiary)
	}
	return l.String()
}

// plainRow draws one dim indented sentence.
func (m *Model) plainRow(sty *tokens.Styler, text string, width int) string {
	l := hintLine{sty: sty, max: width}
	l.add(hintIndent, tokens.TextTertiary)
	l.addClipped(text, tokens.TextTertiary)
	return l.String()
}

// -- the dispatch chip --------------------------------------------------------

// chipRow draws 5.22's dispatch chip: once a mention token exists, the power
// chord stops being invisible.
//
// It shortens rather than wraps, and the order it sheds cells in is the order
// of what the reader can least afford to lose: the two chords are the thing the
// checklist exists to teach, so the destination goes first, then the follow
// chord, then — on a terminal too narrow for four words — nothing at all,
// because a chip cut mid-chord would teach a keystroke that does not exist.
func (m *Model) chipRow(sty *tokens.Styler, t Target, width int) string {
	lead, leadTok := chipTo+t.Word, t.hue()
	if t.Settled {
		lead, leadTok = chipAbout+t.Word, tokens.TextTertiary
	}
	indent := len(hintIndent)
	full := indent + ansi.StringWidth(lead) + ansi.StringWidth(hintSep) +
		ansi.StringWidth(chipStay) + ansi.StringWidth(hintSep) + ansi.StringWidth(chipFollow)
	both := indent + ansi.StringWidth(chipStay) + ansi.StringWidth(hintSep) + ansi.StringWidth(chipFollow)
	stay := indent + ansi.StringWidth(chipStay)

	l := hintLine{sty: sty, max: width}
	switch {
	case width >= full:
		l.add(hintIndent, tokens.TextTertiary)
		l.add(lead, leadTok)
		l.add(hintSep, tokens.TextTertiary)
		l.add(chipStay, tokens.TextTertiary)
		l.add(hintSep, tokens.TextTertiary)
		l.add(chipFollow, tokens.TextTertiary)
	case width >= both:
		l.add(hintIndent, tokens.TextTertiary)
		l.add(chipStay, tokens.TextTertiary)
		l.add(hintSep, tokens.TextTertiary)
		l.add(chipFollow, tokens.TextTertiary)
	case width >= stay:
		l.add(hintIndent, tokens.TextTertiary)
		l.add(chipStay, tokens.TextTertiary)
	default:
		return ""
	}
	return l.String()
}

// -- line assembly ------------------------------------------------------------

// hintLine accumulates painted spans against a cell budget, so a row is
// impossible to overflow by construction rather than by care, and so nothing
// here needs a second truncation pass to be safe.
type hintLine struct {
	b   strings.Builder
	sty *tokens.Styler
	w   int
	max int
}

// room is how many cells are left.
func (l *hintLine) room() int {
	if l.w >= l.max {
		return 0
	}
	return l.max - l.w
}

// add appends text, dropping it entirely if it does not fit. Dropping rather
// than cutting is right for the fixed cells this file adds — a glyph, a
// separator, a chord — where half a cell means nothing.
func (l *hintLine) add(text string, tok tokens.Token) {
	if text == "" {
		return
	}
	w := ansi.StringWidth(text)
	if w > l.room() {
		return
	}
	l.b.WriteString(paint(l.sty, text, tok))
	l.w += w
}

// addClipped appends text cut to whatever room is left, marked with an
// ellipsis. It is for the one field that is prose and can honestly lose its
// tail: a title.
func (l *hintLine) addClipped(text string, tok tokens.Token) {
	room := l.room()
	if room <= 0 || text == "" {
		return
	}
	if ansi.StringWidth(text) > room {
		text = ansi.Truncate(text, room, "…")
		if text == "" {
			return
		}
	}
	l.b.WriteString(paint(l.sty, text, tok))
	l.w += ansi.StringWidth(text)
}

// addMatched appends text with the bytes at pos painted one tier brighter than
// base — the fzf highlight of 5.18/5.21, and the reason a user watching the
// list narrow can see WHY each survivor survived.
//
// pos holds byte offsets into text's lowercase form, which [lower] guarantees
// is the same length as text, so the offsets index text directly. Each offset
// is widened to its whole rune before painting: highlighting one byte of a
// multi-byte character emits an escape into the middle of it, which is a broken
// cell rather than a bright one. An offset past the room left is simply never
// reached, so a clipped word highlights exactly the letters still on screen.
func (l *hintLine) addMatched(text, lowerText string, pos []int32, base tokens.Token) {
	if text == "" {
		return
	}
	if len(pos) == 0 || len(lowerText) != len(text) {
		l.addClipped(text, base)
		return
	}
	bright := tokens.Promote(base)
	cursor := 0
	for _, p := range pos {
		at := int(p)
		if at < cursor || at >= len(text) {
			continue
		}
		if !utf8.RuneStart(text[at]) {
			continue
		}
		_, size := utf8.DecodeRuneInString(text[at:])
		if size <= 0 {
			continue
		}
		if at > cursor {
			l.add(text[cursor:at], base)
		}
		l.add(text[at:at+size], bright)
		cursor = at + size
	}
	if cursor < len(text) {
		l.addClipped(text[cursor:], base)
	}
}

// String is the assembled row.
func (l *hintLine) String() string { return l.b.String() }
