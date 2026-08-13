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
	"github.com/Agent-Field/aforge-v2/internal/tui2/keychip"
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
	// record resolves a click inside an entered task's record — the tree's
	// branch folds and leaf drills (recordtree.go). Same seam shape as fold:
	// the pane hands over the cell, the act is the app's.
	record func(y int) tea.Cmd
	// focus asks for the keyboard, because pointing IS looking (5.14). The
	// transcript's own keys are the scroll vocabulary, which the app's ladder
	// routes here whatever holds custody; what a click on the conversation
	// actually settles is that the MAP no longer does, so the next letter is a
	// letter and the next j is a j. Same seam as answer and fold: the pane knows
	// it was pointed at, the app knows where the keyboard is.
	focus func() bool
	// run performs one of the empty state's example rows, and is the same
	// [App.runFooterVerb] the footer's own words reach — a fourth hand on one
	// executor, never a fourth door. Same seam as answer and fold.
	run func(entryID string) tea.Cmd
	// copy takes one message's words out to the clipboard. Same seam again: the
	// pane resolves a cell to the chip on a row, and the ACT — the OSC 52 write
	// and the frame of proof after it — belongs to the app (copychip.go).
	copy func(block *messageBlock) tea.Cmd
	// style paints the empty state. The transcript's blocks carry their own
	// styler; this pane needs one only for the rows it draws itself.
	style *tokens.Styler
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
	// hoverTeach is the empty state's example row the pointer rests on,
	// one-based, or zero for none. A third field for a third kind of row, on the
	// same reasoning as hoverFold: these rows exist only while the other two
	// cannot, but one shared field would still be one field clearing the wrong
	// mark the first time a lens went from empty to spoken-in.
	hoverTeach int
	// hoverCopy is the block the pointer rests ANYWHERE on, one-based, or zero
	// for none — which is what surfaces that block's copy chip. It is a fourth
	// field for the same reason the third exists: it marks a different thing on a
	// different scale (a whole message, not one row), and a shared field would
	// clear the wrong mark the first time a pointer crossed from a fold row into
	// the prose under it.
	hoverCopy int
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
	if p.transcript.Len() == 0 {
		return p.renderTeaching(width, height)
	}
	return strings.Join(p.transcript.Frame(p.now()).Rows, "\n")
}

// -- the taught empty state (5.22 rule 6) ------------------------------------

// A room nobody has spoken in yet is the one frame that has no content to fall
// back on, and it used to be drawn as nothing at all: an entirely blank lens,
// which teaches a first-time reader that the surface is broken or that they are
// in the wrong place. 5.22 rule 6 is explicit — "a new home shows three
// clickable example actions instead of a blank transcript" — and the entered
// task room next door has said a sentence about its own emptiness since 12.14.
//
// The rows are the registry's, never this file's: the same ids the palette, the
// `?` sheet and the `/` line draw, run through the same executor a click on a
// footer verb reaches. So an accelerator taught here cannot drift from the one
// that works, and a door that stops existing stops being taught on the same
// commit.
//
// It is drawn by the PANE rather than appended to the transcript as a block,
// which is what keeps it out of the record: an empty room has no turns, and a
// teaching block in the block list would be a row the anchor, the fold state
// and the copy verbs all had to learn to ignore. The moment a real block lands
// the teaching is gone, with nothing to clear.

// teachIDs are the three doors offered, in the order they are drawn: what this
// room can do, where its work shows up, and who is doing it. All three are
// unconditional in [App.runEntry] — a taught door that answered with a reason
// (5.20 rule 3) would be teaching the reader a dead end on their first frame.
var teachIDs = [...]string{helpEntryID, "slash.graph", "slash.self"}

// teachLead is the sentence above the three rows. It names the composer first,
// because typing a sentence is what this product is FOR and the examples are
// the second thing to know, not the first.
const teachLead = "nothing here yet — say what you want done, or start with one of these"

// teachSpan is one painted run of an empty-state row: plain text and the tier
// it is drawn at. The rows are assembled plain and painted afterwards, which is
// the same ordering every fitted surface in this tree uses — measuring a
// painted string is measuring its escape sequences.
type teachSpan struct {
	text string
	tier tokens.Token
}

// teachRow is one drawn row: its spans, and the registry row it performs. An
// empty id is prose, which performs nothing and is not a target — the lead
// sentence is a statement about the room, not a verb on it (the same line
// footer/hit.go draws between its own words).
type teachRow struct {
	id    string
	spans []teachSpan
}

// teachRows is the empty state at this width, derived on demand and never
// recorded during a render (Part 2's anti-pattern 14). The paint and the
// pointer both call it, so a click cannot land on a word the paint dropped.
func (p *transcriptPane) teachRows(width int) []teachRow {
	if width <= tokens.LensIndent {
		return nil
	}
	// The lead wraps at the readable measure rather than at the lens: chrome
	// prose running the full width of a 200-column terminal is a line the eye
	// loses its place in (tokens.ProseMeasure).
	measure := width
	if measure > tokens.ProseMeasure {
		measure = tokens.ProseMeasure
	}
	pad := strings.Repeat(" ", tokens.LensIndent)
	rows := make([]teachRow, 0, 6)
	lines, _ := blocks.Wrap(nil, teachLead, measure-tokens.LensIndent)
	for _, line := range lines {
		rows = append(rows, teachRow{spans: []teachSpan{{pad + line, tokens.TextTertiary}}})
	}
	rows = append(rows, teachRow{})

	entries := make([]registry.Entry, 0, len(teachIDs))
	keys := make([]string, 0, len(teachIDs))
	for _, id := range teachIDs {
		entry, found := registry.ByID(id)
		if !found {
			continue
		}
		key := teachKey(entry)
		if key == "" {
			continue
		}
		entries = append(entries, entry)
		keys = append(keys, key)
	}
	// The descriptions are one COLUMN and they leave as one, the way the
	// footer's verbs are fitted as a unit: dropping only the rows whose sentence
	// happened not to fit would teach three doors in two different formats and
	// leave the reader deciding what the difference meant.
	described := true
	for i, entry := range entries {
		if blocks.Width(pad)+keychip.Width([]registry.Chip{registry.ChipFor(entry.Verb, keys[i])})+
			blocks.Width(teachNote(entry)) > width {
			described = false
			break
		}
	}
	for i, entry := range entries {
		rows = append(rows, teachExample(entry, keys[i], pad, width, described))
	}
	return rows
}

// teachKey is the accelerator this row is taught by, in the words that actually
// work from here.
//
// A bare letter is not one of them: the composer holds every printable key on
// this surface, so [registry.Entry.KeyOn] answers "" for `?` and the slash alias
// is what the reader can really type. It is footer.verbLabel's own fallback
// ladder, and the two agree because they are answering one question — what do I
// tell the user to press — from one catalog.
func teachKey(entry registry.Entry) string {
	if key := entry.KeyOn(registry.SurfaceComposerFirst); key != "" {
		return key
	}
	if entry.Slash != "" {
		return "/" + entry.Slash
	}
	return ""
}

// The gap between the two halves of a chip is [registry.ChipGap] now, spelled
// once beside the ORDER it separates rather than here — the aligned key column
// this constant used to pad went with the key-first layout (see [teachExample]).

// teachNote is the registry's own one-line description, behind the telemetry
// separator (5.17).
func teachNote(entry registry.Entry) string {
	return " " + tokens.GlyphSeparator + " " + entry.Description
}

// teachExample lays one example row out: the verb·key chip, and — when the
// whole column fits — the registry's own description behind it.
//
// IT USED TO LEAD WITH THE KEY, in an aligned column of its own, and that was
// §16's `esc close` bug wearing a layout: the first word on every row was the
// thing to press rather than the thing it does, so a reader parsed each row by
// already knowing the answer. The chip is [keychip.Of] now — verb first at the
// brighter tier, key after it one tier down — which is the same two-tier rule
// the footer's verbs, the consent strip and the composer's chips wear.
//
// THE ALIGNED KEY COLUMN WENT WITH IT, and it is not missed: a column is worth
// its cells when a reader SCANS down it (§20's receipt column), and nobody scans
// three rows for a keystroke they are being taught. What the column actually did
// was hold the verbs — the words the row is about — at a ragged left edge that
// moved with the longest accelerator in the set.
//
// What does not fit leaves from the right, description first, and the verb is
// only ever cut — a row that named no verb would name no door.
func teachExample(entry registry.Entry, key string, pad string, width int, described bool) teachRow {
	room := width - blocks.Width(pad)
	spans := []teachSpan{{pad, tokens.TextTertiary}}
	for _, span := range keychip.Of(registry.ChipFor(entry.Verb, key), tokens.TextSecondary) {
		spans = append(spans, teachSpan{blocks.Truncate(span.Text, room), tokens.Token(span.Tok)})
		room -= blocks.Width(span.Text)
		if room < 1 {
			break
		}
	}
	if described {
		spans = append(spans, teachSpan{teachNote(entry), tokens.TextTertiary})
	}
	return teachRow{id: entry.ID, spans: spans}
}

// renderTeaching paints the empty state into the rectangle, dropping rows off
// the BOTTOM when the lens is too short — the lead sentence says what the room
// is before the examples say what to do in it, so the examples are what a short
// window can afford to lose.
func (p *transcriptPane) renderTeaching(width, height int) string {
	rows := p.teachRows(width)
	if len(rows) > height {
		rows = rows[:height]
	}
	lines := make([]string, 0, len(rows))
	for i, row := range rows {
		var line strings.Builder
		for _, span := range row.spans {
			tier := span.tier
			// 13.14's hover law: one tier brighter under the pointer, never a
			// band. The whole row rises together because the whole row is one
			// target — lighting the verb alone would say the description was a
			// different door.
			if row.id != "" && p.hoverTeach == i+1 {
				tier = tokens.Promote(tier)
			}
			line.WriteString(p.paint(span.text, tier))
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

// paint draws one span, or returns it unchanged for a pane built without a
// profile (the golden harness and every headless test).
func (p *transcriptPane) paint(text string, tier tokens.Token) string {
	if p.style == nil || text == "" {
		return text
	}
	return p.style.PaintToken(text, tier)
}

// teachAt resolves a pane-local row to the example on it, if it is one. It is
// [optionAt] for the one lens that has no blocks to ask.
func (p *transcriptPane) teachAt(y int) (string, bool) {
	if p.homes != nil || p.transcript == nil || p.transcript.Len() != 0 {
		return "", false
	}
	rows := p.teachRows(p.width)
	if y < 0 || y >= len(rows) || rows[y].id == "" {
		return "", false
	}
	return rows[y].id, true
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
	if !ok || !block.isFoldRow(line, p.width) {
		return nil, 0, false
	}
	return block, index, true
}

// messageAt resolves a pane-local row to the message block drawn on it, and to
// which of that block's own lines it is.
func (p *transcriptPane) messageAt(y int) (*messageBlock, int, int, bool) {
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
	return block, index, line, true
}

// copyAt resolves a pane-local cell to the copy chip it landed on.
//
// This is the one hit test in the transcript that consults x, and the reason is
// the chip's own: an option row IS the choice and a fold row IS the disclosure,
// so the whole row stands for the thing it does. The chip shares its row with
// the first line of a message, and a click on those words is a click on the
// conversation and must stay one.
func (p *transcriptPane) copyAt(local image.Point) (*messageBlock, bool) {
	block, _, line, ok := p.messageAt(local.Y)
	if !ok || !block.chipAt(local.X, line, p.width) {
		return nil, false
	}
	return block, true
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
		// An example row on the taught empty state performs its registry row
		// (5.22 rule 6's "clickable"). It is asked first only because it is the
		// one target that exists when no block does — the three tests below are
		// mutually exclusive by construction, not by order.
		if p.run != nil {
			if id, ok := p.teachAt(local.Y); ok {
				return p.run(id)
			}
		}
		// The copy chip is asked before the row-wide targets because it is the
		// one target NARROWER than its row: it lives in the right edge of a line
		// that is otherwise prose, or the header of a card, and the row-wide
		// tests would swallow it. It exists only while the pointer is on this
		// message, so there is nothing to swallow the rest of the time.
		if p.copy != nil {
			if block, ok := p.copyAt(local); ok {
				return p.copy(block)
			}
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
		if p.record != nil {
			if cmd := p.record(local.Y); cmd != nil {
				return cmd
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
	// Every door is asked, never short-circuited: they mark different rows and
	// a skipped one is a mark left lit on a row the pointer has left.
	moved := p.hoverFoldRow(local, inside)
	moved = p.hoverTeachRow(local, inside) || moved
	moved = p.hoverMessage(local, inside) || moved

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

// hoverMessage surfaces the copy chip on the message the pointer is resting on.
//
// THE TARGET IS THE WHOLE MESSAGE AND THE CHIP IS ON ITS FIRST ROW. Resting
// anywhere in a reply — its tenth line, its artifact row — offers the door,
// because what a reader wants to take out is the ANSWER and not the line their
// mouse happened to stop on. Drawing it on the first row is what keeps it a
// layer rather than a decoration: one chip per message, in one column, gone the
// instant the pointer leaves.
//
// It touches paint and nothing else — one int here, one flag on the block — so
// the hover door stays the side-effect-free door its type promises (13.14).
func (p *transcriptPane) hoverMessage(local image.Point, inside bool) bool {
	index := 0
	if inside && p.copy != nil {
		if _, at, _, ok := p.messageAt(local.Y); ok {
			index = at + 1
		}
	}
	if p.hoverCopy == index {
		return false
	}
	if p.hoverCopy != 0 && p.transcript != nil && p.hoverCopy-1 < p.transcript.Len() {
		if old, isMessage := p.transcript.Block(p.hoverCopy - 1).(*messageBlock); isMessage {
			old.SetCopyHover(false)
			// The proof goes with the chip it replaced. A `copied` left lit on a
			// message the pointer has left would be feedback about a row nobody
			// is looking at any more.
			old.SetCopied(false)
		}
	}
	p.hoverCopy = index
	if index != 0 {
		if block, isMessage := p.transcript.Block(index - 1).(*messageBlock); isMessage {
			block.SetCopyHover(true)
		}
	}
	return true
}

// hoverTeachRow lights the example row under the pointer.
//
// It is [hoverFoldRow] for the empty state, and it is even cheaper: the rows
// are the pane's own paint rather than a block's, so the mark is one int here
// and nothing is invalidated anywhere else. Nothing it touches is readable by a
// keystroke, which is what keeps the hover door side-effect free.
func (p *transcriptPane) hoverTeachRow(local image.Point, inside bool) bool {
	row := 0
	if inside {
		if _, ok := p.teachAt(local.Y); ok {
			row = local.Y + 1
		}
	}
	if p.hoverTeach == row {
		return false
	}
	p.hoverTeach = row
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
	// THE BREATHE, and not the spinner. §18.2 sanctions exactly one moving
	// glyph on this line and §7 moved the SPINNER to the composer's prompt, one
	// row down: two braille wheels turning at once, three rows apart, is two
	// answers to "is anything happening" and the reader has to decide whether
	// they mean different things.
	//
	// So the thinking line takes §11's OTHER motion — the eased three-tier dot
	// that grows and shrinks on a 1.44s breath ([blocks.DefaultPulse]) — which
	// says the same thing in a different register: the prompt says a reply is
	// being written, this says the room is thinking about it. The two are
	// phase-independent by construction and cannot beat against each other,
	// because the periods (1.2s and 1.44s) were chosen not to.
	//
	// It USED TO DRAW NOTHING AT ALL. Every construction site passed
	// motion:false, so the line has shipped a static [tokens.GlyphWorking] since
	// the spinner moved — §11's second motion existed in the token table, in the
	// clock, and in no frame anybody ever saw.
	glyph := tokens.GlyphWorking
	if b.motion && b.clock != nil {
		glyph = blocks.DefaultPulse.Glyph(b.clock)
	}
	tail := " " + b.phase
	if b.interruptible {
		// VERB FIRST (§16's verb·key chip, [keychip]). It read `esc interrupt`
		// until this line, which is the bug the law was written about spelled out
		// on the one row a reader looks at while they are deciding whether to
		// stop the machine: two dim words, and nothing saying which is the label
		// and which is the key.
		hint := keychip.Sep + keychip.Text(registry.ChipFor("interrupt", "esc"))
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

// -- the seam under the fixed strip (§16 SURFACE SEAMS ARE GROUNDS) ----------
//
// THE DEFECT. Reported in four words — "no border in chat when scrolling". The
// transcript scrolls; the composer region and the contextual line do not; and
// between the moving thing and the still thing there was nothing at all. A line
// of a reply arriving at the bottom of the lens simply stopped existing one row
// later, so the eye had no way to tell whether the surface ended there, whether
// the text had been cut, or whether the two regions were one region behaving
// strangely. It is 12.13's wall finding on the horizontal axis — there two panes
// were flush and read as one room with a glitch; here two PLANES are.
//
// WHAT IT IS NOT. A hairline. §16's RULED LINES admits exactly two: the dialog
// chrome ring (internal/tui2/dialogchrome) and the word-in-line seam, where the
// label IS the rule. A third rule drawn across the bottom of the transcript is
// the school-notebook smell the rule exists to name, and 5.13 already spent this
// surface's structure budget on "whitespace not boxes". So the seam is a GROUND:
// the fixed strip carries its own floor, and what scrolls visibly slides beneath
// it. A plane needs no line to have an edge — the edge is where the floor
// changes.
//
// WHICH RUNGS — AND THERE ARE TWO OF THEM NOW. §7's hug is not one strip, it is
// two rows on a TWO-TONE ground: the row you type into stands one shade lighter
// than the bar row that names where you are, and that step is the whole of the
// depth cue. A single ground under both would be one slab two rows tall, which
// is what this surface shipped before and what the reader called a black bar.
//
// The rungs are [tokens.HugGroundInput] and [tokens.HugGroundBar], derived on
// the ground→band axis and both deliberately BELOW [tokens.Sheet]: a dialog
// arrived and will leave, so it may announce itself; the hug has been on screen
// since the window opened. See tokens/palette.go's [tokens.HugBarTowardBand] for
// why the two numbers are the only pair that survives the xterm greyscale ramp,
// and tokens' TestHugLadder for the measurements.
//
// HONEST DEGRADATION. Below [tokens.Profile.SheetGround] the strip draws
// NOTHING. There is no fallback rule line, and refusing one is the point: a
// hairline invented where the ground could not be painted would reintroduce
// exactly the mark §16 forbids, on precisely the profiles least able to afford
// an extra idiom. At 16 colours and at none the hug's edge glyph at column 0 is
// the whole seam, which is the same answer [tokens.Profile.SelectionStyle] and
// the dialog's own ring give: the ground is the enhancement, the shape is the
// floor. At 256 colours the bar rung resolves to the ground's own greyscale
// entry, so the two-tone reads as one raised INPUT row over an unpainted bar —
// documented as intended rather than worked around, because the alternative is
// a bar rung that collides with the input rung and no depth at all.

// seamStrip paints one region of the fixed bottom strip onto the seam's ground.
//
// It takes a COMPOSED view rather than spans, and that is the one compromise in
// here worth naming. [blocks.CardBlock]'s ground states the rule — "a background
// wrapped around already-painted text is undone by the first inner reset the
// painted spans carry, so the only form that survives is foreground and
// background written together, span by span" — and it is right. But this strip
// is composed by sibling packages (internal/tui2/composer and footer), each of
// which paints its own spans through the token layer's ordinary doors, and
// threading a ground through both would be two renderers rewritten to carry a
// parameter that means nothing to either. So the ground is asserted at the head
// of the row and RE-asserted after every reset that can clear it
// ([tokens.GroundResets]) — the same guarantee, bought with a scan of a handful
// of rows per frame instead of an API.
//
// The GROUND IS A PARAMETER because the hug is two-tone: the input row and the
// bar row call this with different rungs, and a package-level constant would be
// the two rows sharing one floor again (see the WHICH RUNGS note above).
//
// Every row is filled to the full width, because a plane that stops at the last
// letter is a highlight, and the rectangle is filled to its full height, because
// a strip that returned fewer rows than it was given would show the transcript
// through the hole (12.13's ghost, one plane up — the same reasoning
// palette.padSheet states for a floating panel).
func seamStrip(style *tokens.Styler, view string, width, height int, rung tokens.Token) string {
	if width <= 0 || height <= 0 || style == nil || !style.Profile().SheetGround() {
		return view
	}
	profile, focus := style.Profile(), style.Focus()
	ground := rung.Bg(profile, focus)
	if ground == "" {
		return view
	}
	var out strings.Builder
	out.Grow(len(view) + height*(width+len(ground)+8))
	drawn := 0
	for row := range strings.SplitSeq(view, "\n") {
		if drawn > 0 {
			out.WriteByte('\n')
		}
		seamRow(&out, row, ground, profile, width)
		drawn++
	}
	for ; drawn < height; drawn++ {
		if drawn > 0 {
			out.WriteByte('\n')
		}
		seamRow(&out, "", ground, profile, width)
	}
	return out.String()
}

// seamRow writes one row of the strip: the ground, the row with its floor kept
// under it, the fill out to the edge, and the ground given back.
//
// It never truncates. The renderers above it fit their own rows to the width
// they were given, and cutting a painted string here is how a row ends up
// wearing half an escape sequence — the same rule the footer states for its own
// error line.
func seamRow(out *strings.Builder, row, ground string, profile tokens.Profile, width int) {
	out.WriteString(ground)
	out.WriteString(seamReground(row, ground, profile))
	if pad := width - blocks.Width(row); pad > 0 {
		out.WriteString(strings.Repeat(" ", pad))
	}
	out.WriteString(tokens.ResetBg(profile))
}

// seamReground puts the floor back after every span that took it away. The
// containment check is what keeps the ordinary row free: nothing in the footer's
// ordinary vocabulary resets a background, so the common case is two scans and
// no allocation.
//
// The list of resets is [tokens.GroundResets] and is no longer restated here.
// It was, with a test pinning the restatement against what a styler emits — and
// a test that pins a copy is a copy with a guard on it, not one spelling. The
// fact belongs to the package that writes the bytes.
func seamReground(row, ground string, profile tokens.Profile) string {
	for _, reset := range tokens.GroundResets(profile) {
		if strings.Contains(row, reset) {
			row = strings.ReplaceAll(row, reset, reset+ground)
		}
	}
	return row
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
// 10.5.23's split — health here, cost and context on a strip of the composer's
// own — IS OVER, and this type is where it ended. §7 dissolved the meta strip
// into this row: the model word came here as the picker's door, the context
// gauge came here as a standing fact, and the turn's own cost and elapsed came
// here as middle-zone chips that exist only while the turn does. The reason is
// the one §15 gives for everything else: two permanent rows of numbers under a
// conversation is structure announcing itself, and the split was only ever
// keeping two rows apart that should have been one.
type statusPane struct {
	style *tokens.Styler
	bar   *footer.Model

	// Filled once, at construction and then by a model switch or a promotion.
	session string
	// model is the slug this window's voice is bound to. IT IS NEVER DRAWN —
	// §14 puts identifiers in the never-shown tier, and a reader met this one on
	// the live build as `deepseek-v4-flash-latest`. It is kept because the
	// window has to KNOW which model answers (a promotion adopts an engine and
	// this is where the adoption lands), and dropping the field would be losing
	// the fact rather than declining to print it.
	model string

	// Filled by the poll and the composer's turn; poll.go writes turns and err.
	turns int
	live  bool
	err   string

	// The three zones' inputs (§7): places for the left, and the standing facts
	// for the right — where the work lands, who is answering, how much of the
	// window is gone, what the day has cost. Filled by refresh and the poll;
	// every absent one renders as nothing at all (§16's EMPTINESS).
	places    []footer.Place
	doors     []footer.Door
	spend     float64
	haveSpend bool
	// dir is the abbreviated ground, rendered by internal/tui2/placeline and
	// handed over as words. The place line is no longer a ROW of this surface —
	// §7 folded it into the bar's right zone — but it is still the component
	// that knows how to shorten a path, so the app keeps a model and asks it.
	dir string
	// used, window and haveUsage are the context gauge's reading. They arrive
	// from the same poll that used to write them onto the meta strip; what
	// changed is where they land.
	used      int64
	window    int64
	haveUsage bool
	// cost and haveCost are THIS TURN's money, drawn in the middle zone beside
	// the interrupt chip and only while a turn is live. elapsed is its age.
	// None of the three is a standing fact, which is exactly why none of them
	// is on the right.
	cost     float64
	haveCost bool
	elapsed  time.Duration

	// interrupt is the act the middle zone's `interrupt esc` chip performs —
	// the same act esc takes on a live turn, reached by a pointer.
	interrupt func() tea.Cmd

	// dock is §6's hidden sidebar, collapsed onto this row (footer/dock.go). It
	// is filled by refresh: whether the drawer is shut, and how much live work
	// is inside it. openRail is the one act it performs, and it is the very act
	// the rail chord performs — a click and a chord that opened the drawer by
	// two different routes would be two drawers.
	dock     footer.Dock
	openRail func() tea.Cmd

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
	// thread is the TITLE CHIP: the scribe's name for the working conversation
	// this window is in (chat-simplify.md 5.3), and breadcrumb is how deep
	// inside it the reader has navigated. Neither is ever an id (13.3.4); an
	// unnamed thread draws NOTHING, which is [App.threadName]'s empty string
	// arriving here unchanged.
	//
	// It replaced a `room` field that fed the breadcrumb's head. The two were
	// the same fact and the chip is the better home for it: the trail is the way
	// UP, and a thread has nothing above it — it is the conversation, not a
	// scope inside one — so the name it used to lead with was a step nobody
	// could take. See [statusPane.scopeTail].
	thread     string
	breadcrumb string
	// openThreads raises the switcher, and it is the very act the `t` key
	// performs. It is a function for the reason [statusPane.openRail] is: the
	// row knows which word was pointed at and nothing else, and a chip and a key
	// that opened the list by two different routes would be two lists.
	openThreads func() tea.Cmd
	// newThread mints a conversation and walks into it — the `+` door, and the
	// same act the switcher's last row performs. It is a function for the same
	// reason [statusPane.openThreads] is.
	newThread func() tea.Cmd
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

// Render composes the bar row at width, on the DARKER of the hug's two rungs.
//
// The two rows of the hug are one surface and two planes: the transcript scrolls
// past both and through neither, and the step between them says which one you
// type into without a hairline being drawn to say it. This is the row that names
// where you are, so it is the one further back. See the WHICH RUNGS note at
// [seamStrip].
func (p *statusPane) Render(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	p.lastWidth = width
	return seamStrip(p.style, p.row(width), width, height, tokens.HugGroundBar)
}

// row is what the contextual line says right now, unpainted by the seam. It is
// split from Render so the ground is applied at exactly one place and neither
// of the two rows the footer can be — the ordinary registry of columns, and the
// whole coloured sentence a failed read replaces it with — can be given a floor
// the other one lacks.
func (p *statusPane) row(width int) string {
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
		Places:        p.places,
		Doors:         p.doors,
		Spend:         p.spend,
		HaveSpend:     p.haveSpend,
		Verbs:         p.offeredVerbs(width),
		Input:         p.input,
		Hint:          p.hint,
		EscInterrupts: p.escInterrupts,
		Attention:     p.attention,
		KeyMode:       p.keyMode,
		KeyModeCount:  p.keyCount,
		Health:        p.health(),
		Dock:          p.dock,
		Thread:        strings.TrimSpace(p.thread),
		ScopeTail:     p.scopeTail(),
		Hover:         p.hover,
		// What the meta strip used to say, said here (§7). The model word is
		// the picker's door; the gauge and the directory are standing facts;
		// the turn's own money and age are live ones and are gated on Live so
		// they leave with the turn rather than standing as a legend.
		Dir:          p.dir,
		CtxUsed:      p.used,
		CtxWindow:    p.window,
		HaveCtx:      p.haveUsage,
		Live:         p.live,
		Elapsed:      p.elapsed,
		TurnCost:     p.cost,
		HaveTurnCost: p.haveCost,
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
	case footer.InterruptTarget:
		// The chip performs what esc performs, and only while it is drawn —
		// the row only offers it while EscInterrupts is true.
		if p.interrupt != nil {
			return p.interrupt()
		}
		return nil
	case footer.ThreadTarget, footer.ThreadsDoorTarget:
		// The title chip is the switcher's door (5.3), and so is the `threads`
		// word beside the tabs. Both go through the app's own
		// [App.openSwitcher] rather than raising anything themselves, so the
		// chip, the word and the chord cannot leave the surface in three
		// different states.
		//
		// TWO DOORS ONTO ONE ACT IS NOT TWO DOORS TOO MANY. The chip is absent
		// until the scribe has named the conversation; the word is always there.
		// A reader in a fresh window has only the word.
		if p.openThreads != nil {
			return p.openThreads()
		}
		return nil
	case footer.NewThreadTarget:
		// The `+`. It performs exactly what the switcher's last row performs,
		// through the same call, so the bar and the list cannot disagree about
		// what minting a thread does.
		if p.newThread != nil {
			return p.newThread()
		}
		return nil
	case footer.DockTarget:
		// The dock is the shut drawer, and clicking it opens the drawer. It
		// goes through the app's own toggle rather than through a shell call of
		// its own, so the pointer and the chord cannot leave the surface in two
		// different states.
		if p.openRail != nil {
			return p.openRail()
		}
		return nil
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
// It says HOW DEEP INSIDE the conversation the reader has gone — the scopes they
// descended through, in the words they navigated by. 13.3.4 is why nothing on it
// can ever fall back to a session id.
//
// IT NO LONGER LEADS WITH THE THREAD'S NAME. It used to, and the name was the
// only part of it that survived the elision ladder on a narrow row — which was
// the tell that it was doing the title chip's job. The chip does that job now,
// permanently and one zone to the left ([FocusContext.Thread]), so repeating it
// here would be §19's same-fact-twice inside one row. What is left is the part
// the chip cannot say: how far in you are, and that every separator on it is a
// step you can take back.
func (p *statusPane) scopeTail() string {
	// Empty at home, on purpose. The trail begins only when the reader has
	// descended; the chip beside it says which conversation they descended
	// INSIDE OF, and it is drawn whether or not they have.
	crumbs := strings.TrimSpace(p.breadcrumb)
	if crumbs == "" {
		return ""
	}
	return tokens.GlyphScopeUp + " " + crumbs
}
