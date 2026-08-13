package palette

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The shared list. Both 5.22 surfaces are this, and the only thing they argue
// about is what goes in the catalog and what is drawn above it.
//
// It owns three things and no more: which rows survive the current filter,
// which one is selected, and how a row becomes cells. It owns no keyboard (the
// components bind that), no overlay state (the shell owns that), and no
// knowledge of what a row DOES (the wiring owns that).

// Geometry. The numbers are stated here rather than discovered inside a render
// (10.5.24), and each one is derived rather than picked.
const (
	// markerCol is the selection column: one cell holding [tokens.GlyphAccentRail]
	// on the selected row and a space on every other. It is 5.21's left accent
	// rail ("structure without boxes"), and it is drawn ALONGSIDE the band
	// rather than instead of it — which is where this surface parts company
	// with the rail's own gutter, on purpose.
	//
	// The rail marks XOR bands because its one gutter cell is already spoken
	// for by an identity glyph and because a dimmed pane may not carry a band
	// at all (12.11.2). A dialog is never the unfocused pane, and it has the
	// cell: so it spends it, and gets three things for one column. Selection
	// survives `--color none`, where this surface previously drew NOTHING at
	// all — [lineBuf.emit] refuses a band with no bytes to spend, and no other
	// carrier existed, which is exactly the defect 12.11.2 closed in the rail
	// and left standing here. The band on a sheet is a rung shallower than the
	// band on the ground ([tokens.SheetSeparationMin]), so it wants the second
	// carrier anyway. And a selected row now reads at a glance from the left
	// edge, which is what a list of thirty verbs is scanned from.
	markerCol = 1
	// gutter is the marker column plus the glyph column: one cell for a room's
	// state glyph and one space after it. Non-room rows pad through it so every
	// verb in the list starts at the same column — a ragged left edge in a list
	// this dense reads as two lists.
	gutter = markerCol + 2
	// verbColMin and verbColMax bound the verb column. Below 6 a verb is
	// initials; above 22 the description has no room left on an overlay that
	// is itself only two thirds of the frame.
	verbColMin = 6
	verbColMax = 22
	// descColMin is the narrowest description worth drawing. Under it the
	// column is dropped entirely rather than shown as one word and a cut mark:
	// the accelerator outranks the description (see the package doc).
	descColMin = 8
	// descGap separates the verb column from the description.
	descGap = 2
	// rightGap is the minimum whitespace between a description and the
	// right-aligned accelerator, so the two never touch and read as one word.
	// Two cells rather than one: a slash alias ends in a letter and a
	// description ends in a letter, and one space between them reads as a
	// hyphenless compound at a glance.
	rightGap = 2
	// accelColMax caps the right column. The longest accelerator the registry
	// seeds is a slash alias, and one long enough to eat the description would
	// be teaching nothing.
	accelColMax = 16
)

// item is one line of the rendered list: a section header, the blank that opens
// a group, or a row.
type item struct {
	header bool
	// blank is the one line of ground that separates a group from the group
	// above it. 5.13's spacing rhythm — "cards separated by whitespace not
	// boxes" — applied to the only structure this list has.
	blank bool
	sec   section
	// hit indexes the surviving set, and is meaningless on a header or a blank.
	hit int32
}

// list is the filtered, selectable, renderable body of both components.
type list struct {
	profile tokens.Profile
	focus   tokens.Focus
	// linear is [Options.Linear], copied once at construction: it is a rendering
	// mode the surface is built in, not a state that moves under it.
	linear bool
	// noHeaders drops the group NAME above each section and keeps the blank line
	// between them. It is for a surface whose own header already names what the
	// list holds — [Switcher] — where a `threads` row over a sheet titled
	// `threads` would be §19's same-fact-twice at the top of a four-row list.
	//
	// The groups themselves survive: the blank is the structure (5.13 spends
	// this surface's structure budget on whitespace), and the header is the
	// label on it. Dropping the label is not dropping the boundary.
	noHeaders bool

	rows   []row
	hits   []hit
	query  string
	needle string

	cursor int
	top    int

	// emptyText is what the body says when nothing is on it. It is the
	// component's copy, not this type's: an empty capability overlay and an
	// over-filtered palette are different facts and must not share a sentence.
	emptyText string

	// Layout widths, computed once per catalog so the columns do not dance
	// while the user types (5.21: width-stable everything).
	verbW  int
	rightW int

	// Scratch, reused across frames. None of it is observable by the shell.
	line   lineBuf
	buf    strings.Builder
	pos    []int32
	items  []item
	lineOf []int32
	out    []string

	// lastView is the body height the previous render used, so a page key has
	// a page to move by.
	lastView int
}

// setStyle binds the terminal profile and pane focus this list paints for.
// A nil Styler is the headless case and paints nothing, which is what the
// golden harness and every test in this package want.
func (l *list) setStyle(s *tokens.Styler) {
	if s == nil {
		l.profile, l.focus = tokens.NoColor, tokens.FocusNormal
		return
	}
	l.profile, l.focus = s.Profile(), s.Focus()
}

// ground is the floor this surface paints under everything that is not banded.
//
// Linear mode (10.1.5) gets none, and for the same reason it gets no selection
// band: a background fill is what a screen reader cannot see and what a
// high-contrast terminal may render as a solid block. Nothing is lost — the
// ground carries no information, only elevation — which is exactly why it is
// the part that yields, and the ▎ of [markerCol] goes on saying which row the
// cursor is on either way.
func (l *list) ground() tokens.Token {
	if l.linear {
		return tokens.Ground
	}
	return sheetGround
}

// rebuild installs a freshly built catalog, reusing the row slice's capacity.
//
// The builder is called through rather than called by the caller because the
// old rows have to be READ before they are overwritten: the selection is
// preserved by RESULT rather than by index, since a task settling moves a room
// from `rooms` to `history` and shifts every index below it, and a palette
// whose selection jumped to a different action because something finished
// elsewhere would be the surface moving under the user's hand (7.2: a visible
// row never moves).
func (l *list) rebuild(build func(dst []row) []row) {
	prev, hadPrev := l.selectedResult()
	l.rows = build(l.rows)
	l.measure()
	l.refilter()
	if hadPrev {
		l.selectResult(prev)
	}
}

// selectedResult reports the result under the cursor, for selection recovery.
func (l *list) selectedResult() (Result, bool) {
	r, ok := l.selected()
	if !ok || r.result == nil {
		return nil, false
	}
	return r.result, true
}

// selectResult moves the cursor to the row carrying want, if it survived.
// Results are comparable value types, so this is an equality test and not a
// string round trip.
func (l *list) selectResult(want Result) {
	for i := range l.hits {
		if l.rows[l.hits[i].idx].result == want {
			l.cursor = i
			return
		}
	}
}

// measure fixes the two column widths from the whole catalog — not from the
// filtered set — so typing narrows the list without shifting its columns.
func (l *list) measure() {
	l.verbW, l.rightW = 0, 0
	for i := range l.rows {
		r := &l.rows[i]
		if w := blocks.Width(r.verb); w > l.verbW {
			l.verbW = w
		}
		if w := rightWidth(r); w > l.rightW {
			l.rightW = w
		}
	}
	l.verbW = clampInt(l.verbW, verbColMin, verbColMax)
	if l.rightW > accelColMax {
		l.rightW = accelColMax
	}
}

// rightWidth is how wide a row's right column wants to be: the `?N` count chip
// (5.21) and the accelerator, with a space between them when both are present.
// A disabled row advertises no accelerator and so asks for nothing.
func rightWidth(r *row) int {
	w := 0
	if r.questions > 0 {
		w += blocks.Width(chipText(r.questions))
	}
	if r.disabled == "" && r.accel != "" {
		if w > 0 {
			w++
		}
		w += blocks.Width(r.accel)
	}
	return w
}

// chipText is 5.21's compact count chip. Amber is applied at paint time; the
// glyph is 5.17's `?`, which is the same mark the rail draws for the same fact.
func chipText(n int) string { return tokens.GlyphNeedsHuman + strconv.Itoa(n) }

// setQuery installs a new filter and returns whether anything changed. The
// cursor returns to the top: after a keystroke the best match is a new row, and
// leaving the selection where it was would mean enter runs whatever happened to
// be at that index.
func (l *list) setQuery(q string) bool {
	if q == l.query {
		return false
	}
	l.query = q
	l.refilter()
	return true
}

func (l *list) refilter() {
	l.needle = lower(l.query)
	l.hits = filter(l.hits, l.rows, l.needle)
	l.cursor = 0
	l.top = 0
}

// count is how many rows survive the filter.
func (l *list) count() int { return len(l.hits) }

// selected returns the row under the cursor.
func (l *list) selected() (*row, bool) {
	if l.cursor < 0 || l.cursor >= len(l.hits) {
		return nil, false
	}
	return &l.rows[l.hits[l.cursor].idx], true
}

// move walks the selection by delta rows, clamping at both ends. It does not
// wrap: wrapping makes "hold down the down arrow" silently restart at the top,
// and a list whose end is not felt is a list whose length is not known.
func (l *list) move(delta int) bool {
	if len(l.hits) == 0 {
		return false
	}
	next := clampInt(l.cursor+delta, 0, len(l.hits)-1)
	if next == l.cursor {
		return false
	}
	l.cursor = next
	return true
}

// page moves by whole screens, using the height the last render actually had.
func (l *list) page(dir int) bool {
	step := l.lastView
	if step < 1 {
		step = 1
	}
	return l.move(dir * step)
}

// selectHit points the cursor at one surviving row by its index in the
// filtered set — the door a click takes.
func (l *list) selectHit(i int) bool {
	if i < 0 || i >= len(l.hits) || i == l.cursor {
		return false
	}
	l.cursor = i
	return true
}

// hitAtLine maps a body line from the last render back to a surviving row.
// Headers, blanks and the empty-state line map to nothing, which is how a
// click on chrome does nothing rather than something arbitrary.
func (l *list) hitAtLine(y int) (int, bool) {
	if y < 0 || y >= len(l.lineOf) {
		return 0, false
	}
	if l.lineOf[y] < 0 {
		return 0, false
	}
	return int(l.lineOf[y]), true
}

// render draws the body at width by height, at most height lines, none wider
// than width. It appends into the list's own reused slice; the caller joins.
func (l *list) render(width, height int) []string {
	l.out = l.out[:0]
	l.lineOf = l.lineOf[:0]
	if width <= 0 || height <= 0 {
		return l.out
	}
	l.lastView = height

	if len(l.hits) == 0 {
		l.line.reset(width)
		l.line.add(blocks.Truncate(l.emptyText, width), tokens.TextTertiary)
		l.out = append(l.out, l.line.emit(&l.buf, l.profile, l.focus, width, false, tokens.Band, l.ground()))
		l.lineOf = append(l.lineOf, -1)
		return l.out
	}

	l.buildItems()
	l.clampCursor()

	// The overflow cue costs a line, so the body is measured before the scroll
	// is computed — a window sized against a height the cue then takes back is a
	// window whose last row is always the one the reader wanted.
	body := height
	if len(l.items) > height && height > 1 {
		body = height - 1
	}
	l.scroll(body)
	lay := l.layout(width)

	shown := 0
	for i := l.top; i < len(l.items) && len(l.out) < body; i++ {
		it := l.items[i]
		switch {
		case it.blank:
			// Never as the first line: a window that opens on the gap above a
			// group has spent its top row saying nothing.
			if len(l.out) == 0 {
				continue
			}
			l.out = append(l.out, blankLine(&l.line, &l.buf, l.profile, l.focus, width, l.ground()))
			l.lineOf = append(l.lineOf, -1)
		case it.header:
			l.out = append(l.out, l.renderHeader(it.sec, width))
			l.lineOf = append(l.lineOf, -1)
		default:
			r := &l.rows[l.hits[it.hit].idx]
			l.out = append(l.out, l.renderRow(r, int(it.hit) == l.cursor, lay, width))
			l.lineOf = append(l.lineOf, it.hit)
			shown++
		}
	}
	if hidden := len(l.hits) - shown; hidden > 0 && len(l.out) < height {
		l.out = append(l.out, l.renderMore(hidden, width))
		l.lineOf = append(l.lineOf, -1)
	}
	return l.out
}

// renderMore is the honest bottom of a clipped list: how many surviving rows
// are not on screen, above and below taken together.
//
// It is here because 5.20's affordance rule cuts both ways. A sheet showing
// thirteen of thirty verbs with no mark at all is not a short list, it is a
// list that lies about its length — and this is the surface whose entire job is
// to answer "what can this room do" without the reader having to guess. The
// count is of ROWS, not of lines: headers and gaps are chrome and were never
// what the reader was counting.
func (l *list) renderMore(hidden, width int) string {
	l.line.reset(width)
	l.line.padTo(gutter)
	l.line.add(strconv.Itoa(hidden)+" more", tokens.TextTertiary)
	return l.line.emit(&l.buf, l.profile, l.focus, width, false, tokens.Band, l.ground())
}

// buildItems interleaves section headers into the surviving set. The hits are
// already section-grouped (rows are built in section order and the filter sorts
// only within a run), so this is one pass and no lookahead.
func (l *list) buildItems() {
	l.items = l.items[:0]
	prev := sectionCount
	for i := range l.hits {
		sec := l.rows[l.hits[i].idx].sec
		if sec != prev {
			if len(l.items) > 0 {
				l.items = append(l.items, item{blank: true, hit: -1})
			}
			if !l.noHeaders {
				l.items = append(l.items, item{header: true, sec: sec, hit: -1})
			}
			prev = sec
		}
		l.items = append(l.items, item{hit: int32(i)})
	}
}

func (l *list) clampCursor() {
	if len(l.hits) == 0 {
		l.cursor = 0
		return
	}
	l.cursor = clampInt(l.cursor, 0, len(l.hits)-1)
}

// scroll moves the window so the cursor is on screen, and pulls the cursor's
// own section header along when the cursor is the first row under it — a
// selected row whose group name has scrolled off is a row with no context.
func (l *list) scroll(height int) {
	target := 0
	for i := range l.items {
		if !l.items[i].header && int(l.items[i].hit) == l.cursor {
			target = i
			break
		}
	}
	first := target
	if target > 0 && l.items[target-1].header {
		first = target - 1
	}
	if l.top > first {
		l.top = first
	}
	if target >= l.top+height {
		l.top = target - height + 1
	}
	if max := len(l.items) - height; l.top > max {
		l.top = max
	}
	if l.top < 0 {
		l.top = 0
	}
}

// layout resolves the three columns for one width.
type layout struct {
	verbW    int
	descAt   int
	rightW   int
	showDesc bool
}

func (l *list) layout(width int) layout {
	verbW := clampInt(l.verbW, verbColMin, verbColMax)
	// No column takes more than a third of the sheet. The absolute cap above is
	// measured against the CATALOG, so one outlier — a room titled "Permanent
	// Aforge spine", an alias row spelling out what it aliases — sets the
	// column for every row and pushes twenty descriptions into an ellipsis to
	// spell one verb in full. internal/tui2/settings reached the same rule for
	// the same column ("never more than a third of the frame, so a long label
	// never pushes the value off a narrow terminal"), and two surfaces of one
	// product answering this question differently is how a language drifts.
	if third := width / 3; verbW > third && third >= verbColMin {
		verbW = third
	}
	if room := width - gutter; verbW > room {
		verbW = room
	}
	if verbW < 0 {
		verbW = 0
	}
	// The accelerator outranks everything but the verb itself, because the
	// palette exists to teach it (5.22 rule 2). So the description yields
	// first, and then the VERB COLUMN NARROWS — down to verbColMin — before the
	// accelerator is given up. A palette that dropped the door it is teaching
	// in order to spell a verb in full has become a menu.
	right := 0
	if l.rightW > 0 {
		switch {
		case width >= gutter+verbW+rightGap+l.rightW:
			right = l.rightW
		default:
			if shrunk := width - gutter - rightGap - l.rightW; shrunk >= verbColMin {
				verbW, right = shrunk, l.rightW
			}
		}
	}
	lay := layout{verbW: verbW, descAt: gutter + verbW + descGap, rightW: right}
	lay.showDesc = width >= lay.descAt+descColMin+rightGap+lay.rightW
	return lay
}

// ruleGap is the space on either side of a header's hairline, so the rule never
// touches the word it separates from or the count it runs to.
const ruleGap = 1

// renderHeader draws one group name, its hairline, and its surviving count.
//
// The rule is the whole reason a header reads as a header here. Every column of
// this list is a grey (5.16 spends hue on meaning, and a group name means
// nothing), so a name drawn in the chrome tier among rows drawn in the primary
// tier is quieter than its rows but not structurally different from them — and
// with four groups on one sheet the reader needs the boundary, not just the
// word. 5.13 allows exactly one mark for this and names it: "hairline rules
// only at room boundaries", which is the same sentence 12.11 read to give the
// dialog its two rules. A group inside the sheet is the smaller boundary of the
// same kind, and it gets the same hairline — one line thick, drawn on the
// header's own row so it costs no line of its own, at the chrome tier so it
// recedes under everything it separates.
//
// The count keeps the right edge. It is what makes a filtered group legible as
// a filter ("actions 3" after typing) and it is the one number a group has.
func (l *list) renderHeader(sec section, width int) string {
	l.line.reset(width)
	l.line.add(sec.title(), tokens.TextTertiary)
	n := 0
	for i := range l.hits {
		if l.rows[l.hits[i].idx].sec == sec {
			n++
		}
	}
	count := strconv.Itoa(n)
	right := width - blocks.Width(count)
	if right <= l.line.w {
		return l.line.emit(&l.buf, l.profile, l.focus, width, false, tokens.Band, l.ground())
	}
	// The rule fills what is left between the name and the count. Under two
	// cells of gap there is no rule worth drawing and the gap stays whitespace,
	// which is the same structure spent more quietly.
	if span := right - l.line.w - 2*ruleGap; span > 0 {
		l.line.padTo(l.line.w + ruleGap)
		l.line.add(strings.Repeat(tokens.GlyphTreeDash, span), tokens.TextTertiary)
	}
	l.line.padTo(right)
	l.line.add(count, tokens.TextTertiary)
	return l.line.emit(&l.buf, l.profile, l.focus, width, false, tokens.Band, l.ground())
}

// renderRow draws one row: marker, glyph, verb, description, right column.
func (l *list) renderRow(r *row, selected bool, lay layout, width int) string {
	l.line.reset(width)
	verbTok, descTok, accelTok := rowTokens(r, selected)

	if selected {
		l.line.add(tokens.GlyphAccentRail, l.markerToken(r))
	}
	l.line.padTo(markerCol)
	if r.glyph != "" {
		l.line.add(r.glyph, r.glyphTok)
	}
	l.line.padTo(gutter)

	l.pos = appendPositions(l.pos[:0], r.lowerVerb, l.needle)
	l.line.addMatched(r.verb, verbTok, l.pos, lay.verbW)

	if lay.showDesc {
		// A disabled row spends its description column on the REASON (5.20
		// rule 3): "what does this do here" and "why can you not do it here"
		// are the same question, and the second answer is the more useful one.
		text, matched := r.desc, true
		if r.disabled != "" {
			text, matched = r.disabled, false
		}
		cells := width - lay.rightW - rightGap - lay.descAt
		if lay.rightW == 0 {
			cells = width - lay.descAt
		}
		// [layout] already guarantees cells >= descColMin whenever showDesc is
		// true; the check is the backstop that keeps a future column from
		// silently writing over the accelerator's reserved cells. The pad is
		// inside the guard so a row with no description leaves no trailing
		// whitespace behind it.
		if text != "" && cells > 0 {
			l.line.padTo(lay.descAt)
			if matched {
				l.pos = appendPositions(l.pos[:0], r.lowerDesc, l.needle)
				l.line.addMatched(text, descTok, l.pos, cells)
			} else {
				l.line.addMatched(text, descTok, nil, cells)
			}
		}
	}

	if lay.rightW > 0 {
		l.renderRight(r, accelTok, width, lay.rightW)
	}
	// The band is a background fill, and linear mode (10.1.5) is exactly where a
	// background fill is least trustworthy — a screen reader gets nothing from it
	// and a high-contrast terminal may render it as a block. The ▎ of [markerCol]
	// above already carries the same fact in a printable cell, so linear mode
	// keeps the marker and drops the fill rather than replacing one with the
	// other.
	return l.line.emit(&l.buf, l.profile, l.focus, width, selected && !l.linear, r.band, l.ground())
}

// markerToken tints the selection marker. A room carries its task's identity
// pastel (5.16: the identity hue answers "which room am I in" peripherally),
// and everything else carries the secondary grey — the same fallback
// [rail.View.gutter] makes, so the two surfaces mark a cursor the same way.
//
// The identity is dropped below 256 colours rather than approximated, because
// [tokens.Profile.IdentityDistinct] is explicit that a lying identity is worse
// than none: at 16 colours eight pastels collapse onto six slots, and a marker
// that told the reader two different rooms were the same task would be the
// affordance lying (5.20) in the one cell built to stop it.
func (l *list) markerToken(r *row) tokens.Token {
	if i, ok := tokens.IdentityIndex(r.band); ok && l.profile.IdentityDistinct() {
		return tokens.Identity(i)
	}
	return tokens.TextSecondary
}

// renderRight lays the count chip and the accelerator against the right edge.
func (l *list) renderRight(r *row, accelTok tokens.Token, width, rightW int) {
	accel := r.accel
	if r.disabled != "" {
		// A disabled affordance advertises no accelerator. Printing the key
		// beside a reason it cannot be used is the affordance lying (5.22 rule
		// 5), and it is the one lie this surface exists to stop telling.
		accel = ""
	}
	if r.questions == 0 && accel == "" {
		return
	}
	want := 0
	chip := ""
	if r.questions > 0 {
		chip = chipText(r.questions)
		want += blocks.Width(chip)
	}
	if accel != "" {
		if want > 0 {
			want++
		}
		want += blocks.Width(accel)
	}
	if want > rightW {
		want = rightW
	}
	start := width - want
	if start <= l.line.w {
		return
	}
	l.line.padTo(start)
	if chip != "" {
		l.line.add(chip, tokens.Amber)
		if accel != "" {
			l.line.add(" ", tokens.TextTertiary)
		}
	}
	l.line.add(accel, accelTok)
}

// rowTokens is the three-tier assignment, and the tier is spent on the COLUMN
// rather than on the cursor. That is the correction: 5.13's hierarchy is
// "primary for speech and TITLES, secondary for status lines, tertiary for
// telemetry", and a row here is exactly those three things left to right — the
// verb is the title, the description is the status line about it, the
// accelerator is the key it answers to. Drawn that way a reader scanning thirty
// rows sees three columns; drawn the old way — verb secondary, description and
// accelerator both tertiary — they saw two greys a shade apart, which is the
// "near-uniform grey with no hierarchy" this lane was opened for.
//
// Selection does not brighten the row, and that is 5.16 verbatim: "Selection is
// a background band, not a foreground color — text keeps its tier color." The
// band and the ▎ marker carry the cursor, and a row that also changed tier
// would be saying the same thing three times while destroying the column
// hierarchy on exactly the row the reader is looking hardest at.
//
// The ACCELERATOR is the one exception and it is 5.22's own checklist item:
// "an interactive chip may never live permanently in the dimmest tier — dim at
// rest, secondary on focus". It is also the thing this surface exists to teach,
// so the row under the cursor is where it is worth reading.
//
// A DIM GROUP (history, settings) is the whole row one tier down: reference
// material a reader scrolls to on purpose should not compete with the verbs the
// palette opened for.
//
// A DISABLED row stays tertiary in every column even under the selection band,
// because it is the one row here that is not interactive, and dimness is the
// truth about it rather than a decoration.
func rowTokens(r *row, selected bool) (verb, desc, accel tokens.Token) {
	if r.disabled != "" {
		return tokens.TextTertiary, tokens.TextTertiary, tokens.TextTertiary
	}
	verb, desc, accel = tokens.TextPrimary, tokens.TextSecondary, tokens.TextTertiary
	if r.sec.dim() {
		verb, desc = tokens.Demote(verb), tokens.Demote(desc)
	}
	if selected {
		accel = tokens.Promote(accel)
	}
	return verb, desc, accel
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
