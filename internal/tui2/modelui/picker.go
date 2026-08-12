package modelui

import (
	"image"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Geometry, stated here rather than discovered inside a render (10.5.24).
const (
	// gutter is the glyph column: one cell for the bound-model mark and one
	// space after it, so every verb starts at the same column whether or not
	// its row carries a glyph.
	gutter = 2
	// markerCol is the extra column linear mode (10.1.5) opens at the left edge
	// for [tokens.GlyphAccentRail]. It is not spent by default because the
	// gutter's one cell is already the bound-model ✓ and the band says which row
	// the cursor is on; linear mode has no band to say it with, so it buys the
	// column rather than overwriting a fact with a cursor. The width is fixed at
	// construction, so no row ever moves under a reader who is looking at it.
	markerCol = 1
	// linearGutter is the gutter that column produces.
	linearGutter = markerCol + gutter
	// verbColMin and verbColMax bound the name column. The floor is the
	// narrowest word that still reads once a long one has been given up
	// ("naming" is 6); the ceiling is set by the longest model word worth
	// spelling in full before the chip beside it starts losing cells.
	verbColMin = 6
	verbColMax = 24
	// descColMin is the narrowest description worth drawing. Under it the
	// column is dropped whole rather than shown as one word and a cut mark.
	descColMin = 8
	// descGap separates the name column from the description.
	descGap = 2
	// rightGap keeps the right column from touching the description, so the
	// two never read as one word.
	rightGap = 2
	// rightColMax caps the right column. A provenance word is never long; the
	// models level's receipt block is, because it is three columns
	// ("$3.00/$15.00 M  200K  aa 63.1"), and the cap is set to hold that whole
	// sentence rather than to trim it. It is a BACKSTOP and not the mechanism:
	// what actually fits is decided by the step ladder in [Picker.layout],
	// which gives the block up a column at a time instead of cutting a number
	// in half.
	rightColMax = 34
)

// Level is which of the two depths the picker is showing.
type Level uint8

const (
	// LevelRoles is the five rows (5.23). It is where the picker opens, always.
	LevelRoles Level = iota
	// LevelModels is one role's catalog, one level deeper.
	LevelModels
)

// Options configures the picker. Every field is optional and every omission
// degrades to something honest rather than to a panic.
type Options struct {
	// Styler paints every cell. A nil Styler renders plain text — the posture
	// every sibling package in this tree takes, and what the golden harness and
	// the tests want.
	Styler *tokens.Styler

	// Invalidate is [tui2.Shell.Invalidate]. The shell does NOT mark the frame
	// dirty when it hands a key to a pane, so a component that changed on a
	// keystroke must say so or the typed character never appears. Nil is legal
	// and means the caller repaints on its own clock.
	Invalidate func()

	// OnChoose receives the result of the row the user picked. Nil returns a
	// command emitting [ChooseMsg] instead, so a wiring built on messages and
	// one built on callbacks both work without an adapter.
	//
	// It is called BEFORE the close, so a handler that wants to read the
	// picker's state still can.
	OnChoose func(Result) tea.Cmd

	// OnClose is asked to drop the overlay plane — [tui2.Shell.SetOverlay] with
	// false, plus whatever focus the wiring wants restored. Nil emits
	// [CloseMsg].
	//
	// The picker never closes itself, because it does not own the plane it is
	// drawn on. It only ever says that it is finished.
	OnClose func() tea.Cmd

	// Linear is the accessible rendering (10.1.5). This surface is already one
	// column and already still — there is no motion here to reduce — so what it
	// changes is paint: the sheet's ground and the selection band are both
	// background fills, and both are dropped. The selection moves into a
	// printable cell instead, the ▎ of [markerCol], because a row told apart
	// from its neighbours only by a colour is a row a screen reader cannot
	// find.
	Linear bool
}

// ChooseMsg is emitted when a row is chosen and [Options.OnChoose] is nil.
type ChooseMsg struct{ Result Result }

// CloseMsg is emitted when the surface is finished and [Options.OnClose] is nil.
type CloseMsg struct{}

// Picker is the model palette (5.10: "the model chip … is interactive → model
// palette"). It is a [tui2.Pane] for the overlay layer, sized and raised by the
// shell exactly as the command palette and the settings sheet are; it never
// raises or drops that layer itself.
//
// Two levels, one gesture between them: five role rows, and one role's models
// beneath. It emits a [Result] and journals nothing.
type Picker struct {
	opts Options
	cat  Catalog

	level Level
	role  store.ModelRole

	// trail is the path ABOVE this surface — the palette row that opened it,
	// the settings sheet that opened it — root-most first, and empty for a
	// picker raised on its own. onBack leaves for one of those rungs. See
	// [Picker.SetBack].
	trail  []string
	onBack func(depth int) tea.Cmd

	rows    []row
	hits    []int32
	query   string
	needles []string

	cursor int
	top    int

	verbW  int
	rightW [receiptSteps]int
	// anyDesc says at least one row on this level has something to put in the
	// description column. Measured over the WHOLE level like the two widths
	// beside it, so a filter that happens to leave only wordless rows does not
	// move the columns under the reader's hand.
	anyDesc bool

	// bodyTop is how many chrome lines the last render drew above the list, so
	// a click can be translated into a row.
	bodyTop int
	// segs is where the last render put the trail's ancestor steps, so a click
	// on the header can be translated into a level.
	segs []segment
	// lastView is the body height the last render used, so a page key has a
	// page to move by.
	lastView int

	line   lineBuf
	buf    strings.Builder
	out    []string
	lineOf []int32
}

var (
	_ tui2.Pane      = (*Picker)(nil)
	_ tui2.PaneKeys  = (*Picker)(nil)
	_ tui2.PaneMouse = (*Picker)(nil)
)

// New builds a picker. It renders the five roles, honestly unbound, until a
// catalog arrives.
func New(opts Options) *Picker {
	p := &Picker{opts: opts}
	p.rebuild()
	return p
}

// SetCatalog installs the facts. Call it when they move — a binding is written,
// the scope changes, the model catalog finishes loading — and not per frame.
//
// The level survives: a catalog arriving while the user is looking at one
// role's models must not throw them back to the top, because a surface that
// reset itself when a fact refreshed would be unusable on a machine where facts
// refresh. The selection survives by RESULT, for the same reason the palette
// preserves its own — a row that moved because something finished elsewhere is
// the surface moving under the user's hand (7.2).
func (p *Picker) SetCatalog(c Catalog) {
	p.cat = c
	p.rebuild()
	p.invalidate()
}

// SetStyler rebinds the painter, for a terminal profile or a focus change.
func (p *Picker) SetStyler(s *tokens.Styler) {
	p.opts.Styler = s
	p.invalidate()
}

// Reset returns to the role level and clears the query. The wiring calls it
// when the palette opens: a palette that reopened three levels down, still
// holding the last search, is one whose first keystroke edits a query the user
// cannot see the origin of.
func (p *Picker) Reset() {
	p.level, p.role, p.query = LevelRoles, "", ""
	p.cursor, p.top = 0, 0
	// The trail's hit table goes with the level it described. Every render
	// rebuilds it, so this only matters for a pointer that arrives between the
	// reset and the next frame — but a click that walked back through a trail
	// nobody can see would be the surface moving on its own (7.2).
	p.segs = p.segs[:0]
	p.rebuild()
	p.invalidate()
}

// SetBack installs the path this surface was reached THROUGH, and the door back
// out of it. The wiring calls it on every raise, with a nil trail for a picker
// opened on its own.
//
// It is what makes this surface a rung of one drill rather than a second
// surface with its own private ladder. Reached from the palette the trail reads
// `all`; reached from a settings model row it reads `all ‧ settings`, and every
// one of those words is a door — clicked, or walked back to by backspace on an
// empty filter, exactly as the levels inside this picker already are.
//
// The trail is COPIED. A caller that reuses its own slice between raises would
// otherwise be rewriting a path this surface is still drawing.
func (p *Picker) SetBack(trail []string, onBack func(depth int) tea.Cmd) {
	p.trail = append(p.trail[:0], trail...)
	p.onBack = onBack
	p.invalidate()
}

// Trail is the path above this surface, for a caller that wants to read what it
// installed. It never includes this surface's own steps — those are [Level].
func (p *Picker) Trail() []string { return p.trail }

// Open shows one role's catalog straight away, which is what a settings row
// naming exactly one slot means when it is pressed.
//
// It used to be unreachable, and the seam said so in as many words: "the picker
// has no door to open AT one role, so a settings model row … lands them one
// keystroke above it". What a reader actually got was the five role rows — a
// list of slot words where they had asked for a list of models — which is the
// defect reported as "when I click to choose a model I don't get an OpenRouter
// list". One click is now one list: the models, with their price, window and
// score, and the role list reachable by ascending rather than by arriving.
//
// A role outside the ladder leaves the surface where it is: an unknown slot is
// not a role whose catalog could be shown, and descending into one would open a
// level with nothing in it and no honest word at its head.
func (p *Picker) Open(role store.ModelRole) {
	if !role.Valid() {
		return
	}
	p.descend(role)
}

// Level is which depth is showing.
func (p *Picker) Level() Level { return p.level }

// Role is the role whose models are showing, and is empty at [LevelRoles].
func (p *Picker) Role() store.ModelRole { return p.role }

// Query is the current filter text. It is always empty at [LevelRoles], which
// has five rows and no filter.
func (p *Picker) Query() string { return p.query }

// Count is how many rows survive the current filter.
func (p *Picker) Count() int { return len(p.hits) }

// Total is how many rows this level holds.
func (p *Picker) Total() int { return len(p.rows) }

// Selected is the result the current row would yield, and whether it would
// yield one at all. A role row yields none — it opens a level rather than
// finishing — and neither does a disabled row, which is the same answer the row
// is already giving on screen.
func (p *Picker) Selected() (Result, bool) {
	r, ok := p.selected()
	if !ok || !r.enabled() || r.result == nil {
		return nil, false
	}
	return r.result, true
}

func (p *Picker) selected() (*row, bool) {
	if p.cursor < 0 || p.cursor >= len(p.hits) {
		return nil, false
	}
	return &p.rows[p.hits[p.cursor]], true
}

// rebuild reconstructs this level's rows, preserving the selection by result.
func (p *Picker) rebuild() {
	prev, hadPrev := p.selectedResult()
	if p.level == LevelModels {
		p.rows = buildModels(p.rows, p.cat, p.role)
	} else {
		p.rows = buildRoles(p.rows, p.cat)
	}
	p.measure()
	p.refilter()
	if hadPrev {
		p.selectResult(prev)
	}
}

func (p *Picker) selectedResult() (Result, bool) {
	r, ok := p.selected()
	if !ok || r.result == nil {
		return nil, false
	}
	return r.result, true
}

func (p *Picker) selectResult(want Result) {
	for i := range p.hits {
		if p.rows[p.hits[i]].result == want {
			p.cursor = i
			return
		}
	}
}

// measure fixes the two column widths from the WHOLE level — not the filtered
// set — so typing narrows the list without shifting its columns.
func (p *Picker) measure() {
	p.verbW, p.rightW, p.anyDesc = 0, [receiptSteps]int{}, false
	for i := range p.rows {
		r := &p.rows[i]
		if w := blocks.Width(r.verb); w > p.verbW {
			p.verbW = w
		}
		if r.hasChip || r.hint != "" || r.disabled != "" {
			p.anyDesc = true
		}
		if r.disabled != "" {
			continue
		}
		for step := range r.right {
			if w := blocks.Width(r.right[step]); w > p.rightW[step] {
				p.rightW[step] = w
			}
		}
	}
	p.verbW = clampInt(p.verbW, verbColMin, verbColMax)
	for step := range p.rightW {
		if p.rightW[step] > rightColMax {
			p.rightW[step] = rightColMax
		}
	}
}

// refilter recomputes the surviving set. The role level has five rows and no
// filter, so its needle is always empty and every row survives.
func (p *Picker) refilter() {
	p.needles = strings.Fields(lower(p.query))
	p.hits = p.hits[:0]
	for i := range p.rows {
		if p.matches(&p.rows[i]) {
			p.hits = append(p.hits, int32(i))
		}
	}
	p.cursor, p.top = 0, 0
}

// matches is a case-insensitive test over everything the row knows itself by:
// the word it shows, the wiring's note, the provider id it will bind, and the
// provider's own display name. Every WHITESPACE-SEPARATED TERM must be found
// somewhere, in any of the four and in any order, so "claude opus" narrows to
// "anthropic/claude-opus-5" though no field spells it with a space.
//
// It is deliberately NOT the palette's fuzzy scorer, and the reason got
// stronger rather than weaker when this list went from four rows to four
// hundred. A model catalog is a list of near-identical strings —
// "claude-sonnet-4", "claude-sonnet-4.5", "claude-haiku-4.5" — where a
// subsequence match matches nearly everything and then has to RANK, which
// reorders the list under the reader's hand on almost every keystroke (7.2).
// Here the reader is narrowing toward a name they already half-know; the terms
// are the fuzziness, and the order stays put.
func (p *Picker) matches(r *row) bool {
	for _, needle := range p.needles {
		if !strings.Contains(r.lowerVerb, needle) &&
			!strings.Contains(r.lowerHint, needle) &&
			!strings.Contains(r.lowerSlug, needle) &&
			!strings.Contains(r.lowerName, needle) {
			return false
		}
	}
	return true
}

// Render implements [tui2.Pane]: a header, an optional honesty line, a blank,
// and the list.
//
// The blank is the first thing height pressure takes and the header is the
// last: a list with no header is a list whose level and scope the user cannot
// see, and this surface's whole risk is a user binding the wrong scope.
func (p *Picker) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	p.out = p.out[:0]
	p.out = append(p.out, p.header(width))
	if note := p.effortNote(); note != "" && height >= 4 {
		p.out = append(p.out, p.chromeLine(note, width))
	}
	if height >= 3 {
		p.out = append(p.out, blankLine(&p.line, &p.buf, p.profile(), p.focus(), width, p.ground()))
	}
	p.bodyTop = len(p.out)
	p.out = p.body(p.out, width, height-p.bodyTop)
	if len(p.out) > height {
		p.out = p.out[:height]
	}
	for len(p.out) < height {
		p.out = append(p.out, blankLine(&p.line, &p.buf, p.profile(), p.focus(), width, p.ground()))
	}
	return strings.Join(p.out, "\n")
}

// Header copy. The level's name, the scope it governs, and the exit — every
// overlay advertises its own door (5.22).
const (
	rolesLabel        = "model roles"
	searchLabel       = "search"
	searchPlaceholder = "type to filter"
)

// closeChip is the one door this surface advertises, as a verb·key chip (§16
// THE VERB·KEY CHIP) rather than as the two flat words it used to be.
//
// It reads "close esc", and the order is half the point: this header used to
// say `esc close`, which is a key and a verb in the same tier with nothing
// telling a reader which is which — the exact bug internal/registry's chip.go
// was written to end. The words come from [registry.ChipFor] and the paint is
// this package's, which is the split that keeps one grammar across surfaces
// without dragging a renderer into the catalog.
//
// There used to be a second chip beside it, `back esc`, drawn on the models
// level only. It is gone with the meaning it advertised: esc closes from either
// level now, so the same key can no longer be printed with two different
// promises. What replaced it is the trail, which says where back GOES rather
// than only that it exists (§15) — and, being a click target, is also the door
// rather than a label about one.
var closeChip = registry.ChipFor("close", "esc")

// scopeSeparator joins the level's name to the scope it governs.
const scopeSeparator = " " + tokens.GlyphSeparator + " "

// THE DRILL TRAIL — internal/tui2/palette/trail.go's grammar, arriving on the
// other palette in the product. Read that file for the whole argument; the
// short of it is that a surface you can enter and not leave is a trap, that the
// path itself is the way out (every ancestor step a door, the current step a
// label), and that ESC is not part of the ladder: it closes the surface from
// any depth, because 8.2.21 is that esc acts on what you are WATCHING, and two
// keys with one meaning between them is how "press esc until something happens"
// gets learned. Backspace on an empty query is what walks a rung back.
//
// The arithmetic is copied rather than imported for line.go's reason and no
// other: both halves are unexported in their own packages, and a third package
// existing to hold twenty lines of path arithmetic would put a public API in
// front of something no consumer of this tree should call. If a shared trail is
// ever factored out, this is the second call site that moves.
const (
	// trailLead opens each segment. It is [tokens.GlyphPromptChat], the same
	// mark the composer draws to mean "and then", which is exactly what a step
	// of a path means. The space is part of the lead so a segment never touches
	// its own mark.
	trailLead = tokens.GlyphPromptChat + " "
	// trailSep parts one drawn segment from the next.
	trailSep = " "
)

// headerLine is the row the trail is drawn on. It is named because the pointer
// has to find it and [Picker.Render] has to draw it, and a 0 written twice is a
// 0 that can drift.
const headerLine = 0

// segment is one drawn step of the trail: where a click on it goes, and the
// cells it occupied in the last render. The columns are RECORDED at paint time
// rather than recomputed, because a hit test that measured the string again
// would be a second opinion about where the words are (trail.go).
//
// back is which rung of [Picker.trail] the step returns to, and -1 means the
// step is one of THIS surface's own levels. One field rather than two tables
// because the path is one path: the reader walking it does not know or care
// which of its words this component owns.
type segment struct {
	back  int
	level Level
	at    int
	w     int
}

// inSurface marks a step that ascends inside this picker rather than leaving it.
const inSurface = -1

// hitAt maps a column on the header row back to the step a click there means.
// The CURRENT step is never in the table — clicking where you already are must
// do nothing rather than rebuild the list under the pointer.
func hitAt(segs []segment, x int) (segment, bool) {
	for i := range segs {
		if s := segs[i]; x >= s.at && x < s.at+s.w {
			return s, true
		}
	}
	return segment{}, false
}

// addTrail writes where the reader is standing at the head of the header, and
// records the cells each ANCESTOR step landed on so a click can be turned back
// into a level.
//
// An undrilled roles level draws its bare label and records nothing: there is
// nothing above it to walk back to, and a one-step path is a label pretending to
// be navigation (trail.go). The models level draws both steps — `› model roles ›
// execution` — and every step above the picker is drawn the same way, because it
// is the same path. The tiers split them by what they ARE: an ancestor is a door
// and sits at the secondary tier, the current step opens nothing and sits at the
// dimmest. That split is 5.22's checklist rather than a preference — an
// interactive chip may never live permanently in the dimmest tier.
func (p *Picker) addTrail(l *lineBuf) {
	p.segs = p.segs[:0]
	// The steps ABOVE this surface first — the palette, the settings sheet —
	// each one a door out of the picker entirely ([Picker.SetBack]). They are
	// drawn exactly like the picker's own ancestors because they are ancestors:
	// a path that changed its mark halfway would be two paths.
	for depth, word := range p.trail {
		if word == "" {
			continue
		}
		l.add(trailLead, tokens.TextTertiary)
		at := l.w
		l.add(word, tokens.TextSecondary)
		if w := l.w - at; w > 0 {
			p.segs = append(p.segs, segment{back: depth, at: at, w: w})
		}
		l.add(trailSep, tokens.TextTertiary)
	}
	if p.level != LevelModels {
		if len(p.segs) == 0 {
			// Nothing above it and nothing below: a one-step path is a label
			// pretending to be navigation, so it is drawn as the label it is.
			l.add(rolesLabel, tokens.TextSecondary)
			return
		}
		l.add(trailLead, tokens.TextTertiary)
		l.add(rolesLabel, tokens.TextTertiary)
		return
	}
	l.add(trailLead, tokens.TextTertiary)
	at := l.w
	l.add(rolesLabel, tokens.TextSecondary)
	// A step clipped by a narrow header is still the cells it drew, and still a
	// door on them. A step that drew nothing at all is not recorded, because a
	// zero-width target is one a pointer can only hit by arithmetic accident.
	if w := l.w - at; w > 0 {
		p.segs = append(p.segs, segment{back: inSurface, level: LevelRoles, at: at, w: w})
	}
	l.add(trailSep+trailLead, tokens.TextTertiary)
	l.add(RoleWord(p.role), tokens.TextTertiary)
}

// header draws the trail, the scope that trail's choice will land in, and — on
// the models level — the filter and the count.
//
// The scope is on BOTH levels, always: "which role" and "how far" are the two
// questions a wrong choice here answers wrongly, and the second one is the one
// no row states for itself.
func (p *Picker) header(width int) string {
	l := &p.line
	l.reset(width)
	p.addTrail(l)
	l.add(scopeSeparator, tokens.TextTertiary)
	l.add(p.cat.scopeWord(), tokens.TextTertiary)
	if p.level == LevelRoles {
		// No count here: the roles level is five rows by construction, and a
		// "5" beside a list of five is a cell spent saying what the reader can
		// already see.
		p.addTailChip(l, width, "", closeChip)
		return l.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, p.ground())
	}
	l.add("  ", tokens.TextTertiary)
	l.add(searchLabel, tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	if p.query == "" {
		l.add(searchPlaceholder, tokens.TextTertiary)
	} else {
		l.add(p.query, tokens.TextPrimary)
	}
	count := strconv.Itoa(len(p.hits)) + "/" + strconv.Itoa(len(p.rows))
	p.addTailChip(l, width, count+scopeSeparator, closeChip)
	return l.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, p.ground())
}

// addTailChip right-aligns a lead and a verb·key chip, and falls back to the
// lead alone when the chip will not fit beside it.
//
// The fallback drops the CHIP and keeps the count, which is the opposite of
// what a header usually gives up, and it is right here: the count is a fact
// only this line can state, while the door it advertises is esc — a key every
// overlay in the product answers, and one the reader will find without ink
// (5.22). The chip is a teaching aid; the count is information.
func (p *Picker) addTailChip(l *lineBuf, width int, lead string, chip registry.Chip) {
	if start := width - blocks.Width(lead+chip.String()); start > l.w+1 {
		l.padTo(start)
		if lead != "" {
			l.add(lead, tokens.TextTertiary)
		}
		// Verb first at the brighter tier, key after it one tier down. Two
		// tiers is what makes the pair parseable without being read twice.
		l.add(chip.Verb, tokens.TextSecondary)
		if chip.Key != "" {
			l.add(registry.ChipGap+chip.Key, tokens.TextTertiary)
		}
		return
	}
	p.addTail(l, width, strings.TrimSuffix(lead, scopeSeparator))
}

// addTail right-aligns the first candidate that fits with a space to spare, and
// draws nothing when none does. Candidates are in preference order.
func (p *Picker) addTail(l *lineBuf, width int, candidates ...string) {
	for _, tail := range candidates {
		if start := width - blocks.Width(tail); start > l.w+1 {
			l.padTo(start)
			l.add(tail, tokens.TextTertiary)
			return
		}
	}
}

// effortNote is the one sentence this surface says about reasoning effort, and
// it is a refusal (12.3.5).
//
// Effort has no journal axis: it is encoded inside the model slug, so there is
// nothing to bind it to and no control that could change it without changing
// the slug. The chip DISPLAYS it. This line says why there is no control, on
// the level where a user would look for one, and only when the current binding
// actually carries an effort — a note about a knob nobody was looking for is
// noise.
func (p *Picker) effortNote() string {
	if p.level != LevelModels {
		return ""
	}
	word := Effort(p.cat.roleRow(p.role).Model)
	if word == "" {
		return ""
	}
	return "effort " + word + " rides the slug — shown, not set"
}

func (p *Picker) chromeLine(text string, width int) string {
	p.line.reset(width)
	p.line.add(blocks.Truncate(text, width), tokens.TextTertiary)
	return p.line.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, p.ground())
}

// emptyText is what a level with nothing on it says. Only the models level can
// be empty — the roles level is five rows by construction — and the two reasons
// it can be are different facts that must not share a sentence (5.22 rule 6).
func (p *Picker) emptyText() string {
	if p.query != "" {
		return "no model matches " + p.query
	}
	return "no models to offer yet"
}

// body draws the list at width by height, appending into the caller's slice so
// a frame costs no second allocation.
func (p *Picker) body(rows []string, width, height int) []string {
	p.lineOf = p.lineOf[:0]
	if width <= 0 || height <= 0 {
		return rows
	}

	if len(p.hits) == 0 {
		p.line.reset(width)
		p.line.add(blocks.Truncate(p.emptyText(), width), tokens.TextTertiary)
		p.lineOf = append(p.lineOf, -1)
		return append(rows, p.line.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, p.ground()))
	}

	p.clampCursor()
	// The overflow cue costs a line, so the body is measured before the scroll
	// is computed — a window sized against a height the cue then takes back is
	// a window whose last row is always the one the reader wanted. This is the
	// command palette's arithmetic, verbatim, because it is the same list.
	body := height
	if len(p.hits) > height && height > 1 {
		body = height - 1
	}
	p.lastView = body
	p.scroll(body)
	lay := p.layout(width)
	shown := 0
	for i := p.top; i < len(p.hits) && shown < body; i++ {
		r := &p.rows[p.hits[i]]
		rows = append(rows, p.renderRow(r, i == p.cursor, lay, width))
		p.lineOf = append(p.lineOf, int32(i))
		shown++
	}
	if hidden := len(p.hits) - shown; hidden > 0 && shown < height {
		rows = append(rows, p.moreLine(hidden, width))
		p.lineOf = append(p.lineOf, -1)
	}
	return rows
}

// moreLine is the honest bottom of a clipped list (§9): how many surviving rows
// are off screen, above and below taken together.
//
// It matters more here than anywhere else in the product. This list is four
// hundred rows long and the overlay shows a dozen; a picker that drew twelve
// models with no mark would not be a short list, it would be a list claiming
// the provider publishes twelve models — which is the exact complaint this
// surface was rebuilt to answer.
func (p *Picker) moreLine(hidden int, width int) string {
	p.line.reset(width)
	p.line.padTo(p.gutter())
	p.line.add(strconv.Itoa(hidden)+" more", tokens.TextTertiary)
	return p.line.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, p.ground())
}

func (p *Picker) clampCursor() {
	if len(p.hits) == 0 {
		p.cursor = 0
		return
	}
	p.cursor = clampInt(p.cursor, 0, len(p.hits)-1)
}

// scroll moves the window so the cursor is on screen.
func (p *Picker) scroll(height int) {
	if p.top > p.cursor {
		p.top = p.cursor
	}
	if p.cursor >= p.top+height {
		p.top = p.cursor - height + 1
	}
	if max := len(p.hits) - height; p.top > max {
		p.top = max
	}
	if p.top < 0 {
		p.top = 0
	}
}

// layout resolves the three columns for one width.
type layout struct {
	// gutter is [Picker.gutter] for this render, carried on the layout so every
	// column below it is measured against the same left edge — the marker column
	// linear mode adds shifts the whole row, and a row that shifted for one
	// arithmetic and not another would be two layouts.
	gutter   int
	verbW    int
	descAt   int
	rightW   int
	showDesc bool
	// step is which of a row's [receiptSteps] right-column spellings this
	// width can afford, widest first.
	step int
}

// layout gives up the columns in a stated order, and the order is the opposite
// of the command palette's on purpose.
//
// THE DESCRIPTION COLUMN OUTRANKS EVERYTHING HERE, because on a role row it
// carries the CHIP — the model itself. It outranks the provenance column (a row
// that dropped the model in order to say "global" would be answering "where
// from" about a value it is no longer showing) and it outranks the full
// spelling of the role word, which narrows to verbColMin to make room. Only
// when even that fails does the description go, and then the provenance takes
// the space it can: with no model on the row, where the binding came from is
// the last true thing left to say.
func (p *Picker) layout(width int) layout {
	left := p.gutter()
	verbW := clampInt(p.verbW, verbColMin, verbColMax)
	if room := width - left; verbW > room {
		verbW = room
	}
	if verbW < 0 {
		verbW = 0
	}
	if need := left + descGap + descColMin; verbW > verbColMin &&
		width >= need+verbColMin && width < need+verbW {
		verbW = width - need
	}

	lay := layout{gutter: left, verbW: verbW, descAt: left + verbW + descGap}
	// A description column exists when there is a description to put in it. A
	// bare catalog — four hundred model words, no notes, no chips — used to
	// reserve eight cells for prose that was never coming and then refuse the
	// price block for want of exactly those cells (§16 EMPTINESS, applied to a
	// column rather than to a value).
	lay.showDesc = p.anyDesc && width >= lay.descAt+descColMin
	// The receipt block gives itself up a COLUMN at a time (see
	// [fillReceipts]), widest step first, and only vanishes when even its
	// narrowest spelling has nowhere to go. A block that truncated instead
	// would cut a price in half, and half a price is worse than no price.
	for step := 0; step < receiptSteps; step++ {
		want := p.rightW[step]
		if want == 0 {
			continue
		}
		room := left + verbW
		if lay.showDesc {
			room = lay.descAt + descColMin
		}
		if width >= room+rightGap+want {
			lay.rightW, lay.step = want, step
			break
		}
	}
	return lay
}

// renderRow draws one row: glyph, name, description (or chip, or reason), right
// column.
func (p *Picker) renderRow(r *row, selected bool, lay layout, width int) string {
	l := &p.line
	l.reset(width)
	verbTok, descTok, rightTok := rowTokens(r, selected)

	if p.opts.Linear {
		// Linear mode has no band, so the cursor is a cell rather than a colour
		// (5.17: every state a colour carries also has a glyph). It is the ▎ of
		// [markerCol] — the same mark the rail and the command palette put in
		// front of a selected row, so the three surfaces mark a cursor the same
		// way — and it is drawn before the gutter rather than inside it, because
		// the gutter's cell already belongs to the bound-model ✓.
		if selected {
			l.add(tokens.GlyphAccentRail, tokens.TextSecondary)
		}
		l.padTo(markerCol)
	}
	if r.glyph != "" {
		l.add(r.glyph, r.glyphTok)
	}
	l.padTo(lay.gutter)

	at, n := matchAt(r.lowerVerb, p.needles)
	l.addMatched(r.verb, verbTok, at, n, lay.verbW)

	if lay.showDesc {
		cells := width - lay.descAt
		if lay.rightW > 0 {
			cells = width - lay.rightW - rightGap - lay.descAt
		}
		if cells > 0 {
			l.padTo(lay.descAt)
			switch {
			case r.disabled != "":
				// A disabled row spends its description column on the REASON
				// (5.20 rule 3): "what does this do" and "why can you not do it
				// here" are the same question, and the second answer is the more
				// useful one.
				l.addMatched(r.disabled, tokens.TextTertiary, -1, 0, cells)
			case r.hasChip:
				// The chip first, then the role's own sentence in whatever
				// cells are left. The hint is the teaching half of 5.22 rule 6
				// and the chip is the fact, so the fact never yields to it: on
				// a narrow overlay the hint simply is not there.
				p.addChip(l, r.chip, cells, selected)
				if left := lay.descAt + cells - l.w - descGap; left >= descColMin {
					l.add(spaces(descGap), tokens.TextTertiary)
					l.addMatched(r.hint, tokens.TextTertiary, -1, 0, left)
				}
			default:
				l.addMatched(r.hint, descTok, -1, 0, cells)
			}
		}
	}

	if right := r.right[lay.step]; lay.rightW > 0 && r.disabled == "" && right != "" {
		want := blocks.Width(right)
		if want > lay.rightW {
			want = lay.rightW
		}
		if start := width - want; start > l.w {
			l.padTo(start)
			l.add(right, rightTok)
		}
	}
	// The band is a background fill, and linear mode (10.1.5) is exactly where a
	// background fill is least trustworthy — a screen reader gets nothing from it
	// and a high-contrast terminal may render it as a block. The marker above
	// carries the same fact in a printable cell, so linear mode keeps the marker
	// and drops the fill rather than replacing one with the other.
	return l.emit(&p.buf, p.profile(), p.focus(), width, selected && !p.opts.Linear, tokens.Band, p.ground())
}

// addChip folds a role row's chip into the line. The chip keeps its own tokens
// — it is the same component the status line and the rail draw, and a picker
// that recoloured it would be a second opinion about what a chip looks like —
// except under the selection band, where every cell brightens one tier the way
// the rest of the row does.
func (p *Picker) addChip(l *lineBuf, chip Chip, cells int, selected bool) {
	room := l.room()
	if cells > 0 && cells < room {
		room = cells
	}
	if room <= 0 {
		return
	}
	for _, s := range chip.Spans(room) {
		tok := s.Token
		if selected {
			tok = tokens.Promote(tok)
		}
		l.add(s.Text, tok)
	}
}

// rowTokens is the three-tier assignment. A choosable row is secondary at rest
// and PRIMARY when selected; a DISABLED row stays tertiary in every column even
// under the band, because it is the one row here that is not interactive, and
// dimness is the truth about it rather than a decoration.
func rowTokens(r *row, selected bool) (verb, desc, right tokens.Token) {
	if r.disabled != "" {
		return tokens.TextTertiary, tokens.TextTertiary, tokens.TextTertiary
	}
	verb, desc, right = tokens.TextSecondary, tokens.TextTertiary, tokens.TextTertiary
	if selected {
		verb, desc, right = tokens.Promote(verb), tokens.Promote(desc), tokens.Promote(right)
	}
	return verb, desc, right
}

// matchAt finds the first term that appears in an already-lowered haystack, and
// returns the byte offset and length of the run to brighten. A rune boundary is
// checked because the offsets index the original string: highlighting from the
// middle of a multi-byte character would emit an escape sequence inside it,
// which is a broken cell rather than a bright one.
//
// ONE run is painted, not one per term. Two bright fragments in a nine-letter
// word is a word wearing camouflage, and the highlight's job is to answer "why
// did this row survive" at a glance rather than to be exhaustive — a term that
// matched the id or the display name is not on the row at all, and is not
// paintable anywhere.
func matchAt(haystack string, needles []string) (int, int) {
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		at := strings.Index(haystack, needle)
		if at < 0 || !utf8.RuneStart(haystack[at]) {
			continue
		}
		return at, len(needle)
	}
	return -1, 0
}

// Key implements [tui2.PaneKeys].
//
// esc is consumed unconditionally and CLOSES, from either level. Nothing below
// the overlay hears it — an open overlay IS what the user is watching, which is
// also why it does not unwind a rung: what a reader watching a drilled picker
// wants gone is the picker (trail.go, where the product settled this for every
// palette at once). This surface used to spend esc on the ladder and print two
// different promises on one key to explain it.
//
// The ladder is walked back by three things instead, each answering a different
// reader: BACKSPACE on an empty filter, a click on the trail's ancestor step,
// and left — the arrow that already meant "out of this" opposite the right that
// means "into it".
//
// Everything that is not a binding is text, and only at the models level. The
// roles level has five rows and no filter, so a stray letter there types
// nothing rather than opening a search box over a list that does not need one.
func (p *Picker) Key(msg tea.KeyPressMsg) tea.Cmd {
	switch s := msg.String(); s {
	case "esc":
		return p.close()
	case "enter":
		return p.activate()
	case "right":
		if p.level == LevelRoles {
			return p.activate()
		}
	case "left":
		if p.level == LevelModels {
			p.ascend()
			return nil
		}
		return p.back()
	case "ctrl+u":
		p.setQuery("")
	case "ctrl+w":
		p.setQuery(dropWord(p.query))
	case "backspace":
		// Empty-then-back is the same rule a shell path prompt and every file
		// picker already teach, and it costs no chord nobody knows. With text in
		// the filter it edits the text, unchanged: a key that sometimes deleted
		// a letter and sometimes threw away the list would make typing feel
		// dangerous (trail.go).
		if p.level == LevelModels && p.query == "" {
			p.ascend()
			return nil
		}
		if p.level == LevelRoles {
			// The top of THIS surface is not the top of the path when something
			// opened it. One more rung back leaves the picker for whatever did.
			return p.back()
		}
		p.setQuery(dropRune(p.query))
	default:
		if p.navigate(s) {
			return nil
		}
		if p.level == LevelModels {
			if text := msg.Key().Text; text != "" {
				p.setQuery(p.query + text)
			}
		}
	}
	return nil
}

// Mouse implements [tui2.PaneMouse]: a wheel notch scrolls the selection, a
// left click on a row selects and activates it (5.22 rule 5 — the row IS the
// button), and a click on an ancestor step of the header's trail goes back to
// that level.
//
// The header is a target now, and only because the trail is on it: this surface
// had NO pointer path out of a drilled level at all, which is the trap trail.go
// describes with a mouse in your hand. Every other cell of chrome still does
// nothing, rather than something arbitrary — including the step the reader is
// already standing on, which is not in the hit table.
func (p *Picker) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelUp:
			p.moved(p.move(-wheelStep))
		case tea.MouseWheelDown:
			p.moved(p.move(wheelStep))
		}
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		if local.Y == headerLine {
			// Only ancestors are ever recorded, so a hit IS a step back — the
			// question is only how far. A step this surface owns ascends a
			// level; a step above it leaves for the rung that opened us.
			if seg, hit := hitAt(p.segs, local.X); hit {
				if seg.back == inSurface {
					p.ascend()
					return nil
				}
				return p.backTo(seg.back)
			}
			return nil
		}
		y := local.Y - p.bodyTop
		if y < 0 || y >= len(p.lineOf) || p.lineOf[y] < 0 {
			return nil
		}
		p.moved(p.selectHit(int(p.lineOf[y])))
		return p.activate()
	}
	return nil
}

// wheelStep is how far a wheel notch moves the selection — the same three rows
// the transcript and the palette use, so the panes do not disagree about what a
// wheel means.
const wheelStep = 3

// navigate handles the movement vocabulary and reports whether it consumed the
// key. It does not wrap: a list whose end is not felt is a list whose length is
// not known.
func (p *Picker) navigate(key string) bool {
	switch key {
	case "up", "ctrl+p", "shift+tab":
		p.moved(p.move(-1))
	case "down", "ctrl+n", "tab":
		p.moved(p.move(1))
	case "pgup":
		p.moved(p.page(-1))
	case "pgdown":
		p.moved(p.page(1))
	case "home":
		p.moved(p.move(-len(p.hits)))
	case "end":
		p.moved(p.move(len(p.hits)))
	default:
		return false
	}
	return true
}

// activate is what enter and a click do: open a role, or choose a model.
//
// A DISABLED row does neither and does not close. The user asked for something
// this scope cannot do, and the honest answer is the reason still on screen
// beside it (5.20 rule 3) — not a surface that vanishes as if it had worked.
func (p *Picker) activate() tea.Cmd {
	r, ok := p.selected()
	if !ok || !r.enabled() {
		return nil
	}
	if r.descend {
		p.descend(r.role)
		return nil
	}
	if r.result == nil {
		return nil
	}
	return tea.Batch(p.emit(r.result), p.close())
}

// descend opens one role's catalog, with the cursor on the model the role is
// running RIGHT NOW.
//
// Four rows deep this was nothing; four hundred rows deep it is the difference
// between a list and a decision. The reader came here from a chip showing what
// they are on, and the question they arrived with is "what else" — which is
// asked from where they are, not from the top of an alphabet. It is also the
// only way the ✓ is visible at all without scrolling, and a mark nobody ever
// sees is a mark that is not doing its job.
func (p *Picker) descend(role store.ModelRole) {
	p.level, p.role, p.query = LevelModels, role, ""
	p.cursor, p.top = 0, 0
	p.rebuild()
	for i := range p.hits {
		if p.rows[p.hits[i]].bound {
			p.cursor = i
			break
		}
	}
	p.invalidate()
}

// ascend returns to the five, with the role that was open selected — the user
// came from that row and that is where their eye is.
//
// That IS the reader's place restored, not an approximation of it: popping is a
// RETURN and not a fresh open (trail.go's level keeps a query, a cursor and a
// scroll for exactly this), and here the whole of the roles level's place is
// that one row. There is no query to bring back because the roles level never
// has one ([Picker.setQuery] refuses it), and no scroll to bring back because
// five rows and a cursor decide the window in [Picker.scroll]. So nothing is
// stashed: the place is derivable from p.role, and a saved copy of a derivable
// fact is a second opinion waiting to disagree with it.
func (p *Picker) ascend() {
	role := p.role
	p.level, p.role, p.query = LevelRoles, "", ""
	p.cursor, p.top = 0, 0
	p.rebuild()
	for i := range p.hits {
		if p.rows[p.hits[i]].role == role {
			p.cursor = i
			break
		}
	}
	p.invalidate()
}

// back leaves the picker for the rung directly above it — one step of the same
// ladder [Picker.ascend] walks, taken when there is no level left to ascend to.
// With nothing above, it does nothing and says so by returning no command,
// which is what lets the key fall through without a second test.
func (p *Picker) back() tea.Cmd { return p.backTo(len(p.trail) - 1) }

// backTo leaves for a stated rung, which is what a click on an ancestor word
// means. The rung is the wiring's to raise: this package holds words and never
// learns what they open.
func (p *Picker) backTo(depth int) tea.Cmd {
	if p.onBack == nil || depth < 0 || depth >= len(p.trail) {
		return nil
	}
	return p.onBack(depth)
}

func (p *Picker) setQuery(q string) {
	if p.level != LevelModels || q == p.query {
		return
	}
	p.query = q
	p.refilter()
	p.invalidate()
}

func (p *Picker) move(delta int) bool {
	if len(p.hits) == 0 {
		return false
	}
	next := clampInt(p.cursor+delta, 0, len(p.hits)-1)
	if next == p.cursor {
		return false
	}
	p.cursor = next
	return true
}

func (p *Picker) page(dir int) bool {
	step := p.lastView
	if step < 1 {
		step = 1
	}
	return p.move(dir * step)
}

func (p *Picker) selectHit(i int) bool {
	if i < 0 || i >= len(p.hits) || i == p.cursor {
		return false
	}
	p.cursor = i
	return true
}

func (p *Picker) emit(res Result) tea.Cmd {
	if p.opts.OnChoose != nil {
		return p.opts.OnChoose(res)
	}
	return func() tea.Msg { return ChooseMsg{Result: res} }
}

func (p *Picker) close() tea.Cmd {
	if p.opts.OnClose != nil {
		return p.opts.OnClose()
	}
	return func() tea.Msg { return CloseMsg{} }
}

func (p *Picker) moved(changed bool) {
	if changed {
		p.invalidate()
	}
}

func (p *Picker) invalidate() {
	if p.opts.Invalidate != nil {
		p.opts.Invalidate()
	}
}

func (p *Picker) profile() tokens.Profile {
	if p.opts.Styler == nil {
		return tokens.NoColor
	}
	return p.opts.Styler.Profile()
}

func (p *Picker) focus() tokens.Focus {
	if p.opts.Styler == nil {
		return tokens.FocusNormal
	}
	return p.opts.Styler.Focus()
}

// ground is the floor this surface paints under everything that is not banded.
//
// Linear mode (10.1.5) gets none, and for the same reason it gets no selection
// band: a background fill is what a screen reader cannot see and what a
// high-contrast terminal may render as a solid block. Nothing is lost — the
// ground carries no information, only elevation — which is exactly why it is
// the part that yields.
func (p *Picker) ground() tokens.Token {
	if p.opts.Linear {
		return tokens.Ground
	}
	return sheetGround
}

// gutter is the left column this render spends before the name: the glyph, plus
// the selection marker linear mode adds to it.
func (p *Picker) gutter() int {
	if p.opts.Linear {
		return linearGutter
	}
	return gutter
}

// dropRune removes the last rune of s. Dropping a byte would corrupt a pasted
// accented word into a sequence the filter could then never match.
func dropRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

// dropWord removes the trailing run of non-spaces, and any spaces before it.
func dropWord(s string) string {
	end := len(s)
	for end > 0 && s[end-1] == ' ' {
		end--
	}
	for end > 0 && s[end-1] != ' ' {
		end--
	}
	return s[:end]
}
