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
	// homes is the one lens whose rows are not blocks (5.24). When it is set
	// the pane draws it INSTEAD of a transcript, because a notebook fact and a
	// charter are not turns of conversation and dressing them as messages would
	// be the transcript claiming they were said. It is a function rather than
	// the View itself so this pane never learns what a home is.
	homes func(width, height int) []string
	// answer performs an option row a pointer landed on. It is a function for
	// the reason every other pointer seam here is: the pane resolves a cell to
	// a row, and the ACT belongs to the app — this one is question.go's own
	// [App.answer], the same call the digits make.
	answer func(block *messageBlock, number int) tea.Cmd
	// fold opens or closes the one block a pointer landed on. Same seam as
	// answer, for the same reason: the pane resolves a cell to a row and the ACT
	// belongs to the app, which is where the reader's per-row decision is
	// remembered across the rebuilds that would otherwise lose it (disclose.go).
	fold func(block *messageBlock) tea.Cmd
	// focus asks for the keyboard, because pointing IS looking (5.14). The
	// transcript's own keys are the scroll vocabulary, which the app's ladder
	// routes here whatever holds custody; what a click on the conversation
	// actually settles is that the MAP no longer does, so the next letter is a
	// letter and the next j is a j. Same seam as answer and fold: the pane knows
	// it was pointed at, the app knows where the keyboard is.
	focus func() bool
	// width is the rectangle the pane was last drawn at, kept because an option
	// row's position depends on how tall its block rendered.
	width int
	// hoverBlock and hoverOption are the row the pointer rests on: the block
	// index and the one-based option number. Zero option means no row.
	hoverBlock  int
	hoverOption int
	// hoverFold is the block whose fold row the pointer rests on, one-based, or
	// zero for none. It is kept apart from hoverBlock because the two marks are
	// different marks on different rows: an option row moves the question's own
	// answer cursor, a fold row only brightens. One field could not clear the
	// right one when the pointer crossed from a card into the question under it.
	hoverFold int
}

var (
	_ tui2.Pane      = (*transcriptPane)(nil)
	_ tui2.PaneKeys  = (*transcriptPane)(nil)
	_ tui2.PaneMouse = (*transcriptPane)(nil)
	_ tui2.PaneHover = (*transcriptPane)(nil)
)

// Render assembles one screenful. The size is applied here rather than at
// layout time because the pane is told its rectangle and nothing else, which is
// exactly the information a viewport needs.
func (p *transcriptPane) Render(width, height int) string {
	if p.homes != nil {
		return strings.Join(p.homes(width, height), "\n")
	}
	if p.transcript == nil {
		return ""
	}
	p.transcript.SetSize(width, height)
	p.width = width
	return strings.Join(p.transcript.Frame(p.now()).Rows, "\n")
}

// optionAt resolves a pane-local cell to an answerable option row: which block,
// and which one-based option on it.
//
// The x is deliberately not consulted. An option row is a whole row of the
// conversation and the reader is pointing at the CHOICE, not at the four
// characters of its label — asking them to hit the words would be a target
// narrower than the thing it stands for.
func (p *transcriptPane) optionAt(y int) (*messageBlock, int, int, bool) {
	if p.transcript == nil || p.homes != nil {
		return nil, 0, 0, false
	}
	index, line, ok := p.transcript.BlockAtScreenRow(y)
	if !ok {
		return nil, 0, 0, false
	}
	block, ok := p.transcript.Block(index).(*messageBlock)
	if !ok {
		return nil, 0, 0, false
	}
	number, ok := block.optionAtLine(line, p.width)
	if !ok {
		return nil, 0, 0, false
	}
	return block, index, number, true
}

// foldAt resolves a pane-local cell to a block whose fold that row opens.
//
// It is [optionAt] one row up: the same BlockAtScreenRow lookup against the
// layout the last frame actually produced, and the same refusal to consult x.
// Nothing here is recorded during Render — Part 2's anti-pattern 14 — because
// the transcript's own cache IS the map, and a click cannot land on a row the
// paint did not draw.
func (p *transcriptPane) foldAt(y int) (*messageBlock, int, bool) {
	if p.transcript == nil || p.homes != nil {
		return nil, 0, false
	}
	index, line, ok := p.transcript.BlockAtScreenRow(y)
	if !ok {
		return nil, 0, false
	}
	block, ok := p.transcript.Block(index).(*messageBlock)
	if !ok || !block.isFoldRow(line) {
		return nil, 0, false
	}
	return block, index, true
}

// Key handles the scroll vocabulary. Everything else on the keyboard belongs to
// the composer and never reaches here — see the app's key ladder.
func (p *transcriptPane) Key(msg tea.KeyPressMsg) tea.Cmd {
	if p.transcript == nil {
		return nil
	}
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
func (p *transcriptPane) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	// A click on an option row answers the question, which is 5.22's law read
	// at the one place it matters most: a blocked human is the most expensive
	// state this product has (5.9), and the row already names its own key. The
	// pointer is a second hand on that key and not a second way to answer —
	// [App.answer] is what the digit reaches too.
	if click, isClick := msg.(tea.MouseClickMsg); isClick {
		if click.Button != tea.MouseLeft {
			return nil
		}
		// The keyboard comes with the pointer, before the row is resolved and
		// whether or not it resolves to anything: a click on prose answers no
		// question and opens no fold, and it is still the reader saying "I am
		// reading this, not walking the map".
		if p.focus != nil {
			p.focus()
		}
		if p.answer != nil {
			if block, _, number, ok := p.optionAt(local.Y); ok {
				return p.answer(block, number)
			}
		}
		// A click on a fold row opens THAT row, and a click on it again shuts
		// it — 7.2's per-block expand/collapse, reached by the hand that is
		// already pointing at the `▸`. The order matters and only trivially:
		// an option row is never a block's header, so the two targets cannot
		// overlap, and asking the more consequential question first is the
		// safer habit to leave behind.
		if p.fold != nil {
			if block, _, ok := p.foldAt(local.Y); ok {
				return p.fold(block)
			}
		}
		return nil
	}
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok || p.transcript == nil {
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

// Hover previews the option row under the pointer by moving the block's OWN
// answer cursor — the same cursor ↑/↓ walk (question.go's moveChoice) and the
// same mark it draws.
//
// This is the one hover on the surface that touches a cursor, and it is
// allowed for the reason 5.14 states rather than in spite of it: there is only
// one answer cursor, it belongs to the question, and nothing else on the screen
// is reading it. A hover that lit an option a different way would be a second
// mark for one meaning. Enter still answers whatever the mark is on, so the
// pointer and the keyboard agree about which choice is live.
func (p *transcriptPane) Hover(local image.Point, inside bool) bool {
	moved := p.hoverFoldRow(local, inside)

	block, index, number, ok := (*messageBlock)(nil), 0, 0, false
	if inside {
		block, index, number, ok = p.optionAt(local.Y)
	}
	if !ok {
		index, number = 0, 0
	}
	if p.hoverBlock == index && p.hoverOption == number {
		return moved
	}
	// Clear the mark on the row the pointer left, then set it where it is now.
	if p.hoverOption != 0 && p.transcript != nil && p.hoverBlock < p.transcript.Len() {
		if old, isMessage := p.transcript.Block(p.hoverBlock).(*messageBlock); isMessage {
			old.SetChosen(0)
		}
	}
	p.hoverBlock, p.hoverOption = index, number
	if block != nil {
		block.SetChosen(number)
	}
	return true
}

// hoverFoldRow makes the `▸` row LOOK like the door it now is.
//
// 5.22's amendment is that an interactive control may never live permanently in
// the dimmest tier — "dim at rest, secondary on focus" — and until this lane the
// fold hint lived there permanently with no focus to rise to. It rises one tier
// under the pointer (13.14's hover law, tokens.Promote, never a band), and that
// promotion is the whole difference between chrome a reader reads past and an
// affordance they reach for.
//
// It touches paint and nothing else. No cursor moves, no command is returned,
// and the pane's own state is one int — which is what keeps the hover door the
// side-effect-free door its type promises.
func (p *transcriptPane) hoverFoldRow(local image.Point, inside bool) bool {
	index := 0
	if inside {
		if _, at, ok := p.foldAt(local.Y); ok {
			index = at + 1
		}
	}
	if p.hoverFold == index {
		return false
	}
	if p.hoverFold != 0 && p.transcript != nil && p.hoverFold-1 < p.transcript.Len() {
		if old, isMessage := p.transcript.Block(p.hoverFold - 1).(*messageBlock); isMessage {
			old.SetHovered(false)
		}
	}
	p.hoverFold = index
	if index != 0 {
		if block, isMessage := p.transcript.Block(index - 1).(*messageBlock); isMessage {
			block.SetHovered(true)
		}
	}
	return true
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

	// run performs a footer word, and pop is the breadcrumb's way out. Both are
	// functions for the reason the rail's are: the row knows which word was
	// pointed at and nothing else — the acts belong to the app, and they are
	// the same acts the keyboard reaches.
	run func(entryID string) tea.Cmd
	pop func() tea.Cmd
	// hover is the word the pointer is resting on, or "" for none.
	hover string
	// lastWidth is the width the row was last drawn at, so a pointer can be
	// resolved against the row that is actually on screen. It is written by
	// Render — the one number a pane may keep, because it IS the rectangle the
	// pane was told about and nothing else can know it.
	lastWidth int
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

var (
	_ tui2.Pane      = (*statusPane)(nil)
	_ tui2.PaneMouse = (*statusPane)(nil)
	_ tui2.PaneHover = (*statusPane)(nil)
)

// Render composes the footer at width.
func (p *statusPane) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	p.lastWidth = width
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
	return p.bar.Render(p.focusContext(width), width)
}

// focusContext is what the row says right now, built once so the paint and the
// pointer are answering about the same row. Every field it leaves at zero is a
// column that does not appear — the affordance never lies, and that includes
// lying by presence.
func (p *statusPane) focusContext(width int) footer.FocusContext {
	return footer.FocusContext{
		Verbs:         p.offeredVerbs(width),
		Input:         p.input,
		Hint:          p.hint,
		EscInterrupts: p.escInterrupts,
		Attention:     p.attention,
		KeyMode:       p.keyMode,
		KeyModeCount:  p.keyCount,
		Health:        p.health(),
		ScopeTail:     p.scopeTail(),
		Hover:         p.hover,
	}
}

// Mouse makes the footer's words live (5.22 rule 4 and rule 5: the contextual
// footer is drawn FROM the registry, so a click on one of its verbs runs the
// registry row it was drawn from).
//
// This is the click parity that costs the least chrome: nothing is added to the
// screen, and the row that has been advertising `? help` since this surface
// existed becomes the door it names. The footer never takes focus — the shell's
// focusable list refuses it — so clicking a verb does not move the cursor, only
// performs the verb, which is what a button is.
func (p *statusPane) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft || p.bar == nil {
		return nil
	}
	id, found := p.bar.TargetAt(p.focusContext(p.lastWidth), p.lastWidth, local.X)
	if !found {
		return nil
	}
	switch id {
	case footer.ScopeTarget:
		if p.pop != nil {
			return p.pop()
		}
		return nil
	case footer.HelpTarget:
		id = helpEntryID
	}
	if p.run == nil {
		return nil
	}
	return p.run(id)
}

// Hover lights the word under the pointer and nothing else.
func (p *statusPane) Hover(local image.Point, inside bool) bool {
	next := ""
	if inside && p.bar != nil {
		if id, ok := p.bar.TargetAt(p.focusContext(p.lastWidth), p.lastWidth, local.X); ok {
			next = id
		}
	}
	if p.hover == next {
		return false
	}
	p.hover = next
	return true
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
