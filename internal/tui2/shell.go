package tui2

import (
	"image"
	"os"
	"strings"
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

	// Terminal configures what the surface says to the terminal from outside
	// the frame — title, taskbar progress, desktop notifications, prompt marks,
	// hyperlinks. Nil takes [DefaultTerminalOptions]; a zero
	// [TerminalOptions{}] value turns every channel off, which is what a
	// headless driver wants and what the golden harness gets.
	Terminal *TerminalOptions

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
	// railHidden is §6's `sidebar: hidden`, asked for by the surface rather than
	// by the width. See [mode.RailHidden] and [Shell.SetRailHidden].
	railHidden bool
	// composerGrow is the extra rows the composer region has asked for, so its
	// inline completions have somewhere to be (GrowComposer).
	composerGrow int
	focus        LayerID

	hitID LayerID
	hitAt image.Point
	hitOK bool

	// hoverID is the layer the pointer is resting over, and hoverOK says the
	// pointer is over anything at all. They exist so the pane the pointer LEFT
	// can be told it lost the pointer without every pane having to notice its
	// own absence.
	hoverID LayerID
	hoverOK bool

	// The terminal-protocol side. term is the operator's permissions, note
	// decides whether an event becomes a notification, and attn/busy are the
	// two facts the title and the taskbar are computed from.
	//
	// out is the pending out-of-band write queue. Everything here except the
	// title travels beside the frame rather than inside it, because the frame
	// is composed into a CELL BUFFER: a cell holds a rune, a style and a
	// hyperlink, and there is nowhere in it to put a notification or a prompt
	// mark. So those bytes are queued and flushed as a Bubble Tea command, and
	// the queue is what makes "zero bytes when nothing changed" structural —
	// a setter that changed nothing appends nothing, and a flush with nothing
	// to flush is a nil command rather than an empty write.
	//
	// The queue exists so that two facts moving in the same message — a turn
	// ends AND it was worth interrupting for — leave as one write.
	term TerminalOptions
	note notifier
	attn int
	busy bool
	out  []string

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
	term := DefaultTerminalOptions()
	if opts.Terminal != nil {
		term = *opts.Terminal
	}
	return &Shell{
		metrics: metrics.sane(),
		linear:  opts.Linear,
		db:      opts.DB,
		session: opts.Session,
		focus:   LayerTranscript,
		term:    term.sane(),
		note:    newNotifier(),
		caps:    Capabilities{Mux: detectMultiplexer(os.Getenv)},
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

// SetRailHidden takes the rail off the frame, or puts it back.
//
// It is §6's `sidebar: hidden` as a door rather than as a setting, because the
// surface above has one state where the sidebar is not a choice: a lens already
// showing the same list of jobs at page altitude. The columns go back to the
// lens rather than to a blank margin — hiding a pane and leaving its rectangle
// reserved would be the surface paying for a sidebar it decided not to draw.
//
// It is deliberately NOT SetPane(LayerRail, nil): a nil pane keeps its slot and
// draws the skeleton's placeholder in it, which is a rail-shaped hole rather
// than a full-width page.
func (s *Shell) SetRailHidden(hidden bool) {
	if s.railHidden == hidden {
		return
	}
	s.railHidden = hidden
	s.relayout()
}

// RailHidden reports whether the rail has been taken off the frame.
func (s *Shell) RailHidden() bool { return s.railHidden }

// GrowComposer asks the composer region for extra rows, on top of the metric
// table's own, and reports nothing — the frame simply gets taller there and
// shorter above.
//
// It exists because the composer's inline completions (the `@` filter and the
// `/` line) draw INSIDE the composer's rectangle, below the draft, and the
// metric table budgets that rectangle for a draft and two strips. A list that
// had to fit in the one spare row would show one candidate out of seventeen,
// which is a list only in the sense that it is not zero.
//
// It is deliberately a REQUEST and not a size. The layout still solves top-down
// from the terminal's own height, so a short window gives back less than was
// asked for and gives back nothing at all rather than eating the transcript
// whole — the pane contract's promise that a pane is told its rectangle and
// takes no row from anyone else, kept while letting the region breathe.
//
// Zero restores the table's number, and calling it with the value it already
// has costs nothing: the layout is only re-solved when the answer moved.
func (s *Shell) GrowComposer(rows int) {
	if rows < 0 {
		rows = 0
	}
	if s.composerGrow == rows {
		return
	}
	s.composerGrow = rows
	s.relayout()
}

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

// THE TERMINAL HOOK CONTRACT — internal/tui2/chat wires to exactly these five
// members and nothing else in this file.
//
// The shape is deliberate. Four of the five return a tea.Cmd, because the bytes
// they produce leave the program beside the frame and Bubble Tea owns the
// output; a caller returns the command the way it returns any other, and a
// command that would write nothing is nil, so `return s.SetBusy(true)` on an
// already-busy shell is free. Nothing here starts a goroutine, sleeps, or reads
// the clock outside the notifier, so every one of them is safe to call from
// inside Update.
//
// A returned command must be returned onwards. There is no background writer
// and no queue that drains itself: the command IS the write. Dropping one
// drops bytes, and because the shell's own state has already moved, dropping
// [Shell.SetBusy]'s command means the taskbar disagrees with s.busy until the
// next transition puts them back in step. This is stated rather than defended
// against, because the alternative — a shell that writes to the terminal from
// outside Bubble Tea's output — is the bug that atomic frames exist to prevent.
//
//	Notify(kind, title, body) tea.Cmd  one of the three events reached the
//	                                   interruption budget (10.5.27)
//	SetAttention(n int)                how many things are waiting for the human;
//	                                   drives the title, no command needed
//	SetBusy(running bool) tea.Cmd      a turn is streaming / has stopped;
//	                                   drives OSC 9;4 taskbar progress
//	MarkPrompt() tea.Cmd               a user message was just committed to the
//	                                   transcript (OSC 133 A)
//	Linker() Linker                    the OSC 8 helper for trusted chrome
//
// What the contract deliberately does NOT offer: a way to send arbitrary bytes,
// a fourth notification kind, a percentage for the progress bar, and a
// notification on progress. Each of those is a door 10.5.27 closed on purpose.

// Notify raises one of the three interruption-budget events.
//
// It returns the command that delivers it, or nil — and nil is the common
// case, because most calls are suppressed: the terminal is focused, the
// operator turned notifications off, or this exact event was already announced.
// The caller does not need to know which; "I noticed something worth
// interrupting for" is the caller's whole job, and whether that becomes an
// interruption is this shell's.
func (s *Shell) Notify(kind AttentionKind, title, body string) tea.Cmd {
	s.queue(s.note.emit(s.term, s.caps, kind, title, body))
	return s.flush()
}

// SetAttention records how many things are waiting for a human. The count
// reaches the terminal through the title, which Bubble Tea diffs for us, so an
// unchanged count costs no bytes and this returns no command.
//
// It is a count and not a list on purpose (7.2): the title is glanced at from
// another workspace, and "is anything waiting" is the only question a glance
// can ask.
func (s *Shell) SetAttention(n int) {
	if n < 0 {
		n = 0
	}
	s.attn = n
}

// Attention reports the current count.
func (s *Shell) Attention() int { return s.attn }

// SetBusy says whether a turn is running. It drives OSC 9;4 taskbar progress:
// indeterminate while something streams, cleared when it stops.
//
// Progress never becomes a notification (10.5.27). This is the whole of the
// progress channel, and it is two states wide.
func (s *Shell) SetBusy(running bool) tea.Cmd {
	if s.busy == running {
		return nil
	}
	s.busy = running
	if s.term.Progress {
		s.queue(progressBytes(running))
	}
	return s.flush()
}

// Busy reports whether the shell believes a turn is running.
func (s *Shell) Busy() bool { return s.busy }

// MarkPrompt marks the point where a user message was committed, so the
// terminal's own "jump to previous prompt" navigates the conversation (7.2).
//
// SEAM — internal/tui2/chat: call this once per committed user message, at the
// moment it is appended to the transcript.
//
// It is called from here and not from a block renderer, and that is forced
// rather than chosen. A prompt mark is a position, and positions in this
// surface are owned by the compositor's cell buffer, which carries runes,
// styles and hyperlinks and has no room for a semantic zone — an OSC 133
// written into a pane's rows would be parsed away during composition and never
// reach the terminal. So the mark is written beside the frame, at the moment
// the fact is true, and internal/tui2/blocks needed no seam for it: blocks has
// no notion of who wrote a block anyway, so a marker hook there would have had
// to be told, by chat, exactly what chat can tell us directly.
//
// The honest limit, stated where it is implemented: inside the alt screen the
// mark lands wherever the cursor happens to be, so a terminal that anchors
// prompt zones to a row anchors this one to the row we were on. Terminals that
// keep a list of marks — which is what every "previous prompt" binding actually
// walks — get exactly what they need.
func (s *Shell) MarkPrompt() tea.Cmd {
	if !s.term.PromptMarks {
		return nil
	}
	s.queue(promptMarkBytes)
	return s.flush()
}

// Linker returns the OSC 8 helper for trusted chrome (5.21). The result is a
// value: a renderer can hold it, and it renders plain text when hyperlinks are
// off, so there is no capability check at the call site.
//
// It links what the caller authored. It never scans content for paths to
// linkify — internal/sanitize strips OSC 8 out of model and tool output for
// exactly that reason, and a helper that put one back would be walking around
// the chokepoint from the inside.
func (s *Shell) Linker() Linker { return Linker{on: s.term.Hyperlinks} }

// TerminalOptions reports the channels this shell was permitted.
func (s *Shell) TerminalOptions() TerminalOptions { return s.term }

// queue appends an out-of-band write. Empty is the ordinary answer from every
// producer above, and appending nothing is what makes an unchanged fact cost
// nothing.
func (s *Shell) queue(seq string) {
	if seq != "" {
		s.out = append(s.out, seq)
	}
}

// flush turns the queue into one command and empties it. One write per flush
// rather than one per sequence: a notification at the same instant as a
// progress change is two escapes and should be one syscall.
func (s *Shell) flush() tea.Cmd {
	switch len(s.out) {
	case 0:
		return nil
	case 1:
		seq := s.out[0]
		s.out = s.out[:0]
		return tea.Raw(seq)
	default:
		seq := strings.Join(s.out, "")
		s.out = s.out[:0]
		return tea.Raw(seq)
	}
}

// withFlush attaches any pending out-of-band writes to a command. It keeps nil
// meaning nil: a message that changed nothing still returns no command, which
// is what the resize-storm test measures and what the bandwidth budget wants.
func (s *Shell) withFlush(cmd tea.Cmd) tea.Cmd {
	pending := s.flush()
	switch {
	case pending == nil:
		return cmd
	case cmd == nil:
		return pending
	default:
		return tea.Batch(cmd, pending)
	}
}

// Init asks the terminal the questions whose answers change how we draw, and
// the one whose answer changes how we interrupt.
func (s *Shell) Init() tea.Cmd { return negotiate() }

// Update folds a message into the shell.
func (s *Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return s, s.withFlush(s.sizeChanged(msg.Width, msg.Height))

	case resizeSettledMsg:
		if msg.epoch == s.resizeEpoch {
			s.resizeArmed = false
			s.applySize()
		}
		return s, s.withFlush(nil)

	case tea.KeyPressMsg:
		return s, s.withFlush(s.key(msg))

	case tea.MouseMsg:
		return s, s.withFlush(s.mouse(msg))

	case tea.FocusMsg:
		// Focus reporting is never requested (View.ReportFocus stays false,
		// 10.1.2), so these arrive only when something else in the terminal's
		// history turned it on. We take the fact when it is offered and require
		// it never: the notifier's gate treats "we were never told" as "notify".
		s.note.setFocus(true)
		return s, s.withFlush(nil)

	case tea.BlurMsg:
		s.note.setFocus(false)
		return s, s.withFlush(nil)
	}

	// Everything else is the terminal answering a question we asked at start.
	if s.caps.observe(msg) {
		s.dirty = true
	}
	return s, s.withFlush(nil)
}

// View declares the frame and the terminal state that frame wants. Bubble Tea
// v2's declarative view management is the point: alt screen and mouse capture
// are properties of what we are showing, not commands scattered across a
// startup path and three toggles.
func (s *Shell) View() tea.View {
	v := tea.NewView(s.render())

	// Alt screen is not negotiable (8.1.1).
	v.AltScreen = true

	// All motion, not cell motion — and the reasoning is the doctrine's own,
	// re-weighed rather than reversed.
	//
	// 10.1.1 makes bandwidth the metric, and the earlier reading took that to
	// forbid a message per mouse move. But the cost that matters is the FRAME,
	// not the report: a motion report is six inbound bytes on a link whose
	// inbound direction carries keystrokes and nothing else, and it produces
	// output only when the pointer crosses from one target to a DIFFERENT one.
	// A pointer sweeping the rail therefore costs one repainted row per row
	// crossed and zero bytes for every cell in between — which is exactly
	// "bandwidth proportional to what actually changed", not an exception to it.
	//
	// What it buys is the other half of 5.22 rule 5: an interactive chip has to
	// SAY it is interactive, and on a surface with no boxes and no icon bars
	// (5.21) the only thing left to say it with is the target answering the
	// pointer. Without the report there is no honest way to draw that, and
	// affordances that cannot be seen are the failure 5.22 exists to prevent.
	v.MouseMode = tea.MouseModeAllMotion

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
	if !s.linear {
		for _, sl := range s.layout.Slots {
			if sl.ID != LayerComposer {
				continue
			}
			p, ok := s.panes[LayerComposer].(interface {
				CaretAt(width, height int) (int, int, bool)
			})
			if !ok {
				break
			}
			if x, y, on := p.CaretAt(sl.Rect.Dx(), sl.Rect.Dy()); on {
				// §8/§11's third motion: a blinking block on the caret. Bubble
				// Tea writes the DECSCUSR and restores the terminal's default
				// on quit, suspend and its panic teardown — which raw bytes
				// could not promise.
				v.Cursor = &tea.Cursor{
					Position: tea.Position{X: sl.Rect.Min.X + x, Y: sl.Rect.Min.Y + y},
					Shape:    tea.CursorBlock,
					Blink:    true,
				}
			}
			break
		}
	}

	// The title carries the attention count and nothing else (7.2). It is
	// declared rather than written: Bubble Tea's renderer compares it with the
	// last one and emits OSC 2 only when it moved, so an idle surface that
	// re-renders sixty times a second sends the title zero times. Leaving it
	// empty when titles are off means the renderer never emits one at all —
	// not an empty one.
	if s.term.Title {
		v.WindowTitle = attentionTitle(s.term.AppName, s.attn)
	}

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
	metrics := s.metrics
	metrics.ComposerHeight += s.composerGrow
	s.layout = solveInto(s.layout.Slots, s.width, s.height, metrics, mode{
		Linear:      s.linear,
		ScopeOpen:   s.scopeOpen,
		RailHidden:  s.railHidden,
		OverlayOpen: s.overlayOpen,
	})
	s.comp.setLayout(s.layout)

	// A pane that is no longer on screen cannot hold focus, and a stale hit
	// record would name a region that is not there.
	if _, ok := s.comp.rect(s.focus); !ok {
		s.focus = s.firstFocusable()
	}
	s.clearHover()
	s.hitOK = false
	s.hitID = LayerNone
	s.dirty = true
}

// key handles the bindings the shell owns. Everything else is offered to the
// focused pane, which is where Wave 3's real keyboard lives.
func (s *Shell) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		// Clear the taskbar first. A progress state is the one thing here that
		// outlives the process — Bubble Tea restores the title on its way out,
		// but nobody owns an indeterminate taskbar bar except whoever set it,
		// and a bar left spinning after the program exits is our litter on the
		// user's dock. Sequence, not Batch: the reset has to reach the terminal
		// before the program stops writing to it.
		if clear := s.SetBusy(false); clear != nil {
			return tea.Sequence(clear, tea.Quit)
		}
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
	case tea.MouseMotionMsg:
		// A bare pointer move is a hover and nothing else. It never reaches
		// PaneMouse, never moves focus and never returns a command: 5.14 allows
		// one cursor, and the pointer resting somewhere is not it.
		s.hover(id, local, ok)
		return nil
	}
	// A floating dialog is modal by Z ORDER, which covers the cells it draws
	// and nothing else. §16's OVERLAY DISMISSAL wants the rest of the frame
	// too: a click outside the panel closes the overlay and must not also do
	// whatever it landed on. The boundary is what knows where "outside" begins
	// (internal/tui2/dialogchrome), so the click is handed THERE rather than
	// answered here — the shell routes, the ring decides.
	if _, isClick := msg.(tea.MouseClickMsg); isClick && s.overlayOpen &&
		id != LayerOverlay && id != LayerDialogChrome {
		if rect, sited := s.comp.rect(LayerDialogChrome); sited {
			id, local, ok = LayerDialogChrome, image.Pt(pos.X, pos.Y).Sub(rect.Min), true
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

// hover moves the pointer preview from one pane to another.
//
// The pane the pointer LEFT is told first, so the frame never holds two
// highlights for one pointer, and each pane answers whether the move actually
// changed its bytes. A pointer travelling across cells inside one target
// answers false twice and the shell stays clean — which is what makes an idle
// screen under a moving pointer cost nothing.
func (s *Shell) hover(id LayerID, local image.Point, ok bool) {
	if s.hoverOK && (!ok || s.hoverID != id) {
		if p, okp := s.paneFor(s.hoverID).(PaneHover); okp && p.Hover(image.Point{}, false) {
			s.dirty = true
		}
	}
	s.hoverID, s.hoverOK = id, ok
	if !ok {
		return
	}
	if p, okp := s.paneFor(id).(PaneHover); okp && p.Hover(local, true) {
		s.dirty = true
	}
}

// clearHover drops the pointer preview outright. A layout change is the case
// that needs it: the cell the pointer is over may now belong to a different
// pane, and nothing will say so until the pointer moves again.
func (s *Shell) clearHover() {
	if !s.hoverOK {
		return
	}
	if p, ok := s.paneFor(s.hoverID).(PaneHover); ok && p.Hover(image.Point{}, false) {
		s.dirty = true
	}
	s.hoverOK = false
	s.hoverID = LayerNone
}

// setFocus moves the conversation. The status line and the dialog's margin are
// chrome and never take focus — clicking a footer must not change what the
// composer is bound to, and clicking the ring around a dialog must not move the
// conversation to a region that holds no content.
func (s *Shell) setFocus(id LayerID) {
	if !focusable(id) || id == s.focus {
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
		if focusable(sl.ID) {
			return sl.ID
		}
	}
	return LayerNone
}

// focusable is the one list of regions the conversation can move to. Chrome —
// the footer that explains the surface, the ring that bounds a dialog — is
// drawn, is clickable, and is never talked to.
func focusable(id LayerID) bool {
	switch id {
	case LayerNone, LayerStatus, LayerDialogChrome:
		return false
	}
	return id < numLayers
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
		switch p := s.paneFor(sl.ID); {
		case p != nil:
			content = p.Render(w, h)
		case sl.ID == LayerDialogChrome:
			// The dialog's boundary is the shell's to draw, because the shell is
			// what decided the dialog floats (layout.go). A caller that wants it
			// tinted binds its own pane and takes the first branch.
			content = dialogChrome(w, h)
		default:
			content = s.placeholder(sl.ID, w, h)
		}
		s.comp.setContent(sl.ID, content)
	}
	s.frame = s.comp.render()
	s.dirty = false
	return s.frame
}
