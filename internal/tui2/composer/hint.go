package composer

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The composer's own chrome ABOVE the draft: the open `@` filter's candidate
// list, the attachment chips, and — once a mention token exists — the dispatch
// chip.
//
// All of it lives above the draft rows and below whatever the wiring welds to
// the region's top edge (internal/tui2/chat draws a bounded HUD above and keeps
// a blank padding row below; it hands this package the rows in between). Above
// rather than below is 8's law and plain geometry: this region is welded to the bottom of
// the frame, so downward is the status line and there is nothing to open into.
// The prompt does not move when a list opens — the REGION grows upward instead
// (see [Model.GrowRows]), which is what keeps 5.21's width-stability promise in
// the small: the row you are typing on stays exactly where it was.
//
// Order inside the block is relevance, nearest the draft first. The list is
// furthest up, the chips sit against the prompt, and the highlighted candidate
// is one row above the words it will complete — the shortest trip the eye can
// make to the thing it is about to choose.
//
// None of it ever takes the draft's own rows. A composer that hid what you were
// writing to show you what you could address would have the priority exactly
// backwards — see [Model.plan], where the draft is served first.

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

	// glyphEnter is THE enter keycap, decided here and spelled nowhere else in
	// this package. Two characters were in play across the tree — `↵` (U+21B5)
	// and `⏎` (U+23CE) — and they are one meaning wearing two shapes, which 5.17
	// does not allow. `↵` wins: it is what every keycap on this surface already
	// wore (the footer's accelerator words, this chip), and both measure one cell
	// under both rulers so the choice is about vocabulary rather than metrics.
	//
	// `⏎` is NOT the loser of that contest, because in the one other place it
	// appears it is not a keycap at all: internal/tui2/chat's trace reader
	// substitutes it for a newline so one journal record stays one line. That is
	// a transport marker for a character, not the name of a key, and giving it
	// this constant's meaning would be the conflation rather than the fix.
	//
	// REQUESTED SLOT — internal/tui2/tokens: this wants to be a vocabulary slot
	// (`GEnter`, plain `↵`, nf-md/nf-fa keyboard-return on the NF side) so the
	// glyph tier can draw it and the parity golden can measure it. It is a
	// constant here because this lane does not own that package; the day the slot
	// lands, this line becomes a [tokens.Styler.Glyph] call and nothing else in
	// the file moves.
	glyphEnter = "↵"
	// glyphCtrl is the control modifier's cap, on the same rule and with the same
	// request attached.
	glyphCtrl = "⌃"

	// The dispatch chip (5.22's registry-fix checklist). The chords are named
	// exactly as the checklist writes them, because the chip's whole job is to
	// teach the accelerator that 5.18 otherwise leaves invisible.
	// VERB FIRST, KEY AFTER (§16's verb·key chip, internal/tui2/keychip). These
	// two read `↵ stay` and `⌃↵ follow` until this wave, which is key-first —
	// the `esc close` shape the law was written against, with a keycap standing
	// in for the word `esc`. The words are the same words; the eye now lands on
	// the one that names the act.
	//
	// They are STRINGS rather than [keychip.Line] spans because this row paints
	// each chip as one run at one tier: a dispatch chip is a single affordance
	// the composer either offers or does not, and splitting it across two tiers
	// mid-row would make the key look like a separate cell of the hint line.
	chipStay   = "stay " + glyphEnter
	chipFollow = "follow " + glyphCtrl + glyphEnter
	// chipTo and chipAbout are the two things a dispatch can be. The words are
	// the difference between speaking INTO a thread and speaking ABOUT one, and
	// they are never interchangeable (5.18).
	chipTo    = "→ "
	chipAbout = "about "

	// hintSep is the telemetry separator (5.17).
	hintSep = " " + tokens.GlyphSeparator + " "
)

// hintIndent aligns chrome with the draft's TEXT rather than with its gutter.
// It is [promptGutter] and not a number this file gets to pick: a candidate the
// reader is about to insert has to stand in the column the words it joins stand
// in, and the two cells to the left of that column — the hug's state edge and
// the prompt — are markers hanging outside the text, exactly as the delivery
// card's rail hangs outside its title.
//
// It used to be [tokens.LensIndent], the room's one left edge, and it stopped
// being that when the edge cell was added: the room's edge is where the BAR row
// under the composer still opens, one row down and one plane back, and the two
// were only ever the same number by coincidence.
var hintIndent = strings.Repeat(" ", promptGutter)

// chrome is the block drawn above the draft: its rows top to bottom, and how
// many of the leading ones belong to an open completion list.
//
// The count is carried rather than re-derived because the pointer needs it: a
// click on a candidate has to know which rows are candidates, and asking the
// list a second time how tall it would have been is the second spelling of one
// number that Part 2's anti-pattern 14 is about.
type chrome struct {
	rows []string
	// list is how many of rows, from the top, are the open completion's.
	list int
}

// chromeRows returns the rows drawn above the draft, at most budget of them.
//
// It returns the zero chrome — and, before that, does not so much as look at
// the draft — for a composer built without [Options.Targets], [Options.Attach]
// and [Options.Commands]. That is the byte-identity guarantee: no wiring, no
// extra rows, no arithmetic done differently, so Render's output is what it was
// before any of this existed.
func (m *Model) chromeRows(sty *tokens.Styler, width, budget int) chrome {
	if budget <= 0 || width <= 0 {
		return chrome{}
	}
	// Attachment chips sit closest to the draft and stay there. They are the
	// only chrome here that describes what the SEND will carry rather than what
	// the cursor is doing, so they are the one row that must not move when a
	// filter opens above them — a chip that jumped a row every time an '@' was
	// typed would be 5.21's dancing, in the small.
	chips := m.attachRows(sty, width, budget)
	budget -= len(chips)

	var list []string
	switch {
	case budget <= 0:
	// The slash line, checked before the `@` opt-out because the two grammars
	// are independent: a wiring may supply commands and no mention targets, and
	// a composer with neither renders exactly the bytes it rendered before
	// either existed.
	case m.slash.open:
		list = m.slashRows(sty, width, budget)
	case m.targets == nil:
	case m.filter.open:
		list = m.filterRows(sty, width, budget)
	default:
		if mn, ok := m.firstMention(); ok {
			// The dispatch chip is the LAST chip: it teaches the chord that sends
			// this very draft, so it is the row the prompt is directly under.
			if row := m.chipRow(sty, mn.target, width); row != "" {
				chips = append(chips, row)
			}
		}
	}
	if len(list) == 0 {
		return chrome{rows: chips}
	}
	return chrome{rows: append(list, chips...), list: len(list)}
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

// upwardWindow is which slice of a list drawn UPWARD is on screen: the window
// sits as close to the draft as it can — the END of the list, where the best
// matches are — and climbs only far enough to keep the selection visible.
//
// It is the mirror image of the draft's own tail anchoring, and for the same
// reason: the row that matters is the one nearest the prompt, so that is the
// row that never scrolls away.
func upwardWindow(total, sel, budget int) (start, end int) {
	if total <= 0 || budget <= 0 {
		return 0, 0
	}
	if budget > total {
		budget = total
	}
	start = total - budget
	if sel >= 0 && sel < start {
		start = sel
	}
	return start, start + budget
}

// filterItems is the candidate list in display order — least relevant at the
// top, the best match LAST, one row above the draft — and where the highlighted
// row sits in it.
//
// The `history` heading lands at the very top, because settled targets rank
// after live ones and the reversal therefore puts the whole settled group above
// the whole live group. The heading still names the rows under it (5.15's "one
// faint lowercase word may announce a section"), and it is an item rather than a
// draw-time special case so the scroll window can count it — a heading that
// scrolled away would leave settled rows looking like live ones, which is the
// one confusion this list may not create.
func (m *Model) filterItems() ([]hintItem, int) {
	f := &m.filter
	items := make([]hintItem, 0, len(f.hits)+1)
	selAt := 0
	for i := len(f.hits) - 1; i >= 0; i-- {
		if len(items) == 0 && int(f.hits[i].idx) >= f.live {
			items = append(items, hintItem{header: true})
		}
		if i == f.sel {
			selAt = len(items)
		}
		items = append(items, hintItem{hit: i})
	}
	return items, selAt
}

func (m *Model) filterRows(sty *tokens.Styler, width, budget int) []string {
	f := &m.filter
	if budget > maxFilterRows {
		budget = maxFilterRows
	}
	if budget <= 0 {
		return nil
	}
	if len(f.hits) == 0 {
		return []string{plainRow(sty, noMatch, width)}
	}

	items, selAt := m.filterItems()
	start, end := upwardWindow(len(items), selAt, budget)

	pos := make([]int32, 0, 16)
	rows := make([]string, 0, end-start)
	for i, it := range items[start:end] {
		// The heading is PINNED to the top of the window. It lives at the head of
		// the list, so a window that climbed past it would leave settled rows
		// looking like live ones — the one confusion this list may not create —
		// and a heading nobody can see is a heading that is not doing its job.
		if it.header || (i == 0 && m.settledItem(it)) {
			rows = append(rows, groupRow(sty, width))
			continue
		}
		var row string
		row, pos = m.candidateRow(sty, f.rows[f.hits[it.hit].idx], it.hit == f.sel, width, pos)
		rows = append(rows, row)
	}
	return rows
}

// settledItem reports that an item draws a target from the `history` group.
func (m *Model) settledItem(it hintItem) bool {
	f := &m.filter
	return !it.header && it.hit >= 0 && it.hit < len(f.hits) && int(f.hits[it.hit].idx) >= f.live
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
func groupRow(sty *tokens.Styler, width int) string {
	l := hintLine{sty: sty, max: width}
	l.add(hintIndent, tokens.TextTertiary)
	l.add(historyGroup, tokens.TextTertiary)
	if l.room() >= ansi.StringWidth(hintSep)+ansi.StringWidth(historyGroupNote) {
		l.add(hintSep, tokens.TextTertiary)
		l.add(historyGroupNote, tokens.TextTertiary)
	}
	return l.String()
}

// plainRow draws one dim indented sentence.
func plainRow(sty *tokens.Styler, text string, width int) string {
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
	// The destination is written in the target's own TIER, never in its identity
	// pastel. §18.3 states the rule the whole palette rests on — identity never
	// colours running text; it rides a glyph, a rail or a ground — and this row
	// was breaking it in the one place that made the breach look deliberate: a
	// word coloured lime next to two chords coloured chrome reads as a semantic
	// distinction, and the reader spends a moment deciding what lime means before
	// discovering it means nothing but "this task, not that one". The GLYPH on
	// the candidate rows above already carries that hue, in the one column where
	// a hue is allowed to be an identity rather than a state.
	//
	// [Target.wordToken] is the package's own answer to "what tier is this
	// target's word", and it is the answer the candidate list has always used —
	// so the chip and the row above it now agree by construction rather than by
	// coincidence.
	lead, leadTok := chipTo+t.Word, t.wordToken()
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
		text = ansi.Truncate(text, room, tokens.GlyphEllipsis)
		if text == "" {
			return
		}
	}
	l.b.WriteString(paint(l.sty, text, tok))
	l.w += ansi.StringWidth(text)
}

// pad advances the line by n blank cells. It is how a right-aligned cell finds
// its edge without a second pass over the row.
func (l *hintLine) pad(n int) {
	if n <= 0 {
		return
	}
	if n > l.room() {
		n = l.room()
	}
	l.b.WriteString(strings.Repeat(" ", n))
	l.w += n
}

// addClippedWithin is addClipped against a budget SMALLER than the room left,
// so a cell reserved further right survives. Passing the budget rather than
// letting the caller truncate first is what keeps the ellipsis decision in the
// one place that knows the width rules.
func (l *hintLine) addClippedWithin(text string, tok tokens.Token, budget int) {
	if budget <= 0 || text == "" {
		return
	}
	if budget > l.room() {
		budget = l.room()
	}
	if ansi.StringWidth(text) > budget {
		text = ansi.Truncate(text, budget, tokens.GlyphEllipsis)
		if text == "" {
			return
		}
	}
	l.b.WriteString(paint(l.sty, text, tok))
	l.w += ansi.StringWidth(text)
}

// displayWidth is the printable width of a plain string, named here so the
// slash line does not reach past this file for the measure its rows are fitted
// against.
func displayWidth(s string) int { return ansi.StringWidth(s) }

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
