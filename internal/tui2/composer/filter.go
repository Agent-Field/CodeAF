package composer

import (
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// The inline `@` filter (5.18, 5.21's "fzf-style inline filter"): typing '@' at
// a word boundary opens an as-you-type filter over the wiring's dispatch
// targets — live tasks first, then a dim history group of settled ones — fuzzy
// over task word + title, match chars highlighted, enter/tab completing the
// mention into the draft.
//
// # The filter never owns the draft
//
// The typed '@' and everything after it are ORDINARY DRAFT TEXT. The filter is
// a lens over the run of runes between its '@' and the cursor; it holds an
// index, not a buffer. That is what makes esc cheap and honest: closing the
// filter is forgetting an index, and the words the user typed are exactly where
// they were. It is also why the composer's esc law (8.2.21) is untouched — esc
// only reaches [Model.handleEsc] when there is no filter to close first, so the
// key's meaning is always the one thing on screen that is open.
//
// # Budgets
//
// Ranking runs over every target on every keystroke and appends into slices the
// filter owns and reuses ([mentionFilter.hits], [mentionFilter.rows],
// [mentionFilter.buf]), so a steady-state keystroke costs one small string for
// the lowercased needle and nothing else. Highlighting runs only over the rows
// actually drawn — at most a screenful, and this screenful is six rows.
//
// The scoring rule is a deliberate restatement of [registry.FuzzyMatch]'s, the
// same restatement internal/tui2/palette made and for the same reason: the
// registry scores registry entries, and its ranking allocates a []Match per
// query. filter_test.go pins the agreement against the registry's own catalog,
// so the `@` filter and the ctrl+k palette can never start disagreeing about
// which row best answers a query.

// maxFilterRows bounds the filter's own rectangle. Six rows is a list you read
// at a glance; a taller one would be the composer eating the transcript to show
// candidates, which is the surface swap 8.2.8 spends a bounded HUD to avoid.
const maxFilterRows = 6

// maxMentionQuery is the longest needle the filter will carry before deciding
// the user is writing prose rather than picking a task. A `@` followed by forty
// characters with no match is not a filter session any more.
const maxMentionQuery = 40

// prefixBonus is subtracted from the score of a haystack the needle prefixes
// outright, so typing "wi" ranks "wisp-parity" ahead of a row that merely
// contains w and i somewhere. Same magnitude as the registry's and the
// palette's — the number is part of the agreement, not a local taste.
const prefixBonus = 100

// filterRow is one candidate with its lowercase fields precomputed at open, so
// the per-keystroke path never folds case.
type filterRow struct {
	target     Target
	lowerWord  string
	lowerTitle string
}

// filterHit is one surviving row: its index into rows and the score that ranked
// it. Both 32-bit so the filtered set stays compact — it is walked twice per
// frame and is the only per-keystroke state.
type filterHit struct {
	idx   int32
	score int32
}

// mentionFilter is the open filter's whole state.
type mentionFilter struct {
	open bool
	// at is the rune index of the '@' that opened this session. The needle is
	// the draft between at+1 and the cursor.
	at int
	// rows are this session's candidates, live first then settled. The split
	// point is live, so a group can be found without re-reading Settled.
	rows []filterRow
	live int
	// hits is the ranked survivor set, rebuilt in place every keystroke.
	hits []filterHit
	// sel is the index into hits of the highlighted row.
	sel int
	// buf is reused scratch for the lowercased needle's bytes.
	buf []byte
	// query is the needle the current hits were ranked against, kept so the
	// renderer can highlight the match chars without re-deriving it — Render is
	// a pure function of the model (pane.go's contract) and may not write the
	// scratch buffer the key path owns.
	query string
}

// hasSelection reports whether a row is highlighted and could be completed.
func (f *mentionFilter) hasSelection() bool { return f.sel >= 0 && f.sel < len(f.hits) }

// selected is the highlighted target.
func (f *mentionFilter) selected() (Target, bool) {
	if !f.hasSelection() {
		return Target{}, false
	}
	return f.rows[f.hits[f.sel].idx].target, true
}

// -- opening, closing, staying honest ----------------------------------------

// openMentionFilter starts a session at the '@' at rune index at. It is a no-op
// when the wiring supplies no targets, and — deliberately — also when it
// supplies an empty list: a filter that opens onto nothing is an affordance
// claiming there is something to address (5.20). The '@' stays in the draft as
// the ordinary character it is.
func (m *Model) openMentionFilter(at int) {
	if m.targets == nil {
		return
	}
	list := m.targets()
	f := &m.filter
	f.rows = f.rows[:0]
	for _, t := range list {
		if t.Word == "" || t.Settled {
			continue
		}
		f.rows = append(f.rows, newFilterRow(t))
	}
	f.live = len(f.rows)
	for _, t := range list {
		if t.Word == "" || !t.Settled {
			continue
		}
		f.rows = append(f.rows, newFilterRow(t))
	}
	if len(f.rows) == 0 {
		return
	}
	f.open = true
	f.at = at
	f.sel = 0
	m.rankMentions()
}

func newFilterRow(t Target) filterRow {
	return filterRow{target: t, lowerWord: lower(t.Word), lowerTitle: lower(t.Title)}
}

// closeMentionFilter forgets the session. The draft is not touched — see the
// package comment above.
func (m *Model) closeMentionFilter() {
	m.filter.open = false
	m.filter.hits = m.filter.hits[:0]
	m.filter.sel = 0
}

// syncFilter re-validates the open session against the draft and re-ranks it.
// Every edit and every cursor move runs through here (via [Model.afterEdit]),
// so the filter can never be showing candidates for a needle that is no longer
// on the screen.
//
// The session ends the moment its own preconditions stop holding: the '@' was
// deleted or overwritten, the cursor walked out of the needle, whitespace
// arrived (a task word has none, so a space means the user went back to
// writing prose), or the needle outgrew [maxMentionQuery].
func (m *Model) syncFilter() {
	f := &m.filter
	if !f.open {
		return
	}
	if f.at >= len(m.value) || m.value[f.at] != '@' || m.cursor <= f.at || m.cursor > len(m.value) {
		m.closeMentionFilter()
		return
	}
	if m.cursor-f.at-1 > maxMentionQuery {
		m.closeMentionFilter()
		return
	}
	for i := f.at + 1; i < m.cursor; i++ {
		if unicode.IsSpace(m.value[i]) {
			m.closeMentionFilter()
			return
		}
	}
	m.rankMentions()
}

// afterEdit is the one door every edit leaves through: mention spans are
// recomputed from the new text, then the open filter is re-validated against
// it. Both are no-ops for a composer built without [Options.Targets], which is
// what keeps that composer byte-identical to the one this package shipped
// before the `@` grammar existed.
func (m *Model) afterEdit() {
	// Attachment capture runs first because it is the one sync that EDITS the
	// draft (attach.go). Mentions and the open filter are derived from the text,
	// so they must be derived from the text as it finally stands — deriving a
	// span and then splicing the buffer under it is how a token and its
	// highlight come to disagree.
	m.syncAttachments()
	m.syncMentions()
	m.syncFilter()
	m.syncSlash()
}

// -- ranking ------------------------------------------------------------------

// rankMentions re-scores every candidate against the current needle. Survivors
// land in display order: live rows first, settled rows after, best match first
// within each group and stable on ties.
func (m *Model) rankMentions() {
	f := &m.filter
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
	// The two groups are already contiguous because rows are built that way;
	// only the order WITHIN each run has to move. Insertion sort written out
	// rather than sort.SliceStable, because that call's comparison closure is
	// one heap allocation per keystroke for a run this short.
	split := 0
	for split < len(f.hits) && int(f.hits[split].idx) < f.live {
		split++
	}
	sortHits(f.hits[:split])
	sortHits(f.hits[split:])
	if f.sel >= len(f.hits) {
		f.sel = len(f.hits) - 1
	}
	if f.sel < 0 {
		f.sel = 0
	}
}

// sortHits is a stable insertion sort by score, ascending (lower is better).
func sortHits(run []filterHit) {
	for i := 1; i < len(run); i++ {
		h := run[i]
		j := i - 1
		for j >= 0 && run[j].score > h.score {
			run[j+1] = run[j]
			j--
		}
		run[j+1] = h
	}
}

// needle builds the lowercased query from the draft between the '@' and the
// cursor, reusing the filter's byte scratch. The one allocation left is the
// string header the scorer indexes; everything else is amortized.
func (f *mentionFilter) needle(value []rune, cursor int) string {
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
		f.buf = utf8.AppendRune(f.buf, r)
	}
	if len(f.buf) == 0 {
		return ""
	}
	return string(f.buf)
}

// score is the subsequence scorer. Lower is better. needle must already be
// lowercased by [lower] or [mentionFilter.needle]; haystack must be one of a
// row's precomputed lowercase fields.
//
// Bytes, not runes, on purpose, and for the reason the palette states: a needle
// byte below 0x80 can only ever equal a haystack byte below 0x80, so an ASCII
// query can never match half a multi-byte rune, and a non-ASCII query degrades
// to an exact byte-sequence match rather than a wrong one.
func score(haystack, needle string) (int, bool) {
	if needle == "" {
		return 0, true
	}
	pos, total := 0, 0
	for i := 0; i < len(needle); i++ {
		want := needle[i]
		found := false
		for pos < len(haystack) {
			if haystack[pos] == want {
				total += pos
				pos++
				found = true
				break
			}
			pos++
		}
		if !found {
			return 0, false
		}
	}
	if len(needle) <= len(haystack) && haystack[:len(needle)] == needle {
		total -= prefixBonus
	}
	return total, true
}

// appendPositions appends the byte offsets in haystack that needle matched, in
// increasing order. It walks greedily — the same walk [score] takes — so the
// highlighted cells are exactly the cells the score was computed from, and a
// user watching the list narrow is watching the ranking happen.
//
// A position that is not a rune start is dropped rather than highlighted:
// painting one byte of a multi-byte character emits an escape into the middle
// of it, which is a broken cell rather than a bright one.
func appendPositions(dst []int32, haystack, needle string) []int32 {
	if needle == "" {
		return dst
	}
	pos := 0
	for i := 0; i < len(needle); i++ {
		want := needle[i]
		found := false
		for pos < len(haystack) {
			if haystack[pos] == want {
				if utf8.RuneStart(haystack[pos]) {
					dst = append(dst, int32(pos))
				}
				pos++
				found = true
				break
			}
			pos++
		}
		if !found {
			return dst
		}
	}
	return dst
}

// lower is ASCII case folding that returns its argument unallocated when there
// is nothing to fold. Non-ASCII bytes pass through untouched, which degrades an
// accented query to a case-sensitive match instead of mangling it.
func lower(s string) string {
	upper := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			upper = i
			break
		}
	}
	if upper < 0 {
		return s
	}
	out := make([]byte, len(s))
	copy(out, s[:upper])
	for i := upper; i < len(s); i++ {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		out[i] = b
	}
	return string(out)
}

// -- the keyboard while the filter is open ------------------------------------

// filterKey handles the keys an open filter owns and reports whether it took
// the keystroke. It claims as little as it can: esc closes it, ↑/↓ walk the
// candidates, tab completes, and the send key completes ONLY when a row is
// actually highlighted — a filter showing no match must not swallow the send of
// a draft the user has finished typing.
func (m *Model) filterKey(s string) (tea.Cmd, bool) {
	switch s {
	case "esc":
		m.closeMentionFilter()
		return nil, true
	case "up":
		m.moveSelection(-1)
		return nil, true
	case "down":
		m.moveSelection(1)
		return nil, true
	case "tab":
		m.completeMention()
		return nil, true
	}
	if s == m.sendKey || s == "enter" {
		if m.filter.hasSelection() {
			m.completeMention()
			return nil, true
		}
	}
	return nil, false
}

// moveSelection walks the candidate list, clamping at both ends rather than
// wrapping: a list that jumps from the last row to the first costs the reader
// their place, and this list is at most a screenful.
func (m *Model) moveSelection(delta int) {
	f := &m.filter
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

// completeMention replaces the '@' and its needle with the selected target's
// token plus one trailing space, leaving the cursor after it — where a person
// carries on typing the message they were addressing.
//
// The trailing space is not decoration: it closes the token (see
// [matchWordAt]), so the very next character typed is prose rather than a rune
// that would silently unmake the mention.
func (m *Model) completeMention() {
	t, ok := m.filter.selected()
	if !ok {
		m.closeMentionFilter()
		return
	}
	at := m.filter.at
	token := []rune("@" + t.Word + " ")
	next := make([]rune, 0, len(m.value)-(m.cursor-at)+len(token))
	next = append(next, m.value[:at]...)
	next = append(next, token...)
	next = append(next, m.value[m.cursor:]...)
	m.value = next
	m.cursor = at + len(token)
	m.historyStep = 0
	m.closeMentionFilter()
	m.syncMentions()
}
