package palette

import (
	"image"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

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

// Palette is the ctrl+k command palette (5.22 rule 2): rooms, actions and
// settings in one fuzzy-searched catalog, every row teaching its own
// accelerator.
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
	p.list.rebuild(func(dst []row) []row { return buildRows(dst, c) })
	p.refreshEmptyText()
	p.invalidate()
}

// Reset clears the query and the selection. The wiring calls it when the
// palette opens: a palette that reopened still holding the last search is one
// whose first keystroke edits a query the user cannot see the origin of.
func (p *Palette) Reset() {
	changed := p.list.setQuery("")
	p.list.cursor, p.list.top = 0, 0
	p.refreshEmptyText()
	if changed {
		p.invalidate()
	}
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
		p.out = append(p.out, blankLine(&p.line, &p.buf, p.list.profile, p.list.focus, width))
	}
	p.bodyTop = len(p.out)
	p.out = append(p.out, p.list.render(width, height-p.bodyTop)...)
	if len(p.out) > height {
		p.out = p.out[:height]
	}
	p.out = padSheet(p.out, &p.line, &p.buf, p.list.profile, p.list.focus, width, height)
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

// escHint is the exit, advertised. An overlay that can only be left by a key
// nobody named is the memory test 5.22 exists to remove.
const escHint = "esc close"

func (p *Palette) header(width int) string {
	l := &p.line
	l.reset(width)
	l.add(searchLabel, tokens.TextTertiary)
	l.add("  ", tokens.TextTertiary)
	if p.list.query == "" {
		l.add(searchPlaceholder, tokens.TextTertiary)
	} else {
		l.add(p.list.query, tokens.TextPrimary)
	}
	// The right end carries the match count and the exit hint, in that drop
	// order: the count is what tells a user their query is working, and the
	// hint is a constant they learn once.
	count := strconv.Itoa(p.list.count()) + "/" + strconv.Itoa(len(p.list.rows))
	addTail(l, width, count+" "+tokens.GlyphSeparator+" "+escHint, count)
	return l.emit(&p.buf, p.list.profile, p.list.focus, width, false, tokens.Band, sheetGround)
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

// Mouse implements [tui2.PaneMouse].
func (p *Palette) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
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
