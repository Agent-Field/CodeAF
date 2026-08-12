package composer

import (
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The slash line (5.22 rule 3): typing `/` on an empty draft filters the ONE
// command catalog inline, with the same rows and the same teaching format the
// ctrl+k palette and the `?` sheet draw.
//
// # It is a view, not a second list
//
// This file holds no commands. The wiring supplies them through
// [Options.Commands] and performs them through [Options.OnCommand], exactly as
// it supplies `@` targets and performs dispatches — so the catalog stays in the
// one place that owns it (internal/registry, read by internal/tui2/chat) and
// the slash surface can no more drift from the palette than the palette can
// drift from itself. 5.22's kill list names "the 17-command slash table as a
// parallel system" outright; a package that could enumerate a command is a
// package that could grow that table.
//
// # It is the `@` filter's shape, on purpose
//
// Same lens-not-buffer discipline (the `/` and the needle are ordinary draft
// text; the filter holds an index), same six-row budget above the draft, same
// tab/enter completion, same esc that forgets an index and leaves every
// character where it was, same scoring constants. A reader who has used one
// has used the other, and a maintainer reading one file has read both.
//
// # Where it opens, and why nowhere else
//
// Only a `/` typed as the FIRST character of an empty draft. A slash inside a
// sentence is a slash — a path, a fraction, a date — and a popup that appeared
// over "and/or" would be the surface interrupting prose to offer commands
// nobody asked for. The rule is one comparison, it is the rule every chat
// surface uses, and it means the affordance is never in the way.

// maxSlashQuery bounds the needle. A slash alias is one word; past this the
// reader is writing a sentence that happens to begin with a slash, and the
// filter stands down rather than arguing.
const maxSlashQuery = 24

// slashLead is the character that opens the line, named once.
const slashLead = '/'

// Command is one catalog row as the slash line needs it. It is deliberately not
// a registry type: this package never imports the catalog it draws.
type Command struct {
	// ID is handed back to [Options.OnCommand] on completion, untouched. It is
	// the registry entry id, and this package treats it as an opaque string —
	// which is what keeps the executor single.
	ID string
	// Word is the alias typed after the slash, WITHOUT the slash.
	Word string
	// Title is the one-line description, drawn beside the alias in the same
	// verb-then-sentence format the palette uses.
	Title string
	// Key is the accelerator this row also answers to, right-aligned so the
	// list TEACHES the chord (5.22 rule 2: "the palette teaches the
	// accelerators through use"). Empty draws nothing.
	Key string
	// Reason is why this room cannot run the row (5.20 rule 3). A non-empty
	// Reason makes the row refuse completion and draw the sentence where the
	// accelerator would have been — the same bargain the palette strikes, so a
	// row cannot look runnable on one surface and refused on another.
	Reason string
}

// runnable reports that completing this row would do something.
func (c Command) runnable() bool { return c.Reason == "" && c.ID != "" }

// slashRow is one candidate with its lowercase fields precomputed at open.
type slashRow struct {
	command    Command
	lowerWord  string
	lowerTitle string
}

// slashFilter is the open slash line's whole state. Every field mirrors
// [mentionFilter]'s for the reason given at the top of this file.
type slashFilter struct {
	open bool
	// at is the rune index of the '/' that opened this session — always 0
	// today, and kept as a field anyway so the needle arithmetic below reads
	// the same as the `@` filter's rather than hiding a constant in it.
	at    int
	rows  []slashRow
	hits  []filterHit
	sel   int
	buf   []byte
	query string
}

func (f *slashFilter) hasSelection() bool { return f.sel >= 0 && f.sel < len(f.hits) }

func (f *slashFilter) selected() (Command, bool) {
	if !f.hasSelection() {
		return Command{}, false
	}
	return f.rows[f.hits[f.sel].idx].command, true
}

// -- opening, closing, staying honest ----------------------------------------

// slashBoundary reports that the rune at index at is a '/' that may open the
// line: the first character of the draft, and the only one so far.
//
// "The only one so far" is what keeps the popup out of a sentence the reader is
// part way through: a '/' typed at the head of an existing draft is editing
// prose, not reaching for a command.
func slashBoundary(value []rune, at int) bool {
	return at == 0 && len(value) == 1 && value[0] == slashLead
}

// openSlashFilter starts a session. Like the `@` filter it refuses to open onto
// an empty catalog: an affordance claiming there is something to run when there
// is not is 5.20's failure, and a wiring with no Commands has opted out whole.
func (m *Model) openSlashFilter(at int) {
	if m.commands == nil {
		return
	}
	f := &m.slash
	f.rows = f.rows[:0]
	for _, c := range m.commands() {
		if c.Word == "" {
			continue
		}
		f.rows = append(f.rows, slashRow{
			command:    c,
			lowerWord:  lower(c.Word),
			lowerTitle: lower(c.Title),
		})
	}
	if len(f.rows) == 0 {
		return
	}
	f.open = true
	f.at = at
	f.sel = 0
	m.rankSlash()
}

// closeSlashFilter forgets the session. The draft keeps every character.
func (m *Model) closeSlashFilter() {
	m.slash.open = false
	m.slash.hits = m.slash.hits[:0]
	m.slash.sel = 0
}

// syncSlash re-validates the session against the draft after every edit.
//
// The session ends when its own preconditions stop holding: the '/' is gone or
// is no longer first, the cursor walked out of the needle, whitespace arrived
// (an alias has none, so a space means the reader went back to prose), or the
// needle outgrew [maxSlashQuery].
func (m *Model) syncSlash() {
	f := &m.slash
	if !f.open {
		return
	}
	if f.at != 0 || len(m.value) == 0 || m.value[0] != slashLead ||
		m.cursor <= f.at || m.cursor > len(m.value) {
		m.closeSlashFilter()
		return
	}
	if m.cursor-f.at-1 > maxSlashQuery {
		m.closeSlashFilter()
		return
	}
	for i := f.at + 1; i < m.cursor; i++ {
		if unicode.IsSpace(m.value[i]) {
			m.closeSlashFilter()
			return
		}
	}
	m.rankSlash()
}

// -- ranking ------------------------------------------------------------------

// rankSlash re-scores every row against the needle, best first, stable on ties.
//
// There is no group split here, unlike the `@` filter's live/settled halves:
// the catalog has one section on this surface, and inventing a second grouping
// would be the slash line disagreeing with the palette about the shape of one
// list. A refused row is not filtered out either — 5.20 rule 3 wants it visible
// WITH its reason, not hidden.
func (m *Model) rankSlash() {
	f := &m.slash
	needle := f.needle(m.value, m.cursor)
	f.query = needle
	f.hits = f.hits[:0]
	for i := range f.rows {
		r := &f.rows[i]
		wordScore, wordOK := score(r.lowerWord, needle)
		titleScore, titleOK := score(r.lowerTitle, needle)
		switch {
		case wordOK && titleOK:
			f.hits = append(f.hits, filterHit{idx: int32(i), score: int32(min(wordScore, titleScore))})
		case wordOK:
			f.hits = append(f.hits, filterHit{idx: int32(i), score: int32(wordScore)})
		case titleOK:
			f.hits = append(f.hits, filterHit{idx: int32(i), score: int32(titleScore)})
		}
	}
	sortHits(f.hits)
	if f.sel >= len(f.hits) {
		f.sel = len(f.hits) - 1
	}
	if f.sel < 0 {
		f.sel = 0
	}
}

// needle builds the lowercased query between the '/' and the cursor, reusing
// the filter's byte scratch.
func (f *slashFilter) needle(value []rune, cursor int) string {
	f.buf = f.buf[:0]
	for i := f.at + 1; i < cursor && i < len(value); i++ {
		r := value[i]
		if r < utf8.RuneSelf {
			b := byte(r)
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			f.buf = append(f.buf, b)
			continue
		}
		f.buf = utf8.AppendRune(f.buf, unicode.ToLower(r))
	}
	return string(f.buf)
}

// -- the keyboard while the line is open --------------------------------------

// slashKey handles the keys an open slash line owns, claiming as little as it
// can — the same restraint [Model.filterKey] shows, and for the same reason: a
// key this line swallows is a key the draft, the ladder or the shell has lost.
func (m *Model) slashKey(s string) (tea.Cmd, bool) {
	switch s {
	case "esc":
		// The draft is untouched. Closing the line is forgetting an index, and
		// the words the reader typed are exactly where they were — which is
		// what lets 8.2.21's esc ladder stay one ladder.
		m.closeSlashFilter()
		return nil, true
	case "up":
		m.moveSlashSelection(-1)
		return nil, true
	case "down":
		m.moveSlashSelection(1)
		return nil, true
	case "tab":
		return m.completeSlash(), true
	}
	if s == m.sendKey || s == "enter" {
		if m.slash.hasSelection() {
			return m.completeSlash(), true
		}
	}
	return nil, false
}

func (m *Model) moveSlashSelection(delta int) {
	f := &m.slash
	next := f.sel + delta
	if next < 0 {
		next = 0
	}
	if next >= len(f.hits) {
		next = len(f.hits) - 1
	}
	if next < 0 {
		next = 0
	}
	f.sel = next
}

// completeSlash runs the highlighted row and clears the draft.
//
// CLEARING is the difference between this and [Model.completeMention], and it
// is the difference between the two grammars: a mention is part of a sentence
// the reader is still writing, and a command is the whole of what they meant.
// Leaving "/settings" in the draft after opening settings would be the surface
// keeping a receipt for an act it already performed, and the next enter would
// send it as prose.
//
// A refused row runs nothing and does not close: the reader asked for something
// this room cannot do, and the honest answer is the reason still on screen
// beside it — the same answer the palette gives a disabled row.
func (m *Model) completeSlash() tea.Cmd {
	command, ok := m.slash.selected()
	if !ok {
		return nil
	}
	if !command.runnable() {
		return nil
	}
	m.value = m.value[:0]
	m.cursor = 0
	m.historyStep = 0
	// A performed command is a draft that is over, so the ghost hints are owed
	// again — the same rule a send follows ([Model.reset]).
	m.typed = false
	m.closeSlashFilter()
	m.afterEdit()
	if m.onCommand == nil {
		return nil
	}
	return m.onCommand(command.ID)
}

// -- drawing -------------------------------------------------------------------

// slashRows draws the candidate list above the draft: alias, description, and
// the accelerator or the reason at the right edge.
//
// The rows are in UPWARD display order — the best match last, one row above the
// prompt (see hint.go) — so the index arithmetic runs backwards through the
// hits. [slashDisplay] is the single conversion between the two, and the pointer
// reads it too.
//
// The right-hand cell is the teaching half of 5.22 rule 2 — "every row = verb +
// description + its key/slash equivalent right-aligned" — and it is where a
// refusal goes too, because a row that cannot run has nothing to teach and the
// reader needs the sentence more than the chord.
func (m *Model) slashRows(sty *tokens.Styler, width, budget int) []string {
	f := &m.slash
	if budget > maxFilterRows {
		budget = maxFilterRows
	}
	if budget <= 0 {
		return nil
	}
	if len(f.hits) == 0 {
		return []string{plainRow(sty, noSlashMatch, width)}
	}
	start, end := upwardWindow(len(f.hits), slashDisplay(f, f.sel), budget)
	pos := make([]int32, 0, 16)
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		hit := slashDisplay(f, i)
		var row string
		row, pos = m.slashCandidate(sty, f.rows[f.hits[hit].idx], hit == f.sel, width, pos)
		rows = append(rows, row)
	}
	return rows
}

// slashDisplay converts between a hit's rank and its display row, in either
// direction — the mapping is its own inverse, which is what a reversal is. It
// exists so the paint and the pointer cannot disagree about which row is which
// candidate.
func slashDisplay(f *slashFilter, i int) int { return len(f.hits) - 1 - i }

// slashCandidate draws one row.
func (m *Model) slashCandidate(sty *tokens.Styler, r slashRow, selected bool, width int, pos []int32) (string, []int32) {
	l := hintLine{sty: sty, max: width}

	accent, accentTok := " ", tokens.TextTertiary
	if selected {
		// The same left accent rail the `@` list uses for the same reason
		// (5.21): six rows inside another pane's rectangle cannot afford a
		// background band, and the row keeps its own tiers either way.
		accent, accentTok = tokens.GlyphAccentRail, tokens.TextSecondary
	}
	l.add(accent, accentTok)

	// A refused row recedes a tier whole. It is still readable, still says what
	// it is, and no longer reads as something to press.
	word, title := tokens.TextPrimary, tokens.TextTertiary
	if !r.command.runnable() {
		word, title = tokens.TextTertiary, tokens.TextTertiary
	}

	l.add(string(slashLead), tokens.TextTertiary)
	pos = pos[:0]
	pos = appendPositions(pos, r.lowerWord, m.slash.query)
	l.addMatched(r.command.Word, r.lowerWord, pos, word)

	// The right-hand cell is measured before the description is drawn, so the
	// description gives way to it rather than pushing it off the row: the chord
	// is what the list is teaching, and a teaching cell that only appears on
	// wide terminals teaches nobody on an 80-column one.
	right := r.command.Reason
	rightTok := tokens.TextTertiary
	if right == "" {
		right = r.command.Key
	}
	reserved := 0
	if right != "" {
		reserved = displayWidth(right) + 1
	}
	if r.command.Title != "" && l.room() > reserved+3 {
		l.add("  ", tokens.TextTertiary)
		l.addClippedWithin(r.command.Title, title, l.room()-reserved)
	}
	if right != "" && l.room() >= reserved {
		l.pad(l.room() - displayWidth(right))
		l.add(right, rightTok)
	}
	return l.String(), pos
}

// HintRows is how many rows the composer's open inline completion wants under
// the draft right now, and zero when nothing is open.
//
// It is exported because the REGION cannot see it any other way: the completion
// draws inside the composer's own rectangle (hint.go), and that rectangle is
// budgeted by the layout for a draft and two strips. So the wiring asks this,
// asks the shell for that many extra rows, and the list has somewhere to be —
// see [tui2.Shell.GrowComposer], which treats the number as a request the
// layout may refuse rather than as a size.
//
// It counts only what a filter session would draw. Attachment chips and the
// dispatch chip are steady chrome that the metric table already pays for; a
// region that grew for them would move the transcript every time a path was
// typed, which is 5.21's dancing.
func (m *Model) HintRows() int {
	// THE CHIPS COUNT NOW. They did not, and the comment on [Model.GrowRows]
	// said why: they were "steady chrome the metric table already pays for",
	// which was true while that table reserved four rows for this region. §7 cut
	// the reservation to one — the place line and the meta strip are gone — so a
	// chip nobody grew for is a chip drawn over the draft or not drawn at all,
	// which is exactly what an attached file looked like on the first frame after
	// the hug landed.
	//
	// It is still not 5.21's dancing: a chip appears when a path is captured or a
	// mention is typed, which is a discrete act the reader performed, not a
	// per-keystroke reflow. The region grows once, on that act, and gives the row
	// back when the send takes the chip with it.
	rows := m.chipRows()
	switch {
	case m.slash.open:
		return rows + capRows(max(len(m.slash.hits), 1))
	case m.filter.open:
		list := max(len(m.filter.hits), 1)
		// The `history` heading is a ROW, and a heading the region did not grow
		// for is a heading that scrolls off the top — which would leave settled
		// rows looking like live ones, the one confusion this list may not
		// create (hint.go).
		if n := len(m.filter.hits); n > 0 && int(m.filter.hits[n-1].idx) >= m.filter.live {
			list++
		}
		return rows + capRows(list)
	}
	return rows
}

// chipRows is how many rows the steady chrome above the draft occupies: the
// attachment chips (capped by [attachChipMax], with a fold row when there are
// more), and the dispatch chip a mention token brings with it.
//
// It restates [Model.chromeRows]'s own reservations rather than calling it,
// because that function needs a styler and a budget and this question is asked
// before either exists — the region is asking how tall to BE. The two agree
// because they count the same three things in the same order, and the width
// sweep in render_test.go is what keeps them agreeing.
func (m *Model) chipRows() int {
	rows := 0
	if m.attach != nil && len(m.attachments) > 0 {
		rows = min(len(m.attachments), attachChipMax)
	}
	if m.targets != nil && !m.slash.open && !m.filter.open {
		if _, ok := m.firstMention(); ok {
			rows++
		}
	}
	return rows
}

func capRows(n int) int {
	if n > maxFilterRows {
		return maxFilterRows
	}
	return n
}

// -- the pointer ---------------------------------------------------------------

// candidateAt turns a pane-local row into the hit it draws, and reports whether
// the row is a candidate at all.
//
// The geometry is DERIVED, from the very plan Render paints at the same size
// (render.go). Nothing is recorded during a paint (Part 2's anti-pattern 14),
// and there is no arrangement of width, draft and attachment chips under which
// the answer can disagree with the picture, because it IS the picture's
// arithmetic. The list is the HEAD of the chrome, which is what makes the row
// span a count rather than a search: rows [0, list) are candidates.
func (m *Model) candidateAt(width, height, y int) (index int, onList, ok bool) {
	if !m.slash.open || width <= 0 || height <= 0 || y < 0 {
		return 0, false, false
	}
	p := m.plan(m.activeStyler(), width, height)
	if y >= p.above.list {
		return 0, false, false
	}
	if len(m.slash.hits) == 0 {
		// The no-match sentence is a row of the list and not a candidate.
		return 0, true, false
	}
	start, _ := upwardWindow(len(m.slash.hits), slashDisplay(&m.slash, m.slash.sel), p.above.list)
	index = slashDisplay(&m.slash, start+y)
	if index < 0 || index >= len(m.slash.hits) {
		return 0, true, false
	}
	return index, true, true
}

// ClickHint performs the candidate a click landed on, and reports whether the
// click was on the list at all — including the no-match sentence, which is a
// row of this surface that chooses nothing. Consuming it is what keeps the
// caret from jumping to wherever the pointer happened to be.
//
// The x is not consulted: a candidate is a whole row of a list, and asking the
// reader to hit the word rather than the row would be a target narrower than
// the thing it stands for.
func (m *Model) ClickHint(width, height, y int) (tea.Cmd, bool) {
	index, onList, ok := m.candidateAt(width, height, y)
	if !ok {
		return nil, onList
	}
	m.slash.sel = index
	return m.completeSlash(), true
}

// HoverHint moves the highlight to the candidate under the pointer, and reports
// whether the frame moved. It selects and never completes: pointing at a row is
// not choosing it (5.14).
func (m *Model) HoverHint(width, height, y int) bool {
	index, _, ok := m.candidateAt(width, height, y)
	if !ok || index == m.slash.sel {
		return false
	}
	m.slash.sel = index
	return true
}

// noSlashMatch is what an open line says when nothing matches. A sentence reads
// as an answer; an empty list reads as a rendering bug.
const noSlashMatch = "no command by that name"
