package rail

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Mode is which of the three renderings of one scope model to draw (5.15,
// 8.2.8). Scope is not a rail feature; the rail is one rendering of it.
type Mode uint8

// ModeRail and ModeList name two SURFACES and share one implementation, which
// is 5.15 taken literally: "narrow terminals render the same scope rows, with
// the same keys and the same selection semantics, as a full-pane list". The
// same rows means the same code. What differs between a 28-column rail and an
// 80-column pane is how many columns the row has, and the renderer reads that
// from the width it was handed rather than from a flag — which is also why a
// card's telemetry degrades and drops by ROOM (see [View.fitMetaInto]) instead
// of by mode.
//
// The constants stay distinct because the shell speaks them: it is choosing a
// surface, and a caller that says ModeRail should not have to also compute the
// breakpoint that made it true.
const (
	// ModeAuto picks between [ModeRail] and [ModeList] with [ModeFor]. It is
	// the zero value, so a shell that never thinks about breakpoints still gets
	// a sane rendering.
	ModeAuto Mode = iota
	// ModeRail is the right rail at or above [tokens.RailAtWidth].
	ModeRail
	// ModeList is the scope as a full-pane list below that breakpoint. At 80
	// columns this is the primary experience, not a fallback (Part 9.12).
	ModeList
	// ModeHUD is the bounded sticky summary above the composer (8.2.8): at most
	// [tokens.HUDRowCap] rows plus a fold line. It carries the live summary and
	// never the scope map, which is why it draws no surface row, no scope
	// header and no selection.
	ModeHUD
)

// String names the mode.
func (m Mode) String() string {
	switch m {
	case ModeAuto:
		return "auto"
	case ModeRail:
		return "rail"
	case ModeList:
		return "list"
	case ModeHUD:
		return "hud"
	}
	return "invalid"
}

// ModeFor is the breakpoint decision for a TERMINAL width, and the number lives
// in the tokens table rather than in this file (10.5.24: the numbers are
// written down before the code). A shell laying out a rail pane passes the
// terminal's width here and [ModeRail] to the pane it then creates.
func ModeFor(width int) Mode {
	if width >= tokens.RailAtWidth {
		return ModeRail
	}
	return ModeList
}

// Layout constants that are this package's own, with the arithmetic that
// produced them.
const (
	// gutterWidth is the left column a map rendering reserves. It carries the
	// ▎ accent rail (5.21) that marks the selection while the pane is
	// UNFOCUSED — tokens' contrast law forbids a dimmed foreground on a raised
	// band, so a dimmed pane marks its selection with the accent instead. The
	// column is reserved in both focus states so nothing shifts sideways when
	// focus moves.
	gutterWidth = 1
	// gutterFloor is the width below which the gutter is spent on content
	// instead. At seven columns a row is a glyph and four letters, and a column
	// given to a selection marker is a column taken from the name — see
	// [gutterFor].
	gutterFloor = 8
	// indentStep is 5.13's spacing rhythm: two spaces per depth.
	indentStep = 2
	// sep is the telemetry separator (5.17) and its display width.
	sep      = " " + tokens.GlyphSeparator + " "
	sepWidth = 3
	// maxCardWorkers bounds how far a focused card expands in place. A card
	// that expands to forty rows has stopped being a card.
	maxCardWorkers = 6
	// minNameWidth is the narrowest a name may be squeezed to before the cells
	// competing with it start dropping instead.
	minNameWidth = 6
)

// Telemetry drop priorities (5.9: "money is always visible; the rest can
// truncate on narrow rails"). Higher survives longer — the discipline of
// 10.5.22's footer registry, applied to a card's third line.
//
// The order below is what a reader does something about, in order. Money is
// pinned because it is the one number the user never forgives us for hiding.
// Elapsed outranks the model word because "how long has this been going" is a
// question that gets asked of a rail every few seconds and "which model" is one
// that gets asked once a session — and 5.15's own wireframe spends a narrow
// worker row on the clock. Context outranks the worker count because it is a
// health signal (5.9) and a count is trivia.
const (
	prioMoney   = 100
	prioElapsed = 80
	prioModel   = 70
	prioContext = 60
	prioWorkers = 50
)

// View renders a [Model]. It owns the buffers a repaint needs, so a rail that
// repaints on a timer allocates almost nothing per frame: the line slice, the
// span slice, the fold buffers and the string builder are all reused.
//
// A View is not safe for concurrent use and is not meant to be — it belongs to
// the pane that draws with it.
type View struct {
	profile tokens.Profile
	focus   tokens.Focus

	lines []string
	buf   strings.Builder
	line  lineBuf

	fold    folder
	heights []int
	shape   []rowShape

	hudRows   []int
	hudStates []blocks.ItemState
	hudFolder blocks.Folder

	meta  [5]metaCell
	rule  string
	ruleW int
}

// NewView returns a View painting for a Styler's profile and focus. A nil
// Styler means no colour, which is the honest degradation for a terminal that
// would not say what it can do.
func NewView(st *tokens.Styler) *View {
	v := &View{}
	v.SetStyler(st)
	return v
}

// SetStyler re-points the View at a profile and focus. Dimming is a property of
// the pane (8.3), so a pane that just lost focus calls this and repaints; it
// does not re-resolve anything per row.
func (v *View) SetStyler(st *tokens.Styler) {
	if st == nil {
		v.profile, v.focus = tokens.NoColor, tokens.FocusNormal
		return
	}
	v.profile, v.focus = st.Profile(), st.Focus()
}

// Profile and Focus report what the View paints for.
func (v *View) Profile() tokens.Profile { return v.profile }

// Focus reports the pane focus this View paints for.
func (v *View) Focus() tokens.Focus { return v.focus }

// Render draws the model at a size and returns at most height lines, each at
// most width printable cells.
//
// The returned slice aliases the View's buffer and is valid until the next
// Render. Callers that keep lines across frames must copy them.
func (v *View) Render(m *Model, mode Mode, width, height int) []string {
	v.lines = v.lines[:0]
	if m == nil || width <= 0 || height <= 0 {
		return v.lines
	}
	if mode == ModeAuto {
		mode = ModeFor(width)
	}
	if mode == ModeHUD {
		v.renderHUD(m, width, height)
		return v.lines
	}
	v.renderMap(m, width, height)
	return v.lines
}

// Rail, List and HUD are the three renderings by name, for a caller that has
// already decided.
func (v *View) Rail(m *Model, width, height int) []string {
	return v.Render(m, ModeRail, width, height)
}

// List renders the scope as a full-pane list.
func (v *View) List(m *Model, width, height int) []string {
	return v.Render(m, ModeList, width, height)
}

// HUD renders the bounded live summary.
func (v *View) HUD(m *Model, width, height int) []string {
	return v.Render(m, ModeHUD, width, height)
}

// renderMap draws the scope map: scope header, the surface row that never
// folds, a hairline at the room boundary, then the members under the overflow
// policy.
func (v *View) renderMap(m *Model, width, height int) {
	scope := m.Scope()
	rows := scope.Rows
	sel := m.Cursor()
	ident := v.identity(scope.Seed, tokens.Token(255))
	band := tokens.BandFor(ident)
	// The contrast law (tokens.Legal): a dimmed foreground may sit only on the
	// ground, so an unfocused pane draws no band at all and marks its selection
	// with the accent rail instead.
	banded := v.focus == tokens.FocusNormal

	if m.Depth() > 0 {
		v.push(v.scopeHeader(scope, sel, len(rows)-1, ident, width), height)
	}

	// Row 0 is the conversational surface and is never folded away: a scope you
	// cannot speak into is not a scope (5.15).
	v.appendRow(rows[0], sel == 0, width, height, band, banded, ident)

	members := rows[1:]
	if len(members) == 0 {
		return
	}
	// The hairline is the room boundary (5.13), and it is worth a row only
	// when there are at least two left for the members it separates — one for
	// a row and one for the fold line that accounts for the rest.
	if len(v.lines)+2 < height {
		v.push(v.hairline(width), height)
	}

	budget := height - len(v.lines)
	if budget <= 0 {
		return
	}

	v.sizeMembers(members, sel-1)
	p := v.fold.fit(members, v.heights, budget, sel-1, scope.Live())
	if p.atTop && p.fold != "" {
		v.push(v.foldLine(p.fold, width), height)
	}
	prev := ident
	for _, i := range p.shown {
		r := members[i]
		rowIdent := ident
		if r.Kind == RowTask {
			rowIdent = v.identity(seedOf(r, scope.Seed), prev)
			prev = rowIdent
		}
		// The band is the SCOPE's, never the row's: 5.16 spends the identity
		// tint on "which room am I in", so it is plain at home and tinted
		// inside a task's scope. A home rail that tinted each selection with
		// the selected card's hue would answer a question nobody asked and
		// make the band change colour as the cursor moves.
		v.appendRow(r, sel == i+1, width, height, band, banded, rowIdent)
	}
	if !p.atTop && p.fold != "" {
		v.push(v.foldLine(p.fold, width), height)
	}
}

// sizeMembers measures every member so the fold can budget in lines.
func (v *View) sizeMembers(members []Row, sel int) {
	if cap(v.heights) < len(members) {
		v.heights = make([]int, len(members))
		v.shape = make([]rowShape, len(members))
	}
	v.heights = v.heights[:len(members)]
	v.shape = v.shape[:len(members)]
	for i := range members {
		v.shape[i] = shapeOf(members[i], i == sel)
		v.heights[i] = v.shape[i].height()
	}
}

// renderHUD draws the bounded sticky summary (8.2.8). It is a different object
// from the rail with a different job: one line per live thing, capped, with a
// fold line that accounts for the rest. No cursor, no surface row, no map.
//
// The fold here is [blocks.Folder] verbatim — equal-height rows and no
// selection to pin is exactly the shape that package's 8.1.7 implementation
// was written for.
func (v *View) renderHUD(m *Model, width, height int) {
	budget := height
	if budget > tokens.HUDRowCap {
		budget = tokens.HUDRowCap
	}
	if budget <= 0 {
		return
	}
	scope := m.Scope()
	members := scope.Members()
	// Indices rather than copies: a Row is a wide struct and the HUD re-filters
	// on every frame.
	v.hudRows = v.hudRows[:0]
	v.hudStates = v.hudStates[:0]
	live := false
	for i := range members {
		if !hudWorthy(members[i]) {
			continue
		}
		v.hudRows = append(v.hudRows, i)
		v.hudStates = append(v.hudStates, members[i].itemState())
		if members[i].Attention().Live() {
			live = true
		}
	}
	if len(v.hudRows) == 0 {
		return
	}
	var p blocks.Plan
	if live {
		p = v.hudFolder.Live(v.hudStates, budget)
	} else {
		p = v.hudFolder.Finalized(v.hudStates, budget)
	}
	if p.FoldAtTop && p.Fold != "" {
		v.push(v.foldLine(p.Fold, width), budget)
	}
	prev := tokens.Token(255)
	for _, i := range p.Shown {
		r := &members[v.hudRows[i]]
		ident := prev
		if r.Kind == RowTask {
			ident = v.identity(seedOf(*r, scope.Seed), prev)
			prev = ident
		}
		v.push(v.hudLine(*r, width, ident), budget)
	}
	if !p.FoldAtTop && p.Fold != "" {
		v.push(v.foldLine(p.Fold, width), budget)
	}
}

// hudWorthy is what the bounded summary carries: everything that is not over.
// A settled or cancelled row has nothing left to say in a live summary, and
// 10.3.15 is explicit that everything else must be carried — background work
// with no panel goes invisible.
func hudWorthy(r Row) bool {
	if r.Questions > 0 {
		return true
	}
	switch r.Life {
	case LifeSettled, LifeCancelled:
		return false
	}
	return true
}

// hudLine is one bounded summary row: glyph, name, status, and the two numbers
// a person actually acts on.
func (v *View) hudLine(r Row, width int, ident tokens.Token) string {
	l := &v.line
	l.reset(width)
	att := r.Attention()
	l.add(att.Glyph(), v.glyphToken(r, ident))
	l.add(" ", tokens.TextTertiary)

	right := v.rightCell(r.Meta)
	rightW := blocks.Width(right)
	nameRoom := l.max - l.w - rightW
	if rightW > 0 {
		nameRoom--
	}
	name := clean(r.Name)
	if status := clean(r.Status); status != "" && nameRoom > blocks.Width(name)+minNameWidth+sepWidth {
		l.add(blocks.Truncate(name, nameRoom), v.nameToken(r))
		room := l.max - l.w - rightW
		if rightW > 0 {
			room--
		}
		l.add(sep, tokens.TextTertiary)
		l.add(blocks.Truncate(status, room-sepWidth), tokens.TextSecondary)
	} else {
		l.add(blocks.Truncate(name, nameRoom), v.nameToken(r))
	}
	if right != "" {
		l.padTo(l.max - rightW)
		l.add(right, tokens.TextTertiary)
	}
	return v.emit(width, false, tokens.Ground)
}

// rightCell is the HUD's flush-right pair: money then elapsed, both width
// stable so the column never dances (5.21).
func (v *View) rightCell(t Telemetry) string {
	var b strings.Builder
	if t.HasCost {
		b.WriteString(tokens.Money(t.Cost))
	}
	if t.HasElapsed {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(tokens.ElapsedCell(t.Elapsed))
	}
	return b.String()
}

// scopeHeader is the breadcrumb tail (5.15) with the position of 10.3.10:
// `‹ wisp-parity            (2 of 5)`.
func (v *View) scopeHeader(scope Scope, sel, members int, ident tokens.Token, width int) string {
	l := &v.line
	l.reset(width)
	l.add(tokens.GlyphScopeUp, tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	pos := ""
	if sel > 0 && members > 0 {
		var buf [24]byte
		out := append(buf[:0], '(')
		out = strconv.AppendInt(out, int64(sel), 10)
		out = append(out, " of "...)
		out = strconv.AppendInt(out, int64(members), 10)
		out = append(out, ')')
		pos = string(out)
	}
	posW := blocks.Width(pos)
	room := l.max - l.w - posW
	if posW > 0 {
		room--
	}
	l.add(blocks.Truncate(clean(scope.Title), room), ident)
	if pos != "" && l.max-l.w >= posW {
		l.padTo(l.max - posW)
		l.add(pos, tokens.TextTertiary)
	}
	return v.emit(width, false, tokens.Ground)
}

// hairline is the rule under the surface row. 5.13 allows rules only at room
// boundaries, and the seam between a scope's conversational surface and its
// members is exactly one.
func (v *View) hairline(width int) string {
	if v.ruleW != width {
		v.rule = strings.Repeat(tokens.GlyphTreeDash, width)
		v.ruleW = width
	}
	l := &v.line
	l.reset(width)
	l.add(v.rule, tokens.TextTertiary)
	return v.emit(width, false, tokens.Ground)
}

// foldLine draws the overflow accounting (8.1.7). It is chrome: the rows it
// stands for are what the reader is after, not the line itself.
func (v *View) foldLine(text string, width int) string {
	l := &v.line
	l.reset(width)
	l.add(blocks.Truncate(text, width), tokens.TextTertiary)
	return v.emit(width, false, tokens.Ground)
}

// push appends a line while there is room in the height budget.
func (v *View) push(line string, limit int) {
	if len(v.lines) < limit {
		v.lines = append(v.lines, line)
	}
}

// identity resolves a seed to its pastel, avoiding a collision with the row
// above (5.16: no two ADJACENT rail cards share a hue). A seedless row inherits
// nothing — it gets the plain grey path, which is what [tokens.BandFor] turns
// into an untinted band.
func (v *View) identity(seed string, prev tokens.Token) tokens.Token {
	if seed == "" {
		return tokens.Token(255)
	}
	return tokens.IdentityNext(seed, prev)
}

func seedOf(r Row, fallback string) string {
	if r.Seed != "" {
		return r.Seed
	}
	if r.ID != "" {
		return r.ID
	}
	return fallback
}

// addGlyph writes a row's state glyph and the space after it. It is the one
// place a glyph and its colour are chosen, so the two can never disagree.
//
// The SURFACE row is the exception the vocabulary needs: a conversational
// surface is not a job, and ✓ on an idle `aforge` would claim a success nobody
// achieved. Idle, it draws the room dot the 5.15 wireframes show, in the
// scope's identity pastel — "which room am I in", answered peripherally. Alive
// or blocked, it takes the ordinary vocabulary, because those states mean the
// same thing on every row.
func (v *View) addGlyph(l *lineBuf, r Row, ident tokens.Token) {
	glyph, tok := v.glyphOf(r, ident)
	l.add(glyph, tok)
	l.add(" ", tokens.TextTertiary)
}

func (v *View) glyphOf(r Row, ident tokens.Token) (string, tokens.Token) {
	att := r.Attention()
	if r.Kind == RowSurface {
		switch att {
		case AttnQuestion, AttnWorking, AttnFailed, AttnCancelled:
			// fall through to the ordinary vocabulary
		default:
			return roomDot, v.identityOr(ident, tokens.TextPrimary)
		}
	}
	return att.Glyph(), v.glyphToken(r, ident)
}

// roomDot is the ● the 5.15 wireframes draw beside a room name. It is
// [tokens.GlyphStepDone]'s character — one measured, tintable, single-width
// glyph, reused rather than invented, since a token layer that owned two names
// for one cell would be a token layer with a synonym.
const roomDot = tokens.GlyphStepDone

// glyphToken resolves the glyph's colour, and is the only place identity enters
// a row's foreground (5.16).
func (v *View) glyphToken(r Row, ident tokens.Token) tokens.Token {
	switch h := r.GlyphHue(); h {
	case tokens.HueIdentity:
		return v.identityOr(ident, tokens.TextPrimary)
	case tokens.HueNone:
		// Queued and paused: not moving and not done. Chrome tier, because a
		// bright ○ on twenty pending rows is twenty claims on the eye that
		// nothing has earned.
		return tokens.TextTertiary
	default:
		return tokens.ResolveToken(h, r.State())
	}
}

// identityOr is the identity token when there is one and a fallback when the
// row has no seed — a scope with no identity (home) paints no pastel rather
// than borrowing wheel entry zero.
func (v *View) identityOr(ident, fallback tokens.Token) tokens.Token {
	if ident < tokens.Identity0 || ident > tokens.Identity7 {
		return fallback
	}
	return ident
}

// nameToken carries the state axis (8.1.6): accent while live, plain once
// settled. It never carries a hue — 5.16 is explicit that accent hues do not
// colourise text.
func (v *View) nameToken(r Row) tokens.Token {
	return tokens.ResolveToken(tokens.HueNone, r.State())
}
