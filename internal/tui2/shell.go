package tui2

import (
	"image"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The v2 shell: one Bubble Tea v2 program, one compositor, one layout solver,
// and a slot per region for the panes that will arrive from the sibling
// packages. It holds no session, no store and no head — this wave builds the
// room, not what happens in it.
//
// Three properties are load-bearing and everything else here is in service of
// them:
//
//   - Alt screen, always (8.1.1). Native scrollback was considered and refused:
//     it cannot coexist with a persistent rail, overlays or mouse capture, and
//     a byte the terminal owns cannot be re-emitted when a finalized block
//     mutates. We keep our own viewport, so we keep our own anchor.
//   - The frame is recomputed when a fact moved, and not otherwise. Bubble Tea
//     v2 asks for a view after every message; a message that changed nothing
//     gets the same string back, its renderer diffs it to nothing, and nothing
//     goes over the wire. Bytes over SSH is the metric (10.1.1), so a repaint
//     nobody asked for is the defect.
//   - Nothing degenerate crashes. A zero-size terminal, a resize storm, an
//     absent capability and a one-by-one window are ordinary inputs here, not
//     edge cases — each one draws less and none of them panics.

// resizeDebounce coalesces resize storms (10.1.4). A multiplexer redrawing its
// panes emits a burst of sizes a few milliseconds apart, and laying out each
// one costs a full frame for a shape that existed for four milliseconds. The
// window is deliberately inside a frame at 60Hz: long enough to swallow a
// burst, short enough that a deliberate drag still feels attached to the mouse.
const resizeDebounce = 24 * time.Millisecond

// resizeSettledMsg fires once a resize burst has gone quiet. It carries the
// epoch it was armed for so a tick that outlived its reason is ignored rather
// than applying a size that has already been superseded.
type resizeSettledMsg struct{ epoch uint64 }

// Options configures a shell. The zero value is a working, sessionless shell
// at default metrics, which is what the golden harness wants.
type Options struct {
	// Metrics overrides the responsive numbers table (10.5.24). Nil takes
	// DefaultMetrics; the tokens sibling fills this in when it lands.
	Metrics *Metrics

	// Linear selects the accessible rendering (10.1.5): one column, no
	// motion, no cursor jumps. Same product, less motion.
	Linear bool

	// DB and Session are the operator plumbing the v2 entry point was given.
	// Nothing is opened this wave — the shell only reports what it was handed,
	// which is the honest thing for a surface that cannot yet use it.
	DB      string
	Session string
}

// Shell is the program model. It is used through a pointer: the compositor
// holds a cell buffer and a layer table that exist to be reused, and copying
// the model per message would defeat both.
type Shell struct {
	metrics Metrics
	linear  bool
	db      string
	session string

	comp   compositor
	layout layout
	panes  [numLayers]Pane

	caps Capabilities

	// Applied size, and the size the terminal last claimed. They differ only
	// inside a debounce window.
	width, height      int
	pendingW, pendingH int
	sized              bool
	resizeArmed        bool
	resizeEpoch        uint64

	scopeOpen   bool
	overlayOpen bool
	focus       LayerID

	hitID LayerID
	hitAt image.Point
	hitOK bool

	// dirty and frame are the repaint gate. A pane whose content changed
	// without the shell seeing why says so through Invalidate.
	dirty bool
	frame string
}

var _ tea.Model = (*Shell)(nil)

// NewShell builds a shell. It draws nothing until a size arrives, which is the
// state Bubble Tea starts every program in anyway.
func NewShell(opts Options) *Shell {
	metrics := DefaultMetrics()
	if opts.Metrics != nil {
		metrics = *opts.Metrics
	}
	return &Shell{
		metrics: metrics.sane(),
		linear:  opts.Linear,
		db:      opts.DB,
		session: opts.Session,
		focus:   LayerTranscript,
		dirty:   true,
	}
}

// SetPane binds a renderer to a region.
//
// SEAM — internal/tui2/blocks: this is the whole surface area between the
// shell and the block renderers. A nil pane returns the region to its
// placeholder, which is what makes a half-built surface bootable.
func (s *Shell) SetPane(id LayerID, p Pane) {
	if id == LayerNone || id >= numLayers {
		return
	}
	s.panes[id] = p
	s.dirty = true
}

// Invalidate marks the frame stale. A pane calls it when its content moved for
// a reason the shell could not observe.
func (s *Shell) Invalidate() { s.dirty = true }

// SetOverlay raises or drops the overlay plane.
//
// SEAM — Wave 3: dialogs, the palette and the ? surfaces are one pane bound to
// LayerOverlay and shown with this. The z discipline is the compositor's, not
// the dialog's: a raised overlay takes both the cells and the clicks, and no
// pane underneath it learns that a dialog exists.
func (s *Shell) SetOverlay(open bool) {
	if s.overlayOpen == open {
		return
	}
	s.overlayOpen = open
	s.relayout()
}

// OverlayOpen reports whether the overlay plane is raised.
func (s *Shell) OverlayOpen() bool { return s.overlayOpen }

// Linear reports the accessible rendering (10.1.5). Panes read it to drop
// motion, drawn frames and cursor jumps; it is a mode, never a theme.
func (s *Shell) Linear() bool { return s.linear }

// Capabilities reports what the terminal has admitted to so far. Panes read it
// to choose a binding or a glyph ladder; nobody may require anything in it.
func (s *Shell) Capabilities() Capabilities { return s.caps }

// Motion reports whether animation is allowed. Linear mode is a real
// reduced-motion flag (10.1.4), not a theme — the shared animation clock, when
// it lands, asks here before it ticks.
func (s *Shell) Motion() bool { return !s.linear }

// Init asks the terminal the two questions whose answers change how we draw.
func (s *Shell) Init() tea.Cmd { return negotiate() }

// Update folds a message into the shell.
func (s *Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return s, s.sizeChanged(msg.Width, msg.Height)

	case resizeSettledMsg:
		if msg.epoch == s.resizeEpoch {
			s.resizeArmed = false
			s.applySize()
		}
		return s, nil

	case tea.KeyPressMsg:
		return s, s.key(msg)

	case tea.MouseMsg:
		return s, s.mouse(msg)
	}

	// Everything else is the terminal answering a question we asked at start.
	if s.caps.observe(msg) {
		s.dirty = true
	}
	return s, nil
}

// View declares the frame and the terminal state that frame wants. Bubble Tea
// v2's declarative view management is the point: alt screen and mouse capture
// are properties of what we are showing, not commands scattered across a
// startup path and three toggles.
func (s *Shell) View() tea.View {
	v := tea.NewView(s.render())

	// Alt screen is not negotiable (8.1.1).
	v.AltScreen = true

	// Cell motion rather than all motion: we want clicks, wheel and drags, and
	// we do not want a message per mouse move on an idle screen. Bandwidth is
	// the metric.
	v.MouseMode = tea.MouseModeCellMotion

	// Focus reporting stays off, permanently. tmux ships it disabled, so a
	// surface that behaves differently when focused behaves differently for
	// half its users (10.1.2).
	v.ReportFocus = false

	// KKP is an enhancement we accept and never request beyond what Bubble Tea
	// asks for by default. See requestedEnhancements.
	v.KeyboardEnhancements = requestedEnhancements()

	// The cursor belongs to the composer, and there is no composer yet. Nil is
	// a hidden cursor, which is also what linear mode wants (10.1.5).
	v.Cursor = nil

	return v
}

// Frame renders one frame at a given size with no terminal involved.
//
// SEAM — internal/tui2/golden: the screenshot harness drives the shell through
// this and through Update. It applies the size immediately rather than through
// the debounce, because a golden that had to wait 24ms per width would be a
// golden nobody runs at every width from 1 to 110.
func (s *Shell) Frame(width, height int) string {
	s.pendingW, s.pendingH = width, height
	s.applySize()
	return s.render()
}

// sizeChanged records a size and arms the debounce. The first size is applied
// at once: waiting on it would mean holding a blank alt screen for the length
// of the window, and one size is not a storm.
func (s *Shell) sizeChanged(w, h int) tea.Cmd {
	s.pendingW, s.pendingH = w, h
	if !s.sized {
		s.applySize()
		return nil
	}
	if w == s.width && h == s.height {
		return nil
	}
	if s.resizeArmed {
		// Coalesced. The armed tick will pick up whatever the latest size is
		// when it fires, so a burst of forty costs one layout.
		return nil
	}
	s.resizeArmed = true
	s.resizeEpoch++
	epoch := s.resizeEpoch
	return tea.Tick(resizeDebounce, func(time.Time) tea.Msg {
		return resizeSettledMsg{epoch: epoch}
	})
}

// applySize moves the pending size into effect and relays out.
func (s *Shell) applySize() {
	if s.sized && s.pendingW == s.width && s.pendingH == s.height {
		return
	}
	s.width, s.height = s.pendingW, s.pendingH
	s.sized = true
	s.relayout()
}

// relayout solves the frame and hands the table to the compositor. The
// previous slot slice goes back in so a resize storm reuses one allocation
// instead of leaving a frame's worth of garbage per event.
func (s *Shell) relayout() {
	s.layout = solveInto(s.layout.Slots, s.width, s.height, s.metrics, mode{
		Linear:      s.linear,
		ScopeOpen:   s.scopeOpen,
		OverlayOpen: s.overlayOpen,
	})
	s.comp.setLayout(s.layout)

	// A pane that is no longer on screen cannot hold focus, and a stale hit
	// record would name a region that is not there.
	if _, ok := s.comp.rect(s.focus); !ok {
		s.focus = s.firstFocusable()
	}
	s.hitOK = false
	s.hitID = LayerNone
	s.dirty = true
}

// key handles the bindings the shell owns. Everything else is offered to the
// focused pane, which is where Wave 3's real keyboard lives.
func (s *Shell) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "ctrl+o":
		// Provisional, and named here so Wave 3 can move it deliberately: show
		// the scope map. In a wide frame the rail is always drawn and this only
		// records the preference; in a narrow one it is what swaps the main
		// pane to the same rows, the same keys, the same selection (5.15).
		s.scopeOpen = !s.scopeOpen
		s.relayout()
		return nil
	}
	if p, ok := s.paneFor(s.focus).(PaneKeys); ok {
		return p.Key(msg)
	}
	return nil
}

// mouse routes an event to whatever is under the pointer. The pane is handed a
// point relative to its own top-left corner, so the 45 sites that used to ask
// a stored rectangle whether it contained a coordinate collapse to this one.
func (s *Shell) mouse(msg tea.MouseMsg) tea.Cmd {
	pos := msg.Mouse()
	id, local, ok := s.comp.hit(pos.X, pos.Y)

	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
		// Motion deliberately does not update the record: a pointer crossing
		// the screen must not repaint the status line forty times.
		if s.hitID != id || s.hitAt != local || s.hitOK != ok {
			s.hitID, s.hitAt, s.hitOK = id, local, ok
			s.dirty = true
		}
	}
	if !ok {
		return nil
	}
	if _, isClick := msg.(tea.MouseClickMsg); isClick {
		s.setFocus(id)
	}
	if p, okp := s.paneFor(id).(PaneMouse); okp {
		return p.Mouse(msg, local)
	}
	return nil
}

// setFocus moves the conversation. The status line is chrome and never takes
// focus — clicking a footer must not change what the composer is bound to.
func (s *Shell) setFocus(id LayerID) {
	if id == LayerStatus || id == LayerNone || id == s.focus {
		return
	}
	if p, ok := s.paneFor(s.focus).(PaneFocus); ok {
		p.Focus(false)
	}
	s.focus = id
	if p, ok := s.paneFor(s.focus).(PaneFocus); ok {
		p.Focus(true)
	}
	s.dirty = true
}

// firstFocusable picks a home for the cursor when the one it had left the
// frame. It walks the layout rather than a preference list, so the answer is
// always a region that is actually on screen.
func (s *Shell) firstFocusable() LayerID {
	for _, sl := range s.layout.Slots {
		if sl.ID != LayerStatus {
			return sl.ID
		}
	}
	return LayerNone
}

func (s *Shell) paneFor(id LayerID) Pane {
	if id >= numLayers {
		return nil
	}
	return s.panes[id]
}

// render composes the frame, or returns the last one when nothing moved.
func (s *Shell) render() string {
	if !s.dirty {
		return s.frame
	}
	for _, sl := range s.layout.Slots {
		w, h := sl.Rect.Dx(), sl.Rect.Dy()
		var content string
		if p := s.paneFor(sl.ID); p != nil {
			content = p.Render(w, h)
		} else {
			content = s.placeholder(sl.ID, w, h)
		}
		s.comp.setContent(sl.ID, content)
	}
	s.frame = s.comp.render()
	s.dirty = false
	return s.frame
}
