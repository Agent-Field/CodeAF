package chat

import (
	"image"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
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

// statusPane is the contextual footer (10.5.22): a registry of columns that
// drop lowest-priority-first rather than wrapping. It says where you are, who
// is answering and whether anything is wrong — and nothing about this turn's
// cost or context, which belong on the composer's meta strip and never mix.
type statusPane struct {
	style *tokens.Styler

	session string
	model   string
	turns   int
	live    bool
	err     string
}

var _ tui2.Pane = (*statusPane)(nil)

// Render composes the footer at width.
func (p *statusPane) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	phase := "ready"
	if p.live {
		phase = "answering"
	}
	cols := []string{"aforge v2", phase, "session " + shortID(p.session)}
	if p.model != "" {
		cols = append(cols, p.model)
	}
	cols = append(cols, strconv.Itoa(p.turns)+" turns")

	// Truncation happens before painting, always. Cutting a painted string can
	// take its reset sequence with it and leave the rest of the frame wearing
	// the footer's colour.
	line, state, hue := strings.Join(cols, " "+tokens.GlyphSeparator+" "),
		blocks.StateChrome, blocks.HueNone
	if p.err != "" {
		line, state, hue = tokens.GlyphFailed+" "+p.err, blocks.StateSettled, blocks.HueBroken
	}
	line = blocks.Truncate(line, width)
	if p.style == nil {
		return line
	}
	return p.style.Paint(line, state, hue)
}

// railPane is the scope map's place, held honestly empty.
//
// Wave 3 fills it with live task cards and the scope ladder (5.15). Until then
// it says what it is and what is in it, because a rail that drew a fake card
// would be the first lie in a surface built to stop telling them — and a rail
// that drew a debug box would tell the operator about the build instead of
// about their work.
type railPane struct {
	style   *tokens.Styler
	session string
}

var _ tui2.Pane = (*railPane)(nil)

// Render draws the empty rail.
func (p *railPane) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := []string{
		tokens.GlyphScopeUp + " scope",
		"",
		"session " + shortID(p.session),
		"",
		"no live work",
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = blocks.Truncate(lines[i], width)
		if p.style != nil {
			state := blocks.StateChrome
			if i == 0 {
				state = blocks.StateSettled
			}
			lines[i] = p.style.Paint(lines[i], state, blocks.HueNone)
		}
	}
	return strings.Join(lines, "\n")
}

// shortID abbreviates an identifier for a strip of chrome. A session id is a
// UUID and the first octet is what a person actually reads back.
func shortID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return tokens.GlyphMissing
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
