package palette

import (
	"image"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Options configures either component. Every field is optional and every
// omission degrades to something honest rather than to a panic.
type Options struct {
	// Styler paints every cell. A nil Styler renders plain text — the posture
	// every sibling package in this tree takes for the same situation, and
	// what the tests and the golden harness want.
	Styler *tokens.Styler

	// Linear is the accessible rendering (10.1.5). Both surfaces here are
	// already one column and already still — there is no motion to reduce — so
	// the only thing it changes is paint: the sheet's ground and the selection
	// band are both background fills, and linear mode drops both. Nothing is
	// lost, because the ▎ of [markerCol] already carries the selection in a
	// printable cell at every profile, which is why it is drawn alongside the
	// band rather than instead of it.
	Linear bool

	// Invalidate is [tui2.Shell.Invalidate]. The shell does NOT mark the frame
	// dirty when it hands a key to a pane, so a component that changed on a
	// keystroke must say so or the typed character never appears. A nil
	// Invalidate is legal and means the caller repaints on its own clock.
	Invalidate func()

	// OnChoose receives the result of the row the user picked. When it is nil
	// the component returns a command emitting [ChooseMsg] instead, so a
	// wiring built on messages and a wiring built on callbacks both work
	// without an adapter between them.
	//
	// It is called BEFORE the close, so a handler that wants to read the
	// component's state still can.
	OnChoose func(Result) tea.Cmd

	// OnClose is asked to drop the overlay plane — [tui2.Shell.SetOverlay]
	// with false, plus whatever focus the wiring wants restored. A nil OnClose
	// emits [CloseMsg].
	//
	// The component never closes itself, because it does not own the plane it
	// is drawn on. It only ever says that it is finished.
	OnClose func() tea.Cmd
}

// ChooseMsg is emitted when a row is chosen and [Options.OnChoose] is nil.
type ChooseMsg struct{ Result Result }

// CloseMsg is emitted when the surface is finished and [Options.OnClose] is
// nil.
type CloseMsg struct{}

// Palette is the summon palette (5.22 rule 2), raised by ctrl+space: every
// action, every job the store still holds, every room and every settings row in
// ONE fuzzy-searched catalog, each row teaching its own accelerator.
//
// One catalog rather than four surfaces is the whole design. A reader who has
// to know whether the thing they are looking for is a job or a room or a
// setting before they can look for it is a reader doing the search themselves;
// the sections are how the answer is presented, never how the question is
// asked.
//
// It is a [tui2.Pane] for the overlay layer. It never raises or drops that
// layer itself; see [Options.OnClose].
type Palette struct {
	core

	line lineBuf
	buf  strings.Builder
	out  []string

	// bodyTop is how many chrome lines the last render drew above the list, so
	// a click can be translated into a row.
	bodyTop int

	// levels is the drill stack, root first. It is EMPTY at depth zero, which
	// is the ordinary palette and the state the surface opens in — a stack that
	// always held a root entry would make every call site test for a level that
	// carries no information. See trail.go for the grammar.
	levels []level
	// segs is where the trail's segments landed in the last render, so a click
	// on the header can be turned back into a depth.
	segs []segment
}

var (
	_ tui2.Pane      = (*Palette)(nil)
	_ tui2.PaneKeys  = (*Palette)(nil)
	_ tui2.PaneMouse = (*Palette)(nil)
)

// New builds a palette. It renders an empty, honest surface until a catalog
// arrives.
func New(opts Options) *Palette {
	p := &Palette{core: core{opts: opts}}
	p.list.setStyle(opts.Styler)
	p.list.linear = opts.Linear
	p.list.emptyText = emptyCatalogText
	return p
}

// emptyCatalogText is what a palette with nothing in it says. 5.22 rule 6 asks
// empty states to teach; the honest thing to teach here is that the catalog
// has not arrived, not to imply the product has no actions.
const emptyCatalogText = "nothing to show yet"

// noMatchPrefix opens the over-filtered message. It names the query back,
// because the failure a user needs to see is their own typo.
const noMatchPrefix = "no match for "

// SetCatalog installs the facts. Call it when they move — a task settles, the
// scope changes, a setting is written — and not per frame: everything
// expensive happens here so the filter path stays allocation-lean.
func (p *Palette) SetCatalog(c Catalog) {
	// A catalog arriving from the wiring is always the ROOT level's: the facts
	// moved underneath the surface. It does not disturb the drill stack, because
	// a task settling elsewhere is not a reason to throw the reader out of the
	// list they are standing in (7.2), and the levels hold their own catalogs.
	if len(p.levels) > 0 {
		p.levels[0].cat = c
		return
	}
	p.showing, p.drill = c, c.Drill
	p.list.rebuild(func(dst []row) []row { return buildRows(dst, c) })
	p.refreshEmptyText()
	p.invalidate()
}

// Reset clears the query, the selection AND the drill stack. The wiring calls
// it when the palette opens: a palette that reopened still holding the last
// search is one whose first keystroke edits a query the user cannot see the
// origin of, and a palette that reopened three levels down is that same
// surprise with the list changed as well.
func (p *Palette) Reset() {
	changed := p.list.setQuery("")
	p.list.cursor, p.list.top = 0, 0
	p.levels = p.levels[:0]
	p.refreshEmptyText()
	if changed {
		p.invalidate()
	}
}

// -- the drill (see trail.go for the grammar) ---------------------------------

// Push drills one level into a sub-list — an option's choices, a role's models,
// whatever the chosen row opened — naming the level with the word the trail will
// carry. The reader's place on the level being left is kept, so popping is a
// return rather than a fresh open.
//
// The wiring calls this from [Options.OnChoose], synchronously, for a result
// [Catalog.Drill] claimed. It is a real call and not a returned intent, because
// by the time a command reached the runtime the surface would already have
// closed — the same ordering hazard [core.choose] documents.
func (p *Palette) Push(word string, c Catalog) {
	// The rung records the level being LEFT — its catalog and the reader's place
	// in it — rather than the one being entered. That is what makes popping one
	// statement: the way back is written down at the moment it is still known,
	// instead of being reconstructed later from a stack of things that came
	// after it.
	p.levels = append(p.levels, level{
		word:   word,
		cat:    p.showing,
		query:  p.list.query,
		cursor: p.list.cursor,
		top:    p.list.top,
	})
	p.install(c, "", 0, 0)
}

// Pop goes back one level and reports whether there was one to go back to. At
// depth zero it does nothing and says so, which is what lets backspace fall
// through to editing the query without a second test.
func (p *Palette) Pop() bool {
	n := len(p.levels)
	if n == 0 {
		return false
	}
	back := p.levels[n-1]
	p.levels = p.levels[:n-1]
	p.install(back.cat, back.query, back.cursor, back.top)
	return true
}

// popTo unwinds to a depth, which is what a click on a trail segment means. It
// is Pop in a loop rather than an index assignment because each rung has a
// catalog to rebuild and a place to restore, and skipping them would leave the
// list showing one level and the trail claiming another.
func (p *Palette) popTo(depth int) bool {
	moved := false
	for len(p.levels) > depth {
		if !p.Pop() {
			break
		}
		moved = true
	}
	return moved
}

// Depth is how many levels below the root the reader is standing.
func (p *Palette) Depth() int { return len(p.levels) }

// Trail is the words of the levels the reader has drilled through, root-most
// first. It does NOT include [rootSegment]: that is the header's own way of
// drawing "the whole catalog", not a level anybody pushed.
func (p *Palette) Trail() []string {
	if len(p.levels) == 0 {
		return nil
	}
	out := make([]string, len(p.levels))
	for i := range p.levels {
		out[i] = p.levels[i].word
	}
	return out
}

// install replaces the visible catalog and puts the reader at a stated place.
// It is the one door every level change takes — the first SetCatalog, a push
// and a pop alike — so the drill predicate, the empty-state sentence and the
// repaint can never be updated for one of the three and forgotten for another.
func (p *Palette) install(c Catalog, query string, cursor, top int) {
	p.showing = c
	p.drill = c.Drill
	// The query is set BEFORE the rebuild rather than through setQuery after it.
	// setQuery is a no-op when the text has not changed, and the common case
	// here — an empty query on the way in, an empty one on the way back — is
	// exactly that: the filter would keep the level's OLD surviving set and the
	// list would render one catalog through another's hits.
	p.list.query = query
	p.list.rebuild(func(dst []row) []row { return buildRows(dst, c) })
	p.list.cursor, p.list.top = cursor, top
	p.list.clampCursor()
	p.refreshEmptyText()
	p.invalidate()
}

// Query is the current filter text.
func (p *Palette) Query() string { return p.list.query }

// refreshEmptyText keeps the empty-state sentence answering the right
// question: an empty catalog and an over-filtered one are different facts and
// must not share a line.
func (p *Palette) refreshEmptyText() {
	if p.list.query == "" {
		p.list.emptyText = emptyCatalogText
		return
	}
	p.list.emptyText = noMatchPrefix + p.list.query
}

// Render implements [tui2.Pane]: a header line, a blank, and the list.
//
// The blank is the first thing height pressure takes and the header is the
// last: a list with no search line is a list the user cannot tell they are
// filtering.
func (p *Palette) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	p.out = p.out[:0]
	p.out = append(p.out, p.header(width))
	if height >= 3 {
		p.out = append(p.out, blankLine(&p.line, &p.buf, p.list.profile, p.list.focus, width, p.list.ground()))
	}
	p.bodyTop = len(p.out)
	p.out = append(p.out, p.list.render(width, height-p.bodyTop)...)
	if len(p.out) > height {
		p.out = p.out[:height]
	}
	p.out = padSheet(p.out, &p.line, &p.buf, p.list.profile, p.list.focus, width, height, p.list.ground())
	return strings.Join(p.out, "\n")
}

// searchLabel names the field. There is deliberately NO caret glyph: every
// block and bar character that would serve as one is East-Asian Ambiguous and
// would make the line two cells wider under a CJK-locale terminal, which is
// the width instability 5.17 bans. The label plus the placeholder says the
// same thing and always measures the same.
const searchLabel = "search"

// searchPlaceholder is the empty-query hint — instruction, not personality
// (8.2 rule 25 keeps personality in idle placeholders, not in chrome).
const searchPlaceholder = "type to filter"

// closeChip is the exit, advertised. An overlay that can only be left by a key
// nobody named is the memory test 5.22 exists to remove — and a key printed
// BEFORE its verb, both in the same grey, is that same memory test with an extra
// word in it. Design-law §16: verb first and brighter, key behind it and dimmer,
// so `close esc` reads as one control rather than as two words to sort out.
var closeChip = registry.ChipFor("close", "esc")

// escHint is the chip as plain text, for measuring and for the tests that read
// a header as a string.
var escHint = closeChip.String()

func (p *Palette) header(width int) string {
	l := &p.line
	l.reset(width)
	p.addTrail(l)
	l.add(searchLabel, tokens.TextTertiary)
	l.add("  ", tokens.TextTertiary)
	if p.list.query == "" {
		l.add(searchPlaceholder, tokens.TextTertiary)
	} else {
		l.add(p.list.query, tokens.TextPrimary)
	}
	// The right end carries the match count and the exit chip, in that drop
	// order: the count is what tells a user their query is working, and the
	// chip is a constant they learn once.
	count := strconv.Itoa(p.list.count()) + "/" + strconv.Itoa(len(p.list.rows))
	lead := count + " " + tokens.GlyphSeparator + " "
	if start := width - blocks.Width(lead+escHint); start > l.w+1 {
		l.padTo(start)
		l.add(lead, tokens.TextTertiary)
		addChip(l, closeChip)
	} else {
		addTail(l, width, count)
	}
	return l.emit(&p.buf, p.list.profile, p.list.focus, width, false, tokens.Band, p.list.ground())
}

// addTail right-aligns the first candidate that fits with a space to spare,
// and draws nothing when none does. Candidates are in preference order.
func addTail(l *lineBuf, width int, candidates ...string) {
	for _, tail := range candidates {
		if start := width - blocks.Width(tail); start > l.w+1 {
			l.padTo(start)
			l.add(tail, tokens.TextTertiary)
			return
		}
	}
}

// addTrail writes the drill trail at the head of the input row and records
// where each segment landed, so a click can be turned back into a level. At
// depth zero it draws nothing and records nothing: a one-step path is a label
// pretending to be navigation.
//
// The TIERS split the segments by what they are, which is the correction 5.22's
// checklist forces — "an interactive chip may never live permanently in the
// dimmest tier". Every ancestor is a door and sits at the secondary tier; the
// CURRENT segment is where you already are, opens nothing, and is drawn at the
// dimmest one. Both are quieter than the query itself, which is the primary
// tier and is the thing the reader is actually typing.
func (p *Palette) addTrail(l *lineBuf) {
	p.segs = p.segs[:0]
	depth := len(p.levels)
	if depth == 0 {
		return
	}
	for i := 0; i <= depth; i++ {
		word := rootSegment
		if i > 0 {
			word = p.levels[i-1].word
		}
		if word == "" {
			word = untitledRow
		}
		if i > 0 {
			l.add(" ", tokens.TextTertiary)
		}
		l.add(trailLead, tokens.TextTertiary)
		start := l.w
		if i == depth {
			l.add(word, tokens.TextTertiary)
			continue
		}
		l.add(word, tokens.TextSecondary)
		if w := l.w - start; w > 0 {
			p.segs = append(p.segs, segment{depth: i, at: start, w: w})
		}
	}
	l.add(trailGap, tokens.TextTertiary)
}

// addChip writes a [registry.Chip] at the cursor in the two tiers the grammar
// asks for: the verb at the secondary tier, the key one below it.
//
// The verb is NOT at the dimmest tier, and that is 5.22's checklist rather than
// a preference — "an interactive chip may never live permanently in the dimmest
// tier". The key may, because it is annotation on the verb and not the control.
func addChip(l *lineBuf, chip registry.Chip) {
	if chip.Empty() {
		l.add(chip.Key, tokens.TextTertiary)
		return
	}
	l.add(chip.Verb, tokens.TextSecondary)
	if chip.Key != "" {
		l.add(registry.ChipGap+chip.Key, tokens.TextTertiary)
	}
}

// addChipTail right-aligns a chip, or draws nothing when it does not fit with a
// space to spare — [addTail]'s rule, spent on two tiers instead of one.
func addChipTail(l *lineBuf, width int, chip registry.Chip) {
	if start := width - blocks.Width(chip.String()); start > l.w+1 {
		l.padTo(start)
		addChip(l, chip)
	}
}

// Key implements [tui2.PaneKeys].
//
// esc is consumed unconditionally: an open overlay IS what the user is
// watching, and 8.2.21's rule that esc acts on what you are watching resolves
// in this surface's favour while it is up. Nothing below it — not an in-flight
// turn, not a stashed draft — hears the key.
//
// Everything that is not a binding is text. That order matters: the editing
// keys are checked first so a query can never swallow esc or enter, and the
// default arm inserts only what the terminal reported as printable text, so a
// bare modifier or an unmapped chord types nothing rather than a stray rune.
func (p *Palette) Key(msg tea.KeyPressMsg) tea.Cmd {
	switch s := msg.String(); s {
	case "esc":
		return p.close()
	case "enter":
		return p.choose()
	case "ctrl+u":
		p.setQuery("")
	case "ctrl+w":
		p.setQuery(dropWord(p.list.query))
	case "backspace":
		// THE BACK KEY, and only on an empty filter. With text in the field
		// backspace deletes a letter, unchanged — the filter is what the reader
		// is watching while they type, and a key that sometimes edited and
		// sometimes threw away the whole list would make typing feel dangerous.
		// Empty-then-back is the rule a shell prompt and every file picker
		// already teach, and it costs no chord nobody knows. See trail.go.
		if p.list.query == "" && p.Pop() {
			return nil
		}
		p.setQuery(dropRune(p.list.query))
	default:
		if p.navigate(s) {
			return nil
		}
		if text := msg.Key().Text; text != "" {
			p.setQuery(p.list.query + text)
		}
	}
	return nil
}

// headerLine is the row the search field and the trail are drawn on. It is
// named because the pointer has to find it and [Palette.Render] has to draw it,
// and a 0 written twice is a 0 that can drift.
const headerLine = 0

// Mouse implements [tui2.PaneMouse].
//
// The header is a target now, and only because the trail is on it: a click on
// an ancestor segment pops back to that level, which is the pointer's whole way
// out of a drilled list (5.22 rule 5 — the row IS the button, and a trail
// segment is a row of the path). Everything else on that line is still chrome
// and still does nothing, rather than something arbitrary.
func (p *Palette) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	if click, ok := msg.(tea.MouseClickMsg); ok &&
		click.Button == tea.MouseLeft && local.Y == headerLine {
		if depth, hit := hitAt(p.segs, local.X); hit {
			if p.popTo(depth) {
				p.invalidate()
			}
		}
		return nil
	}
	return p.mouse(msg, local, p.bodyTop)
}

func (p *Palette) setQuery(q string) {
	if p.list.setQuery(q) {
		p.refreshEmptyText()
		p.invalidate()
	}
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
