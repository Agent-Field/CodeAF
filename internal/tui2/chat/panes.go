package chat

import (
	"image"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/footer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The three panes this wave binds into the shell: the transcript, the awaiting
// line that lives inside it, and the two strips of chrome around it.
//
// Each one is a [tui2.Pane] and nothing more. None of them knows where it is on
// screen, none of them reserves a row from anyone else, and each one calls
// Invalidate when its content moved for a reason the shell could not observe —
// a delta arriving, a poll landing, a clock tick. That is the whole contract,
// and keeping to it is what makes the bytes on the wire proportional to what
// actually changed.

// scrollStep is how far a wheel notch or an arrow moves the transcript. Three
// rows is the conventional notch and is small enough that a fast scroll still
// reads as motion rather than as teleportation.
const scrollStep = 3

// transcriptPane is the conversation: the block list, the anchor-preserving
// viewport, and nothing else. Every decision about WHAT is in the list belongs
// to the app; this pane owns only how much of it is on screen.
type transcriptPane struct {
	transcript *blocks.Transcript
	now        func() time.Time
	invalidate func()
}

var (
	_ tui2.Pane      = (*transcriptPane)(nil)
	_ tui2.PaneKeys  = (*transcriptPane)(nil)
	_ tui2.PaneMouse = (*transcriptPane)(nil)
)

// Render assembles one screenful. The size is applied here rather than at
// layout time because the pane is told its rectangle and nothing else, which is
// exactly the information a viewport needs.
func (p *transcriptPane) Render(width, height int) string {
	p.transcript.SetSize(width, height)
	return strings.Join(p.transcript.Frame(p.now()).Rows, "\n")
}

// Key handles the scroll vocabulary. Everything else on the keyboard belongs to
// the composer and never reaches here — see the app's key ladder.
func (p *transcriptPane) Key(msg tea.KeyPressMsg) tea.Cmd {
	before := p.transcript.YOffset()
	switch msg.String() {
	case "pgup":
		p.transcript.PageUp()
	case "pgdown":
		p.transcript.PageDown()
	case "shift+up":
		p.transcript.ScrollBy(-scrollStep)
	case "shift+down":
		p.transcript.ScrollBy(scrollStep)
	case "ctrl+home":
		p.transcript.GotoTop()
	case "ctrl+end":
		p.transcript.GotoBottom()
	default:
		return nil
	}
	if p.transcript.YOffset() != before {
		p.invalidate()
	}
	return nil
}

// Mouse handles the wheel. The point is pane-local and unused: a wheel notch
// means the same thing everywhere inside the transcript.
func (p *transcriptPane) Mouse(msg tea.MouseMsg, _ image.Point) tea.Cmd {
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		return nil
	}
	before := p.transcript.YOffset()
	switch wheel.Button {
	case tea.MouseWheelUp:
		p.transcript.ScrollBy(-scrollStep)
	case tea.MouseWheelDown:
		p.transcript.ScrollBy(scrollStep)
	default:
		return nil
	}
	if p.transcript.YOffset() != before {
		p.invalidate()
	}
	return nil
}

// awaitingBlock is the awaiting line (5.20 rule 6, 8.2.21).
//
// It is a live block that sits at the tail of the transcript for exactly as
// long as a turn is being answered in THIS room, and it carries the interrupt
// hint only while esc would in fact interrupt. That condition is the whole
// rule: interruptibility that is not visible does not exist, and a hint for a
// key that would do something else is worse than no hint at all. Whatever this
// line says is what esc will do.
//
// It never finalizes and it is one row, so the committed prefix above it is
// never rebuilt on its account: a spinner tick costs one row, not a transcript.
type awaitingBlock struct {
	clock *blocks.Clock
	style blocks.Styler
	// phase is the word the line carries: what the wait is for.
	phase string
	// interruptible says esc would in fact stop this turn.
	interruptible bool
	// motion is false in linear mode (10.1.5), where the glyph stops moving and
	// the line says the same thing standing still.
	motion bool

	rows [1]string
}

var _ blocks.Block = (*awaitingBlock)(nil)

// ID is stable: there is at most one awaiting line in a room.
func (b *awaitingBlock) ID() string { return "awaiting" }

// IsFinalized is always false. The awaiting line is the live region.
func (b *awaitingBlock) IsFinalized() bool { return false }

// SettledRows promises nothing: the glyph moves.
func (b *awaitingBlock) SettledRows(int) int { return 0 }

// Version never moves; the block is never finalized, so nothing can mutate
// after the fact.
func (b *awaitingBlock) Version() uint64 { return 0 }

// End is live, always.
func (b *awaitingBlock) End() blocks.EndState { return blocks.EndLive }

// Rows draws the line, degrading by dropping the hint and then the phase rather
// than by wrapping.
func (b *awaitingBlock) Rows(width int) []string {
	st := b.style
	if st == nil {
		st = blocks.Plain
	}
	glyph := tokens.GlyphWorking
	if b.motion && b.clock != nil {
		glyph = b.clock.Glyph()
	}
	tail := " " + b.phase
	if b.interruptible {
		const hint = " " + tokens.GlyphSeparator + " esc interrupt"
		if blocks.Width(glyph+tail+hint) <= width {
			tail += hint
		}
	}
	// The glyph and the words are painted separately — accent for the live
	// glyph, chrome for the words (8.1.6) — so the widths are measured
	// separately too. Slicing a painted string by bytes is how a row ends up
	// carrying half an escape sequence.
	glyphWidth := blocks.Width(glyph)
	if width <= glyphWidth {
		b.rows[0] = st.Paint(blocks.Truncate(glyph, width), blocks.StateLive, blocks.HueAlive)
		return b.rows[:]
	}
	b.rows[0] = st.Paint(glyph, blocks.StateLive, blocks.HueAlive) +
		st.Paint(blocks.Truncate(tail, width-glyphWidth), blocks.StateChrome, blocks.HueNone)
	return b.rows[:]
}

// statusPane is the contextual footer (10.5.22, 5.22 rule 4): a registry of
// columns that drop lowest-priority-first rather than wrapping.
//
// The columns, the priorities and the fitting all come from
// internal/tui2/footer, which already implements the mechanic against
// [tokens.FitFooter] — re-implementing the drop order here would be two copies
// of one law drifting apart. This type's whole job is to fill a
// [footer.FocusContext] honestly from state the app actually holds, and every
// field it leaves at zero is a column that does not appear (the affordance
// never lies, 5.20, and that includes lying by presence).
//
// 10.5.23's split is the hard line: system health and the open-question count
// live here; this-turn cost and context live on the composer's meta strip and
// are not duplicated. Nothing on this row mentions money.
type statusPane struct {
	style *tokens.Styler
	bar   *footer.Model

	// Filled once, at construction.
	session string
	model   string

	// Filled by the poll and the composer's turn; poll.go writes turns and err.
	turns int
	live  bool
	err   string

	// Filled every frame by the app's refresh, from the state that decides
	// them. See App.refresh.
	verbs         []registry.Entry
	input         footer.InputState
	hint          string
	escInterrupts bool
	attention     int
	// keyMode and keyCount are 5.22's digit-precedence answer: what a bare
	// number does RIGHT NOW. The footer says it because the transcript cannot —
	// an option row can name its own key, but nothing on screen could otherwise
	// tell a reader that the digits currently belong to a question rather than
	// to the rail.
	keyMode   footer.KeyMode
	keyCount  int
	residency Residency
	// room is the humane name of the room this window is in, and breadcrumb is
	// how deep inside it the reader has navigated. Neither is ever an id
	// (13.3.4); an unnamed room says it is unnamed.
	room       string
	breadcrumb string
	// foldable says the transcript holds a row the receipts fold can act on.
	// The accelerator is only offered while it does — 5.20 rule 3 forbids
	// naming a door that opens nothing.
	foldable bool
}

// offeredVerbs is the verb strip at THIS width: the permanent bindings, plus
// the contextual ones when the door exists and the row can afford to name it.
//
// The width test is not a second fitting algorithm — the footer owns that — it
// is about the GRANULARITY of the one it has. 10.5.22 fits the verb column as a
// unit, so a contextual third entry that overflows does not shorten the row; it
// takes quit and newline off it entirely. Two permanent doors are worth more
// than one contextual accelerator, and below the breakpoint the fold is still
// discoverable where it acts: every collapsible row draws its own expand hint.
func (p *statusPane) offeredVerbs(width int) []registry.Entry {
	if p.foldable && width >= tokens.RailAtWidth {
		return p.verbs
	}
	for i := range p.verbs {
		if p.verbs[i].ID == "key.thread.receipts" {
			return p.verbs[:i:i]
		}
	}
	return p.verbs
}

var _ tui2.Pane = (*statusPane)(nil)

// Render composes the footer at width.
func (p *statusPane) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	// A read that failed is the one state 5.16 hands a whole coloured sentence
	// to: "a whole coloured sentence means something is wrong" is the doctrine,
	// and a surface that cannot reach its own journal is exactly that. It
	// replaces the row rather than joining it, because a footer that kept
	// offering verbs beside a dead store would be advertising doors that no
	// longer open. Truncation happens before painting, always — cutting a
	// painted string can take its reset with it and leave the rest of the frame
	// wearing the footer's colour.
	if p.err != "" {
		line := blocks.Truncate(tokens.GlyphFailed+" "+p.err, width)
		if p.style == nil {
			return line
		}
		return p.style.Paint(line, blocks.StateSettled, blocks.HueBroken)
	}
	if p.bar == nil {
		return ""
	}
	return p.bar.Render(footer.FocusContext{
		Verbs:         p.offeredVerbs(width),
		Input:         p.input,
		Hint:          p.hint,
		EscInterrupts: p.escInterrupts,
		Attention:     p.attention,
		KeyMode:       p.keyMode,
		KeyModeCount:  p.keyCount,
		Health:        p.health(),
		ScopeTail:     p.scopeTail(),
	}, width)
}

// health is 10.5.23's own column: pending-only system states, shown when they
// are pending and absent when they are not.
//
// Which process runs the head is exactly such a state. A resident says nothing,
// because being the one that answers is the ordinary case and the ordinary case
// earns no ink (the same rule v1 states at internal/tui/residency.go, in the
// same words, because it is the same product fact seen from a second surface).
// A visitor says so for as long as it is true, and says what it is waiting on —
// which is the notice that used to go to stderr and got swallowed whole by the
// alt screen, leaving a window that looked like a dead app.
func (p *statusPane) health() []string {
	note := strings.TrimSpace(p.residency.Note)
	if !p.residency.Visitor {
		if note == "" {
			return nil
		}
		return []string{note}
	}
	// "pid 4242" rather than v1's "resident is pid 4242": this row is a fitted
	// column registry, and every cell it can shorten is a cell that survives one
	// breakpoint further down (10.5.22). A visitor that dropped off a
	// 80-column footer to make room for a longer way of saying the same thing
	// would have spent the words on the wrong thing.
	if note == "" && p.residency.PID > 0 {
		note = "pid " + strconv.Itoa(p.residency.PID)
	}
	if note == "" {
		return []string{"visitor"}
	}
	return []string{"visitor " + tokens.GlyphSeparator + " " + note}
}

// scopeTail is the breadcrumb tail, and the lowest-priority column on the row.
//
// It says WHERE the reader is, in the words they navigated by: the room's name,
// and then the scope they have descended into. 13.3.4 is why it can never fall
// back to the session id — 5.14 puts ids in the never-shown tier, and a
// truncated uuid on the footer was the rule being broken in the one place a
// reader looks when they are lost. A room nobody has named says so.
func (p *statusPane) scopeTail() string {
	room := strings.TrimSpace(p.room)
	if room == "" && p.session != "" {
		room = untitledRoom
	}
	if room == "" {
		return ""
	}
	if crumbs := strings.TrimSpace(p.breadcrumb); crumbs != "" {
		room += " " + tokens.GlyphScopeUp + " " + crumbs
	}
	return tokens.GlyphScopeUp + " " + room
}
