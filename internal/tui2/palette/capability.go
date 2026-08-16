package palette

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Capability is the `?` surface: 5.20 rule 3's capability honesty and 5.22
// rule 4's expanded footer, which the doc is explicit are the same data —
// "the full registry filtered to this scope (which is also the 5.20
// capability-honesty surface — same data)".
//
// It renders the same rows as the palette's `actions` section, from the same
// [Catalog], through the same [list]. What it adds is a title that names the
// room, and what it removes is the search field: `?` is asked in one room
// about one room, and a filter over a dozen verbs is ceremony.
//
// The two rules it exists to keep, restated as code below:
//
//   - Nothing is hidden. A disabled action renders WITH ITS REASON rather than
//     vanishing, because a room that silently omits what it cannot do answers
//     "can it do that?" with silence — the exact failure 5.20 rule 3 names.
//   - Everything is in words. A row is a verb, a sentence, and the accelerator
//     that reaches it; a belt-only verb says `ask`, because prose is a real
//     door and printing nothing there would read as "no way to do this".
type Capability struct {
	core

	title string

	line lineBuf
	buf  strings.Builder
	out  []string

	bodyTop int
}

var (
	_ tui2.Pane      = (*Capability)(nil)
	_ tui2.PaneKeys  = (*Capability)(nil)
	_ tui2.PaneMouse = (*Capability)(nil)
)

// NewCapability builds the `?` overlay.
func NewCapability(opts Options) *Capability {
	c := &Capability{core: core{opts: opts}}
	c.list.setStyle(opts.Styler)
	c.list.linear = opts.Linear
	c.list.emptyText = emptyBeltText
	return c
}

// emptyBeltText is what a room with no verbs at all says. It is a real state —
// a settled task's room can reach it — and it is stated rather than left as a
// blank pane, because a blank pane is indistinguishable from a broken one.
const emptyBeltText = "nothing this room can do yet"

// SetCatalog installs the room's belt. Only the action rows are built: rooms
// and settings belong to the palette's catalog-of-everything, and a `?` that
// listed them would be answering a question nobody asked in this room.
func (c *Capability) SetCatalog(cat Catalog) {
	c.title = clean(cat.Title)
	c.list.rebuild(func(dst []row) []row { return appendActions(dst[:0], cat) })
	c.invalidate()
}

// Title is the room name the header shows.
func (c *Capability) Title() string { return c.title }

// titleFallback names the room when the wiring did not. It is deliberately
// vague rather than wrong: `?` in an unnamed room still describes that room.
const titleFallback = "this room"

// Render implements [tui2.Pane]: a title line, a blank, and the list.
func (c *Capability) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	c.out = c.out[:0]
	c.out = append(c.out, c.header(width))
	if height >= 3 {
		c.out = append(c.out, blankLine(&c.line, &c.buf, c.list.profile, c.list.focus, width, c.list.ground()))
	}
	c.bodyTop = len(c.out)
	c.out = append(c.out, c.list.render(width, height-c.bodyTop)...)
	if len(c.out) > height {
		c.out = c.out[:height]
	}
	c.out = padSheet(c.out, &c.line, &c.buf, c.list.profile, c.list.focus, width, height, c.list.ground())
	return strings.Join(c.out, "\n")
}

func (c *Capability) header(width int) string {
	name := c.title
	if name == "" {
		name = titleFallback
	}
	l := &c.line
	l.reset(width)
	l.add("what ", tokens.TextTertiary)
	l.add(name, tokens.TextPrimary)
	l.add(" can do", tokens.TextTertiary)
	addChipTail(l, width, closeChip)
	return l.emit(&c.buf, c.list.profile, c.list.focus, width, false, tokens.Band, c.list.ground())
}

// Key implements [tui2.PaneKeys].
//
// There is no text field here, so the reading vocabulary gets the bare letters
// a reader expects: j/k and g/G alongside the arrows and the page keys. Three
// keys close — esc, q, and `?` itself, because the key that opened the surface
// must not become a key that does nothing while it is up. esc is consumed for
// the same reason it is in the palette (8.2.21: an open overlay is what you
// are watching).
func (c *Capability) Key(msg tea.KeyPressMsg) tea.Cmd {
	switch s := msg.String(); s {
	case "esc", "?", "q":
		return c.close()
	case "enter":
		return c.choose()
	case "j":
		c.moved(c.list.move(1))
	case "k":
		c.moved(c.list.move(-1))
	case "g":
		c.moved(c.list.move(-c.list.count()))
	case "G":
		c.moved(c.list.move(c.list.count()))
	default:
		c.navigate(s)
	}
	return nil
}

// Mouse implements [tui2.PaneMouse].
func (c *Capability) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	return c.mouse(msg, local, c.bodyTop)
}
