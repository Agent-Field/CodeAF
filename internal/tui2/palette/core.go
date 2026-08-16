package palette

import (
	"image"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// core is everything the two components share below their header line: the
// list, the options, the navigation vocabulary, and the two ways a surface can
// finish. Both embed it, so `p.list` and `p.opts` read the same in either and
// the promoted methods are the same methods — not two implementations that
// have to be kept agreeing.
type core struct {
	opts Options
	list list

	// showing is the catalog currently on screen — the root's, or a drilled
	// level's. It is kept so a push can write down the way back (see
	// [Palette.Push]) without the wiring having to hand the same catalog in
	// twice.
	showing Catalog
	// drill is [Catalog.Drill] for the level on screen: does choosing this
	// result open a sub-list rather than finish. Nil means nothing here drills,
	// which is the `?` sheet's permanent state and the palette's usual one.
	drill func(Result) bool
}

// SetStyler rebinds the painter, for a terminal profile or a pane focus
// change.
func (c *core) SetStyler(s *tokens.Styler) {
	c.opts.Styler = s
	c.list.setStyle(s)
	c.invalidate()
}

// Count is how many rows survive the current filter.
func (c *core) Count() int { return c.list.count() }

// Total is how many rows the catalog holds.
func (c *core) Total() int { return len(c.list.rows) }

// Selected is the result the current row would yield, and whether it would
// yield one at all — a disabled row would not, which is the same answer the
// row itself is already giving on screen.
func (c *core) Selected() (Result, bool) {
	r, ok := c.list.selected()
	if !ok || !r.enabled() {
		return nil, false
	}
	return r.result, true
}

// wheelStep is how far a wheel notch moves the selection. Three rows is the
// conventional notch and matches the transcript's own step, so the two panes
// do not disagree about what a wheel means.
const wheelStep = 3

// navigate handles the movement vocabulary both surfaces share, and reports
// whether it consumed the key. Neither list wraps: wrapping makes a held-down
// arrow silently restart at the top, and a list whose end is not felt is a
// list whose length is not known.
func (c *core) navigate(key string) bool {
	switch key {
	case "up", "ctrl+p", "shift+tab":
		c.moved(c.list.move(-1))
	case "down", "ctrl+n", "tab":
		c.moved(c.list.move(1))
	case "pgup":
		c.moved(c.list.page(-1))
	case "pgdown":
		c.moved(c.list.page(1))
	case "home":
		c.moved(c.list.move(-c.list.count()))
	case "end":
		c.moved(c.list.move(c.list.count()))
	default:
		return false
	}
	return true
}

// mouse is the shared pointer vocabulary: a wheel notch scrolls the selection,
// a left click on a row selects and runs it (5.22 rule 5 — the row IS the
// button), and a click on chrome does nothing at all rather than something
// arbitrary. bodyTop is how many chrome lines the component drew above the
// list in its last render.
func (c *core) mouse(msg tea.MouseMsg, local image.Point, bodyTop int) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelUp:
			c.moved(c.list.move(-wheelStep))
		case tea.MouseWheelDown:
			c.moved(c.list.move(wheelStep))
		}
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		hit, ok := c.list.hitAtLine(local.Y - bodyTop)
		if !ok {
			return nil
		}
		c.moved(c.list.selectHit(hit))
		return c.choose()
	}
	return nil
}

// choose emits the selected row's result and finishes. A disabled row does
// neither and does not close: the user asked for something the room cannot do,
// and the honest answer is the reason still on screen beside it.
func (c *core) choose() tea.Cmd {
	res, ok := c.Selected()
	if !ok {
		return nil
	}
	if c.drill != nil && c.drill(res) {
		// A ROW THAT OPENS A SUB-LIST DOES NOT FINISH THE SURFACE. The wiring
		// answers this result by calling [Palette.Push] — synchronously, from
		// inside OnChoose, which is why it is a call and not a returned intent —
		// and the palette stays up one level deeper. Closing here and reopening
		// from the handler would work exactly as badly as it sounds: the reader
		// would watch the overlay blink, and the level they came from would be
		// gone from the stack that has to remember it.
		return c.emit(res)
	}
	// CLOSE FIRST, and the order is the whole point.
	//
	// A host wires OnChoose and OnClose as ordinary calls, so both run right
	// here, synchronously, before either returned command has reached the
	// runtime. The act a row performs may itself raise a door — settings, the
	// model palette, the capability sheet — onto a plane that holds exactly one
	// overlay. Emitting first meant the close ran second and tore down the door
	// the row had just opened: every row on this list that opens something
	// answered enter and answered a click by politely closing and doing
	// nothing, which is the bug a user reported as "the slash commands in the
	// question-mark sheet are not executed".
	//
	// Two statements rather than two arguments to Batch, because the sequence
	// IS the fix and an argument list does not read like one.
	closed := c.close()
	return tea.Batch(closed, c.emit(res))
}

func (c *core) emit(res Result) tea.Cmd {
	if c.opts.OnChoose != nil {
		return c.opts.OnChoose(res)
	}
	return func() tea.Msg { return ChooseMsg{Result: res} }
}

func (c *core) close() tea.Cmd {
	if c.opts.OnClose != nil {
		return c.opts.OnClose()
	}
	return func() tea.Msg { return CloseMsg{} }
}

func (c *core) moved(changed bool) {
	if changed {
		c.invalidate()
	}
}

func (c *core) invalidate() {
	if c.opts.Invalidate != nil {
		c.opts.Invalidate()
	}
}
