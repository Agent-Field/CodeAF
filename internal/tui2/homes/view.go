package homes

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Selection is what the rail's cursor rests on, told to this package rather
// than owned by it (5.15: one cursor, and it belongs to the model that already
// has one).
type Selection struct {
	// Home is the room the rail is scoped into.
	Home Home
	// Row is the member row id the cursor rests on — [BeliefRowPrefix]…,
	// [RouteID.RowID], [CharterRowPrefix]…, [ServiceRowPrefix]… — or "" for the
	// scope's own surface row, which shows the room's brief.
	Row string
}

// View renders the main pane for a [Selection]. It owns the buffers a repaint
// needs, so a pane that repaints on a timer allocates almost nothing per frame.
//
// A View is not safe for concurrent use and is not meant to be — it belongs to
// the pane that draws with it.
type View struct {
	profile tokens.Profile
	focus   tokens.Focus
	glyphs  tokens.GlyphSet

	lines []string
	buf   strings.Builder
	line  lineBuf
	wrap  []string
}

// NewView returns a View painting for a Styler's profile, focus and glyph tier.
// A nil Styler means no colour and the plain tier, which is the honest
// degradation for a terminal that would not say what it can do.
func NewView(st *tokens.Styler) *View {
	v := &View{}
	v.SetStyler(st)
	return v
}

// SetStyler re-points the View. Dimming is a property of the pane (8.3), so a
// pane that just lost focus calls this and repaints; it does not re-resolve
// anything per row.
func (v *View) SetStyler(st *tokens.Styler) {
	if st == nil {
		v.profile, v.focus, v.glyphs = tokens.NoColor, tokens.FocusNormal, tokens.Plain
		return
	}
	v.profile, v.focus, v.glyphs = st.Profile(), st.Focus(), st.GlyphSet()
}

// Profile reports the terminal profile the View paints for.
func (v *View) Profile() tokens.Profile { return v.profile }

// Focus reports the pane focus the View paints for.
func (v *View) Focus() tokens.Focus { return v.focus }

// GlyphSet reports the repertoire tier the View draws with (12.7).
func (v *View) GlyphSet() tokens.GlyphSet { return v.glyphs }

// Render draws the pane at a size and returns at most height lines, each at
// most width printable cells.
//
// The returned slice aliases the View's buffer and is valid until the next
// Render. Callers that keep lines across frames must copy them.
func (v *View) Render(state State, sel Selection, width, height int) []string {
	v.lines = v.lines[:0]
	if width <= 0 || height <= 0 || !sel.Home.Valid() {
		return v.lines
	}
	switch sel.Home {
	case HomeNotebook:
		v.notebook(state, sel.Row, width, height)
	case HomeSelf:
		v.self(state, sel.Row, width, height)
	case HomeStanding:
		v.standing(state, sel.Row, width, height)
	case HomeServices:
		v.services(state, sel.Row, width, height)
	}
	return v.lines
}

// colored reports whether the profile admits to colour. It gates the strike and
// nothing else; every other rendering decision is the token layer's.
func (v *View) colored() bool { return v.profile != tokens.NoColor }

// clean is the View's door for prose somebody else wrote. See [cleanFor].
func (v *View) clean(s string) string { return cleanFor(v.profile, s) }

// glyph resolves a vocabulary slot through the tier (12.7), so a nerd-font
// terminal gets the icon and every other one gets the designed floor.
func (v *View) glyph(id tokens.GlyphID) string { return v.glyphs.Glyph(id) }

// push appends a line while there is room in the height budget.
func (v *View) push(line string, limit int) bool {
	if len(v.lines) >= limit {
		return false
	}
	v.lines = append(v.lines, line)
	return len(v.lines) < limit
}

// text lays one plain line at the content edge (§20). Nothing in this pane
// starts in column 0: the two cells in front of the edge are the gutter, and a
// line with no marker leaves them empty rather than claiming them.
func (v *View) text(s string, tok tokens.Token, width, limit int) bool {
	l := &v.line
	l.reset(width)
	l.padTo(contentEdge)
	l.add(v.clean(s), tok)
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit)
}

// blank lays an empty spacing row. 5.13 spends vertical space rather than rules
// on separation, and a pane that is out of room spends neither.
func (v *View) blank(limit int) bool {
	if len(v.lines) == 0 || len(v.lines) >= limit {
		return len(v.lines) < limit
	}
	return v.push("", limit)
}

// heading is a room's or a row's title line: the state glyph, then the words.
// It is the only place in this package a glyph and its colour are chosen
// together, so the two can never disagree.
func (v *View) heading(glyph string, tok tokens.Token, title string, width, limit int) bool {
	l := &v.line
	l.reset(width)
	if glyph != "" {
		l.add(glyph, tok)
	}
	if l.w >= contentEdge {
		l.add(" ", tokens.TextTertiary)
	} else {
		l.padTo(contentEdge)
	}
	l.add(v.clean(title), tokens.TextPrimary)
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit)
}

// sectionWord is a room's own name: one faint lowercase word at [contentEdge],
// with [blocks.SectionAbove] blank above it and none below.
//
// IT USED TO WEAR A GLYPH, and the glyph was [tokens.GQueued] — the QUEUED state
// mark, on a room. §15's delete test settles it in one move: take the `○` away
// and nothing is lost, because a notebook has no lifecycle to be queued in and
// the mark was never saying anything about one. What it was doing was spending
// the gutter, which §20 reserves for markers that mean something, and lending a
// page title the shape of a work row.
//
// THE WORD IS FAINT, which is the first of [blocks]' three section answers: a
// band inside one page gets a dim lowercase word, a band the reader can OPEN
// gets that word promoted to a door, and a boundary between two KINDS of thing
// gets a rule. This is the first case — the page under it is the subject, and a
// heading that outshone it would be chrome outranking content (5.13).
//
// The air is ABOVE and never below, because a section word belongs to what
// follows it and padding is how a row says which side it is on.
func (v *View) sectionWord(word string, width, limit int) bool {
	for i := 0; i < blocks.SectionAbove; i++ {
		if !v.blank(limit) {
			return false
		}
	}
	l := &v.line
	l.reset(width)
	l.padTo(contentEdge)
	l.add(v.clean(word), tokens.TextTertiary)
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit)
}

// prose wraps a body across the pane at the secondary tier. It is the one place
// this package draws more than one line of somebody else's words, and it is
// bounded by the height budget like everything else.
func (v *View) prose(body string, tok tokens.Token, width, limit int) bool {
	body = v.clean(body)
	if body == "" {
		return len(v.lines) < limit
	}
	measure := width - contentEdge
	if measure < 1 {
		measure = width
	}
	v.wrap, _ = blocks.Wrap(v.wrap[:0], body, measure)
	for _, row := range v.wrap {
		if !v.text(row, tok, width, limit) {
			return false
		}
	}
	return len(v.lines) < limit
}

// pair lays a `label  value` row: the label in the chrome tier at a fixed
// column, the value in the body tier. The column is a fraction of the width
// rather than a constant, so a 40-cell pane and a 120-cell one both read as two
// columns instead of one of them reading as a label with a gap.
func (v *View) pair(label, value string, width, limit int) bool {
	l := &v.line
	l.reset(width)
	col := labelColumn(width)
	l.padTo(contentEdge)
	l.add(v.clean(label), tokens.TextTertiary)
	l.padTo(col)
	l.add(v.clean(value), tokens.TextSecondary)
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit)
}

// pairPath is [View.pair] for a value that is a path (12.5.1) — middle
// ellipsis, never a tail cut.
func (v *View) pairPath(label, path string, width, limit int) bool {
	l := &v.line
	l.reset(width)
	l.padTo(contentEdge)
	l.add(v.clean(label), tokens.TextTertiary)
	l.padTo(labelColumn(width))
	l.addPath(v.clean(path), tokens.TextSecondary)
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit)
}

// labelColumn is where the value column starts, measured from the pane's own
// left edge: the content edge plus the widest label this package writes
// ("auto-restart", twelve cells) plus one for the gap. Under a narrow pane it
// gives way rather than eating the value.
func labelColumn(width int) int {
	const want = contentEdge + 13
	if width < want*2 {
		if half := width / 2; half > 0 {
			return half
		}
		return 0
	}
	return want
}

// row lays a list row: glyph, name, and a right-flush note that claims its room
// BEFORE the name does — 5.21's width-stability law applied to a whole line, so
// a name growing by a character never pushes a number off the row.
func (v *View) row(glyph string, gtok tokens.Token, name, note string, tok tokens.Token, struck bool, width, limit int) bool {
	l := &v.line
	l.reset(width)
	if glyph != "" {
		l.add(glyph, gtok)
	}
	if l.w >= contentEdge {
		l.add(" ", tokens.TextTertiary)
	} else {
		l.padTo(contentEdge)
	}
	note = v.clean(note)
	noteW := blocks.Width(note)
	if noteW > 0 {
		noteW++
	}
	room := l.room() - noteW
	if room < 0 {
		room = 0
	}
	name = v.clean(name)
	if blocks.Width(name) > room {
		name = blocks.Truncate(name, room)
	}
	if struck {
		l.addStruck(name, tok, v.colored())
	} else {
		l.add(name, tok)
	}
	if note != "" && l.room() >= noteW {
		l.padTo(l.max - noteW + 1)
		l.add(note, tokens.TextTertiary)
	}
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit)
}

// indented lays a dim continuation line under a list row, at the child edge E1
// (§20): one indent step past the content edge its parent hangs from.
func (v *View) indented(s string, width, limit int) bool {
	s = v.clean(s)
	if s == "" {
		return len(v.lines) < limit
	}
	l := &v.line
	l.reset(width)
	l.padTo(childEdge)
	l.add(s, tokens.TextTertiary)
	return v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit)
}

// indentStep is §20's step S: two cells per level of depth, never one and never
// three. [contentEdge] (page.go) is E0 and [childEdge] is E1.
//
// It is [blocks.IndentStep] — the one place the step is a number — so this page
// and the transcript beside it descend at the same rhythm by construction.
const indentStep = blocks.IndentStep

// verbs draws the affordance strip of 5.22 rule 1: the verbs of the focused
// object, dim, each preceded by its accelerator, on one line — `p pause · b
// probation · c cadence · r retire`.
//
// A disabled verb keeps its place and spends the strip's tail on the reason,
// because a verb that vanished when it could not be used would be a verb the
// person has to rediscover (5.20 rule 3). The reason is shown ONCE, for the
// first disabled verb, since the common case by far is one reason disabling
// every verb at once — a visitor window (5.24) — and printing it four times
// would spend the whole line saying it.
func (v *View) verbs(list []Verb, width, limit int) bool {
	if len(list) == 0 {
		return len(v.lines) < limit
	}
	l := &v.line
	l.reset(width)
	l.padTo(contentEdge)
	reason := ""
	for i := range list {
		if list[i].Label == "" {
			continue
		}
		if l.w > contentEdge {
			l.add(sepRun, tokens.TextTertiary)
		}
		tok := tokens.TextSecondary
		if list[i].Disabled != "" {
			tok = tokens.TextTertiary
			if reason == "" {
				reason = list[i].Disabled
			}
		}
		if list[i].Key != "" {
			l.add(v.clean(list[i].Key), tokens.TextTertiary)
			l.add(" ", tokens.TextTertiary)
		}
		l.add(v.clean(list[i].Label), tok)
	}
	if l.w <= contentEdge {
		return len(v.lines) < limit
	}
	if !v.push(l.emit(&v.buf, v.profile, v.focus, width, false, tokens.Ground), limit) {
		return false
	}
	if reason != "" {
		return v.text(reason, tokens.TextTertiary, width, limit)
	}
	return len(v.lines) < limit
}

// stateGlyph resolves a lifecycle to its glyph and colour through the same two
// axes the rail uses, so a row means the same thing in both places.
// It is [lifeGlyph], which the full page draws with too — one mapping, so a row
// cannot mean one thing in the detail pane and another on the page.
func (v *View) stateGlyph(life Lifecycle, needs bool) (string, tokens.Token) {
	return lifeGlyph(v.glyphs, life, needs)
}
