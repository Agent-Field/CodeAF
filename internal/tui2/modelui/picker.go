package modelui

import (
	"image"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

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
	// verbColMin and verbColMax bound the name column. The floor is the
	// narrowest role word that still reads ("voice" is 5, "hands" is 5); the
	// ceiling is set by the longest model word worth spelling in full before
	// the chip beside it starts losing cells.
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
	// rightColMax caps the right column: a provenance word or a context count,
	// neither of which is ever long.
	rightColMax = 10
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

	rows   []row
	hits   []int32
	query  string
	needle string

	cursor int
	top    int

	verbW  int
	rightW int

	// bodyTop is how many chrome lines the last render drew above the list, so
	// a click can be translated into a row.
	bodyTop int
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
	p.rebuild()
	p.invalidate()
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
	p.verbW, p.rightW = 0, 0
	for i := range p.rows {
		r := &p.rows[i]
		if w := blocks.Width(r.verb); w > p.verbW {
			p.verbW = w
		}
		if r.disabled != "" {
			continue
		}
		if w := blocks.Width(r.right); w > p.rightW {
			p.rightW = w
		}
	}
	p.verbW = clampInt(p.verbW, verbColMin, verbColMax)
	if p.rightW > rightColMax {
		p.rightW = rightColMax
	}
}

// refilter recomputes the surviving set. The role level has five rows and no
// filter, so its needle is always empty and every row survives.
func (p *Picker) refilter() {
	p.needle = lower(p.query)
	p.hits = p.hits[:0]
	for i := range p.rows {
		if p.matches(&p.rows[i]) {
			p.hits = append(p.hits, int32(i))
		}
	}
	p.cursor, p.top = 0, 0
}

// matches is a contiguous, case-insensitive substring test over the row's name
// and its description.
//
// It is deliberately NOT the palette's fuzzy scorer. A model catalog is a list
// of near-identical strings — "claude-sonnet-4", "claude-sonnet-4.5",
// "claude-haiku-4.5" — where a subsequence match reorders rows on almost every
// keystroke and a substring match does not. Here the user is narrowing a name
// they already know, not discovering a verb, and that is a different search.
func (p *Picker) matches(r *row) bool {
	if p.needle == "" {
		return true
	}
	return strings.Contains(r.lowerVerb, p.needle) ||
		strings.Contains(r.lowerHint, p.needle)
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
		p.out = append(p.out, blankLine(&p.line, &p.buf, p.profile(), p.focus(), width))
	}
	p.bodyTop = len(p.out)
	p.out = p.body(p.out, width, height-p.bodyTop)
	if len(p.out) > height {
		p.out = p.out[:height]
	}
	for len(p.out) < height {
		p.out = append(p.out, blankLine(&p.line, &p.buf, p.profile(), p.focus(), width))
	}
	return strings.Join(p.out, "\n")
}

// Header copy. The level's name, the scope it governs, and the exit — every
// overlay advertises its own door (5.22).
const (
	rolesLabel        = "model roles"
	searchLabel       = "search"
	searchPlaceholder = "type to filter"
	escClose          = "esc close"
	escBack           = "esc back"
)

// scopeSeparator joins the level's name to the scope it governs.
const scopeSeparator = " " + tokens.GlyphSeparator + " "

func (p *Picker) header(width int) string {
	l := &p.line
	l.reset(width)
	if p.level == LevelRoles {
		l.add(rolesLabel, tokens.TextSecondary)
		l.add(scopeSeparator, tokens.TextTertiary)
		l.add(p.cat.scopeWord(), tokens.TextTertiary)
		// No count here: the roles level is five rows by construction, and a
		// "5" beside a list of five is a cell spent saying what the reader can
		// already see.
		p.addTail(l, width, escClose)
		return l.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, sheetGround)
	}
	// The models level names the role it is binding and the scope that binding
	// lands in, in that order. Both, always: "which role" and "how far" are the
	// two questions a wrong choice here answers wrongly.
	l.add(p.role.Word(), tokens.TextSecondary)
	l.add(scopeSeparator, tokens.TextTertiary)
	l.add(p.cat.scopeWord(), tokens.TextTertiary)
	l.add("  ", tokens.TextTertiary)
	l.add(searchLabel, tokens.TextTertiary)
	l.add(" ", tokens.TextTertiary)
	if p.query == "" {
		l.add(searchPlaceholder, tokens.TextTertiary)
	} else {
		l.add(p.query, tokens.TextPrimary)
	}
	count := strconv.Itoa(len(p.hits)) + "/" + strconv.Itoa(len(p.rows))
	p.addTail(l, width, count+scopeSeparator+escBack, count)
	return l.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, sheetGround)
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
	return p.line.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, sheetGround)
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
	p.lastView = height

	if len(p.hits) == 0 {
		p.line.reset(width)
		p.line.add(blocks.Truncate(p.emptyText(), width), tokens.TextTertiary)
		p.lineOf = append(p.lineOf, -1)
		return append(rows, p.line.emit(&p.buf, p.profile(), p.focus(), width, false, tokens.Band, sheetGround))
	}

	p.clampCursor()
	p.scroll(height)
	lay := p.layout(width)
	for i := p.top; i < len(p.hits) && len(rows) < height; i++ {
		r := &p.rows[p.hits[i]]
		rows = append(rows, p.renderRow(r, i == p.cursor, lay, width))
		p.lineOf = append(p.lineOf, int32(i))
	}
	return rows
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
	verbW    int
	descAt   int
	rightW   int
	showDesc bool
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
	verbW := clampInt(p.verbW, verbColMin, verbColMax)
	if room := width - gutter; verbW > room {
		verbW = room
	}
	if verbW < 0 {
		verbW = 0
	}
	if need := gutter + descGap + descColMin; verbW > verbColMin &&
		width >= need+verbColMin && width < need+verbW {
		verbW = width - need
	}

	lay := layout{verbW: verbW, descAt: gutter + verbW + descGap}
	lay.showDesc = width >= lay.descAt+descColMin
	switch {
	case p.rightW == 0:
	case lay.showDesc:
		if width >= lay.descAt+descColMin+rightGap+p.rightW {
			lay.rightW = p.rightW
		}
	case width >= gutter+verbW+rightGap+p.rightW:
		lay.rightW = p.rightW
	}
	return lay
}

// renderRow draws one row: glyph, name, description (or chip, or reason), right
// column.
func (p *Picker) renderRow(r *row, selected bool, lay layout, width int) string {
	l := &p.line
	l.reset(width)
	verbTok, descTok, rightTok := rowTokens(r, selected)

	if r.glyph != "" {
		l.add(r.glyph, r.glyphTok)
	}
	l.padTo(gutter)

	at, n := matchAt(r.lowerVerb, p.needle)
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

	if lay.rightW > 0 && r.disabled == "" && r.right != "" {
		want := blocks.Width(r.right)
		if want > lay.rightW {
			want = lay.rightW
		}
		if start := width - want; start > l.w {
			l.padTo(start)
			l.add(r.right, rightTok)
		}
	}
	return l.emit(&p.buf, p.profile(), p.focus(), width, selected, tokens.Band, sheetGround)
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

// matchAt finds the needle in an already-lowered haystack, and returns the byte
// offset and length of the run to brighten. A rune boundary is checked because
// the offsets index the original string: highlighting from the middle of a
// multi-byte character would emit an escape sequence inside it, which is a
// broken cell rather than a bright one.
func matchAt(haystack, needle string) (int, int) {
	if needle == "" {
		return -1, 0
	}
	at := strings.Index(haystack, needle)
	if at < 0 || !utf8.RuneStart(haystack[at]) {
		return -1, 0
	}
	return at, len(needle)
}

// Key implements [tui2.PaneKeys].
//
// esc is consumed unconditionally, and it unwinds ONE level at a time (8.2.21's
// ladder): from the models level back to the roles, and from the roles out of
// the surface. Nothing below the overlay hears it — an open overlay IS what the
// user is watching.
//
// Everything that is not a binding is text, and only at the models level. The
// roles level has five rows and no filter, so a stray letter there types
// nothing rather than opening a search box over a list that does not need one.
func (p *Picker) Key(msg tea.KeyPressMsg) tea.Cmd {
	switch s := msg.String(); s {
	case "esc":
		if p.level == LevelModels {
			p.ascend()
			return nil
		}
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
	case "ctrl+u":
		p.setQuery("")
	case "ctrl+w":
		p.setQuery(dropWord(p.query))
	case "backspace":
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
// button), and a click on chrome does nothing at all rather than something
// arbitrary.
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

// descend opens one role's catalog.
func (p *Picker) descend(role store.ModelRole) {
	p.level, p.role, p.query = LevelModels, role, ""
	p.cursor, p.top = 0, 0
	p.rebuild()
	p.invalidate()
}

// ascend returns to the five, with the role that was open selected — the user
// came from that row and that is where their eye is.
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
