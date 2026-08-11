package chat

import (
	"image"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/placeline"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The composer region: three parts, one rectangle.
//
// internal/tui2/composer owns the draft: the rune buffer, the caret, the send
// and newline chords the shell negotiated, the recall ring, bracketed paste,
// and the esc law's first half — a non-empty draft is stashed into the ring,
// never destroyed. This package owns the second half: an esc the composer did
// not consume arrives here as [composer.EscMsg], and only this side knows
// whether the room is streaming and therefore what that esc means (8.2.21).
//
// Above the draft sits internal/tui2/placeline (5.19): one dim row naming where
// this work lands on disk — the question a breadcrumb never answers and the pwd
// a terminal usually gives away for free. Below it sits the meta strip, which
// is 10.5.23's other home: THIS TURN's cost and context, and nothing about
// system health, which belongs to the footer under the region and never mixes.
//
// The three are assembled here rather than by the shell because the shell gives
// a pane its whole rectangle and reserves nothing inside it (pane.go's
// contract). A region that had to be told "you have three rows, one of them is
// mine" would be the shell reaching into a pane, which is exactly the seam the
// v2 layout was built to keep clean.

// composerPane is what the app binds into the composer region.
type composerPane interface {
	tui2.Pane
	tui2.PaneKeys
	tui2.PaneFocus
	tui2.PaneMouse
	tui2.PaneHover
	// Draft is the text currently in the buffer. The footer's input-state axis
	// (10.5.26) is read off it, and nothing else consults it — the app never
	// reaches into the draft to change it.
	Draft() string
	// HintRows is how many rows an open inline completion wants under the
	// draft. The region is budgeted by the layout for a draft and two strips,
	// so a list of candidates needs the region to grow — and only the composer
	// knows whether there is a list. See [App.refresh], which is where the
	// answer becomes rows.
	HintRows() int
}

// composerOptions is the composer's own option struct, named here so the app
// reads as one package rather than two.
type composerOptions = composer.Options

// composerStack is the assembled region.
type composerStack struct {
	place *placeline.Model
	draft *composer.Model
	meta  *metaStrip

	// mode reports what the composer is bound to right now (5.15). It is a
	// function rather than a field because the binding is the app's state and
	// this region renders it; a copy here would be a second truth that ages by
	// one keystroke.
	mode func() composerBind
	// hud draws the bounded live summary above the draft (8.2.8). It returns
	// the rows it wants and never more than it is offered; nil means this frame
	// has a rail and does not need one.
	hud func(width, height int) []string
	// openModels is 5.22 rule 5's "click model chip → model palette", and copy
	// is 5.19's "click/`y` copies" on the place line. Both are functions for
	// the reason every other pointer seam is: this stack resolves a cell to a
	// chip, and the act belongs to the app.
	openModels func() tea.Cmd
	copy       func(text string) tea.Cmd
	// focus is 5.14's "you talk to what you are looking at", read for a hand
	// instead of an eye: POINTING IS LOOKING, so a click anywhere in this
	// rectangle asks for the keyboard. It is a function for the same reason the
	// two above are — this region can tell that it was pointed at and cannot
	// know whether the keyboard is currently on the map, which is the app's
	// flag. It reports whether custody actually moved, because a disabled
	// composer refuses it (App.focusConversation) and a caret must not be placed
	// in a draft nobody may type into.
	focus func() bool
	// The rectangle this stack was last drawn at, so a pointer can be resolved
	// against the row that is actually on screen.
	lastWidth, lastHeight int
	// hoverChip is which of the two chips the pointer is on: "" for neither.
	hoverChip string
}

var (
	_ tui2.Pane      = (*composerStack)(nil)
	_ tui2.PaneKeys  = (*composerStack)(nil)
	_ tui2.PaneFocus = (*composerStack)(nil)
)

// newComposer builds the region the app binds.
func newComposer(opts composerOptions, place *placeline.Model, meta *metaStrip) *composerStack {
	return &composerStack{place: place, draft: composer.New(opts), meta: meta}
}

// Key hands every keystroke to the draft. The place line and the meta strip are
// read-only chrome; neither has an affordance that takes the keyboard.
//
// A DISABLED composer takes nothing. 5.15's one rule is that a settled row's
// composer is disabled, and a disabled composer that quietly accepted a draft
// nobody could send would be the affordance lying in the most frustrating way
// available: the reader types a paragraph and only then finds out.
func (s *composerStack) Key(msg tea.KeyPressMsg) tea.Cmd {
	if s.disabled() {
		return nil
	}
	return s.draft.Key(msg)
}

// Paste is offered only when the draft accepts one, which is how the app's
// paste door finds it (app.go asks the pane for the method rather than assuming
// it).
func (s *composerStack) Paste(msg tea.PasteMsg) tea.Cmd { return s.draft.Paste(msg) }

// Focus flows to both rows that render differently for it: the draft shows its
// caret, and the place line reveals the root's full, unabbreviated path (5.19).
func (s *composerStack) Focus(focused bool) {
	s.draft.Focus(focused)
	if s.place != nil {
		s.place.Focus(focused)
	}
}

// Draft is the current buffer.
func (s *composerStack) Draft() string { return s.draft.Value() }

// -- the region's two chips (5.22 rule 5) ------------------------------------

// chipAt resolves a pane-local cell to one of the region's two doors: the place
// line on the top edge, and the model chip on the meta strip at the bottom.
//
// The rows between them are the draft and its completions, and they are
// deliberately not doors — a click there is the reader putting the keyboard
// back on the composer, which the shell has already done by the time this is
// asked.
func (s *composerStack) chipAt(local image.Point) string {
	if s.lastHeight <= 0 {
		return ""
	}
	switch {
	case s.place != nil && s.lastHeight >= 3 && local.Y == 0:
		if s.place.CopyText() == "" {
			return ""
		}
		return placeChipID
	case s.meta != nil && s.lastHeight >= 2 && local.Y == s.lastHeight-1:
		from, to, ok := s.meta.chipSpan(s.lastWidth)
		if !ok || local.X < from || local.X >= to {
			return ""
		}
		return metaChipID
	}
	return ""
}

// draftBand is where the composer's own rows sit inside this region and how
// many of them there are: the place line takes the top edge, the meta strip the
// bottom, and the bounded HUD borrows from the middle. It restates Render's own
// reservations rather than recording them, so the pointer and the paint answer
// the same question from the same numbers.
func (s *composerStack) draftBand() (top, body int) {
	height := s.lastHeight
	if height <= 0 {
		return 0, 0
	}
	body = height
	if height >= 3 && s.place != nil {
		body--
		top++
	}
	if height >= 2 && s.meta != nil {
		body--
	}
	if s.hud != nil && body > 1 {
		if rows := len(s.hud(s.lastWidth, body-1)); rows > 0 {
			body -= rows
			top += rows
		}
	}
	return top, body
}

// The two chip ids, as the pointer names them. metaChipID is the meta strip's
// own; the place line's is declared here because the place line is a component
// that does not know it is a door.
const placeChipID = "place"

// Mouse takes the keyboard, places the caret, and performs a chip.
//
// The first of those three is the correction this lane exists for. The comment
// that used to stand here said "the draft is where the keyboard already is by
// the time this runs", and it was reading the SHELL's rule (a click focuses the
// layer it hit) as though it were the whole story. It is not: this surface
// keeps its own custody flag, because the map's keyboard is the app's and not
// the shell's (rooms.go), and the shell moving its own LayerID changed nothing
// a key would do. So the map kept the keyboard through a click on the draft,
// and the only way back was ctrl+o — reported verbatim as "clicking on the
// typing part or anywhere does not seem to go there — I have to press ctrl+o".
func (s *composerStack) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft {
		return nil
	}
	// Pointing at the composer is talking to the composer. It happens before
	// anything below decides what the click also DID, because it is true of
	// every cell of this rectangle including the two chips and the empty rows
	// between them — one rule for the whole region, which is what makes it a
	// rule a reader can hold rather than a map they have to learn.
	took := s.focus == nil || s.focus()

	// The candidate list next: it is drawn inside this rectangle, between the
	// two chips, and it is the only part of the region where a row means
	// something on its own. draftTop is where the composer's own rows begin —
	// the place line and the HUD sit above them.
	if s.draft != nil {
		top, body := s.draftBand()
		if body > 0 {
			if cmd, taken := s.draft.ClickHint(s.lastWidth, body, local.Y-top); taken {
				return cmd
			}
			// The caret goes where the finger went. It is offered only to a
			// composer that actually holds the keyboard: placing a caret in a
			// draft that refuses every key would be the affordance lying in the
			// quietest way it can, and a disabled composer refuses (5.15).
			if took && !s.disabled() {
				s.draft.ClickCaret(s.lastWidth, body, local.X, local.Y-top)
			}
		}
	}
	switch s.chipAt(local) {
	case placeChipID:
		// 5.19: the place line is the room's ground, and click or `y` copies
		// it. The component already knows what the full, unabbreviated path is
		// — the same text it reveals on focus — so nothing here re-derives it.
		if s.copy != nil {
			return s.copy(s.place.CopyText())
		}
	case metaChipID:
		if s.openModels != nil {
			return s.openModels()
		}
	}
	return nil
}

// Hover lights whichever chip the pointer is on.
func (s *composerStack) Hover(local image.Point, inside bool) bool {
	moved := false
	if inside && s.draft != nil {
		top, body := s.draftBand()
		if body > 0 && s.draft.HoverHint(s.lastWidth, body, local.Y-top) {
			moved = true
		}
	}
	next := ""
	if inside {
		next = s.chipAt(local)
	}
	if s.hoverChip == next {
		return moved
	}
	s.hoverChip = next
	// The place line's own focus state IS the "show me the whole path" tier
	// (5.19), so a pointer resting on it reveals exactly what focusing it
	// reveals — one meaning, one rendering. The chip is left to the meta
	// strip's own paint, which promotes it a tier.
	if s.place != nil {
		s.place.Focus(s.hoverChip == placeChipID || s.draft.Focused())
	}
	if s.meta != nil {
		s.meta.hover = s.hoverChip == metaChipID
	}
	return true
}

// HintRows is how many rows the draft's open inline completion wants. The stack
// adds nothing to the number: the place line and the meta strip are already in
// the metric table, and what the region is short of is room for the LIST.
func (s *composerStack) HintRows() int { return s.draft.HintRows() }

// KillToStart is the clear-draft verb reached from the `?` sheet or the palette
// rather than from ctrl+u. The chord itself never comes through here — it is an
// ordinary keystroke and Key hands it to the draft like any other — so this
// exists only so the registry row has a handler, and it goes through the same
// [composer.Model.KillToStart] the chord does rather than clearing the buffer a
// second way. A disabled composer has no draft to clear and refuses, on the same
// rule Key states above it.
func (s *composerStack) KillToStart() {
	if s.disabled() {
		return
	}
	s.draft.KillToStart()
}

// Render lays the three parts out from the outside in: the place line takes the
// top edge, the meta strip the bottom, and the draft absorbs whatever is left.
// A region too short for all three loses them in that order — the draft is the
// one part whose absence makes the surface unusable, so it is the last to go.
func (s *composerStack) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	s.lastWidth, s.lastHeight = width, height
	rows := make([]string, 0, height)
	body := height
	wantPlace := height >= 3 && s.place != nil
	wantMeta := height >= 2 && s.meta != nil
	if wantPlace {
		body--
	}
	if wantMeta {
		body--
	}
	// The bounded HUD (8.2.8) borrows from the DRAFT and from nothing else, and
	// never takes its last row. The place line names the ground and the meta
	// strip carries the money; a summary that displaced either would have spent
	// a permanent row on a transient fact. What it cannot fit, its fold line
	// accounts for — that is what "bounded" means here.
	var summary []string
	if s.hud != nil && body > 1 {
		if summary = s.hud(width, body-1); len(summary) > 0 {
			body -= len(summary)
		}
	}
	if wantPlace {
		rows = append(rows, s.place.Render(width))
	}
	rows = append(rows, summary...)
	drafted := strings.Split(s.renderDraft(width, body), "\n")
	if len(drafted) == 1 && drafted[0] == "" {
		drafted = nil
	}
	for i := 0; i < body; i++ {
		if i < len(drafted) {
			rows = append(rows, drafted[i])
			continue
		}
		// The draft is shorter than its room. The blank rows are emitted rather
		// than skipped so the meta strip stays welded to the region's bottom
		// edge, one row above the footer, instead of drifting up under a short
		// draft and back down under a long one.
		rows = append(rows, "")
	}
	if wantMeta {
		rows = append(rows, s.meta.render(width))
	}
	return strings.Join(rows, "\n")
}

// -- what the composer is bound to (5.15) ------------------------------------

// steerPlaceholder is the steer line's idle hint (5.11: "steer — one-way").
const steerPlaceholder = "steer — one-way"

// chatPlaceholder mirrors internal/tui2/composer's own idle hint. It is
// duplicated rather than imported because that package deliberately does not
// make it configurable — see its comment — so this is the one string this file
// has to know to be able to replace it.
//
// REQUESTED SEAM: composer.Options should carry Prompt and Placeholder. A room
// whose composer means something different needs to SAY so, and the two glyphs
// 5.11 spends on exactly that distinction currently have to be swapped from
// outside.
const chatPlaceholder = "Type a message"

// bind reports what this region is bound to, defaulting to an ordinary chat so
// a stack built without an app behind it is still a composer.
func (s *composerStack) bind() composerBind {
	if s.mode == nil {
		return composerBind{mode: rail.ComposerChat}
	}
	return s.mode()
}

func (s *composerStack) disabled() bool { return s.bind().mode == rail.ComposerDisabled }

// renderDraft draws the draft in whatever mode the selected row bound.
//
// A DISABLED composer is not a greyed-out text field: it is a sentence saying
// why, drawn where the draft would have been (5.20 rule 3). Nothing takes the
// keyboard, no caret is shown, and no prompt glyph promises a send.
//
// A STEER composer is the ordinary draft with its two visible words changed —
// the prompt glyph and the idle hint — because everything else about typing one
// line of text is the same and forking the editor would fork its bugs. The
// glyph swap is width-preserving by construction: both prompts are single-cell
// glyphs from the same table (5.17), so the row's fit is unchanged.
func (s *composerStack) renderDraft(width, body int) string {
	if body <= 0 || width <= 0 {
		return ""
	}
	bind := s.bind()
	if bind.mode == rail.ComposerDisabled {
		note := bind.note
		if note == "" {
			note = "this work is settled — ask aforge about it"
		}
		line := blocks.Truncate(tokens.GlyphMissing+" "+note, width)
		if s.draft != nil {
			line = paintDim(s.styler(), line)
		}
		rows := make([]string, body)
		rows[0] = line
		return strings.Join(rows, "\n")
	}
	drawn := s.draft.Render(width, body)
	if bind.mode != rail.ComposerSteer {
		return drawn
	}
	head, rest, hasRest := strings.Cut(drawn, "\n")
	head = strings.Replace(head, tokens.GlyphPromptChat, tokens.GlyphPromptSteer, 1)
	if strings.TrimSpace(s.draft.Value()) == "" && strings.Contains(head, chatPlaceholder) {
		// The hint only changes when the replacement still fits. A row that was
		// already cut to width keeps the words it has rather than growing past
		// the edge to say something friendlier.
		if grown := strings.Replace(head, chatPlaceholder, steerPlaceholder, 1); blocks.Width(grown) <= width {
			head = grown
		}
	}
	if !hasRest {
		return head
	}
	return head + "\n" + rest
}

// styler is the region's painter, at the focus the draft is drawn with.
func (s *composerStack) styler() *tokens.Styler {
	if s.meta == nil {
		return nil
	}
	return s.meta.style
}

// paintDim draws chrome-tier text, or plain text when there is no profile.
func paintDim(style *tokens.Styler, text string) string {
	if style == nil {
		return text
	}
	return style.PaintToken(text, tokens.TextTertiary)
}

// -- the meta strip ----------------------------------------------------------

// metaIndent aligns the strip with the draft's text rather than with its
// prompt glyph, so the numbers sit under the words they are about.
const metaIndent = "  "

// metaStrip is the composer's own row of numbers: 10.5.23's cost-and-context
// home, held apart from the footer's health-and-questions home so the two
// never mix.
//
// It shortens rather than wraps, on the same mechanic the footer uses
// ([tokens.FitFooter]) and with money holding the highest priority — 5.9 is
// blunt about why: money "is the one number the user never forgives us for
// hiding", so it is the last cell to leave a narrowing terminal.
//
// The v2 engine seam (engine.go's Commander) carries no per-turn cost and no
// context window yet, so both cells render as the missing-data glyph. That is
// 8.2.20's law applied literally — "missing data renders —, never an estimate" —
// and it is the honest shape: a zero would be a claim that this turn was free.
type metaStrip struct {
	style *tokens.Styler

	// model is the word the answering model goes by.
	model string
	// live says a turn is being answered right now, which is the only time an
	// elapsed cell means anything.
	live bool
	// elapsed is how long the live turn has been running. It ages, which is
	// allowed here and nowhere in the committed transcript: 8.1.2's corollary
	// puts an ageing cell in live regions only, and this strip is one.
	elapsed time.Duration

	// cost and context are the two cells this seam cannot fill yet. The fields
	// exist so the day the engine journals them is a wiring change and not a
	// layout change.
	cost      float64
	haveCost  bool
	used      int64
	window    int64
	haveUsage bool

	// hover says the pointer is resting on the model chip, which brightens it
	// one tier and changes nothing else (5.22: a control that is also telemetry
	// is dim at rest and secondary on focus).
	hover bool
}

// chip is the strip's model cell (5.10, 5.23): the one surface model economics
// is said on, rendered by the component that owns its grammar.
//
// It carries the role word because this cell has no column beside it naming
// one — the chip IS the sentence "the voice runs on this" — and it carries no
// gauge because the strip's own ctx cell is 10.5.23's context home and a second
// gauge two cells away would be the same fact drawn twice. What the chip adds
// over the hand-rolled word this replaced is the effort suffix, which 5.10 says
// rides the chip and which the bare word had nowhere to put.
//
// Boosted stays false: the escalation is real (8.2.16) but the engine seam this
// surface holds has no way to ask whether it is on, and a chip that guessed
// would be claiming a binding that is not there.
func (m *metaStrip) chip() modelui.Chip {
	return modelui.Chip{
		Role:   store.RoleOrchestrate,
		Model:  strings.TrimSpace(m.model),
		Styler: m.style,
	}
}

// metaCell is one column of the strip.
type metaCell struct {
	id    string
	text  string
	token tokens.Token
	// paint replaces the token when a cell owns its own colours. The chip is
	// the only one: it is a run of differently-tiered spans (the model word
	// one tier above the effort suffix beside it), and flattening it to a
	// single token here would be this strip re-deciding what a chip looks
	// like — the one thing modelui's doc says a consumer may not do.
	paint    func(width int) string
	priority int
}

// metaChipID names the model cell, which is the one cell on this strip that is
// also a door (5.22 rule 5: "click model chip → model palette").
const metaChipID = "model"

// cells is what the strip would draw, before fitting. It is shared by the paint
// and by the pointer for the same reason the footer's fit is: two computations
// of one row is a click landing on a cell the paint dropped.
func (m *metaStrip) cells() []metaCell {
	cells := make([]metaCell, 0, 4)
	if chip := m.chip(); chip.Width() > 0 {
		cells = append(cells, metaCell{id: metaChipID, text: chip.Text(),
			paint: chip.Render, priority: 60})
	}
	if m.live {
		cells = append(cells, metaCell{id: "elapsed", text: tokens.Elapsed(m.elapsed),
			token: tokens.TextTertiary, priority: 70})
	}
	if m.haveCost {
		cells = append(cells, metaCell{id: "cost", text: tokens.Money(m.cost),
			token: tokens.Green, priority: 100})
	} else {
		cells = append(cells, metaCell{id: "cost", text: "$" + tokens.GlyphMissing,
			token: tokens.TextTertiary, priority: 100})
	}
	if m.haveUsage {
		cells = append(cells, metaCell{id: "ctx", text: tokens.Gauge(fraction(m.used, m.window)) + " " +
			tokens.Context(m.used, m.window), token: tokens.ContextToken(m.used, m.window),
			priority: 80})
	} else {
		cells = append(cells, metaCell{id: "ctx", text: tokens.GlyphMissing + " ctx",
			token: tokens.TextTertiary, priority: 80})
	}
	return cells
}

// metaSep is the strip's separator and its width, named once so the paint and
// the pointer step by the same amount.
const metaSep = " " + tokens.GlyphSeparator + " "

// fit is which cells survive at width, in display order.
func (m *metaStrip) fit(width int) []metaCell {
	cells := m.cells()
	sepWidth := blocks.Width(metaSep)
	columns := make([]tokens.FooterColumn, len(cells))
	for i, c := range cells {
		w := blocks.Width(c.text)
		if i > 0 {
			w += sepWidth
		}
		columns[i] = tokens.FooterColumn{ID: c.id, MinWidth: w, Priority: c.priority}
	}
	kept := tokens.FitFooter(columns, width-len(metaIndent))
	keep := make(map[string]bool, len(kept))
	for _, c := range kept {
		keep[c.ID] = true
	}
	out := make([]metaCell, 0, len(cells))
	for _, c := range cells {
		if keep[c.id] {
			out = append(out, c)
		}
	}
	return out
}

// chipSpan is the model chip's column range on the strip, or ok=false when the
// fit dropped it. The indent is included, so the answer is in the same
// coordinates a pane-local pointer arrives in.
func (m *metaStrip) chipSpan(width int) (from, to int, ok bool) {
	if m == nil || width <= 0 {
		return 0, 0, false
	}
	x := blocks.Width(metaIndent)
	for i, c := range m.fit(width) {
		if i > 0 {
			x += blocks.Width(metaSep)
		}
		w := blocks.Width(c.text)
		if c.id == metaChipID {
			return x, x + w, true
		}
		x += w
	}
	return 0, 0, false
}

// render draws the strip at width.
func (m *metaStrip) render(width int) string {
	if m == nil || width <= 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(metaIndent)
	written := 0
	for _, c := range m.fit(width) {
		if written > 0 {
			out.WriteString(m.paint(metaSep, tokens.TextTertiary))
		}
		if c.paint != nil {
			if c.id == metaChipID && m.hover {
				// The chip brightens whole rather than span by span: it is one
				// control, and lighting half of it would say the model word and
				// the effort suffix are two different targets.
				out.WriteString(m.paint(c.text, tokens.TextPrimary))
			} else {
				out.WriteString(c.paint(blocks.Width(c.text)))
			}
		} else {
			out.WriteString(m.paint(c.text, c.token))
		}
		written++
	}
	if written == 0 {
		return ""
	}
	return out.String()
}

func (m *metaStrip) paint(text string, token tokens.Token) string {
	if m.style == nil || text == "" {
		return text
	}
	return m.style.PaintToken(text, token)
}

// fraction is used/window, clamped, for the one-cell gauge.
func fraction(used, window int64) float64 {
	if window <= 0 || used <= 0 {
		return 0
	}
	return float64(used) / float64(window)
}
