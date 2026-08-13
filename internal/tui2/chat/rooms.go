package chat

import (
	"image"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/keychip"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// Rooms: the rail becomes a map you can walk, and the main pane follows it.
//
// This file is the shell half of 5.15. internal/tui2/rail owns the scope model
// and its three renderings and owns no keymap; this is where a keystroke becomes
// a gesture, a gesture becomes a [rail.Event], and an event becomes the two
// things the reader can see move: what the main pane shows, and what the
// composer will talk to.
//
// The rules it implements, stated once so the code below can be read against
// them rather than explained line by line:
//
//   - ONE CURSOR. There is no highlighted-versus-open state. What the cursor
//     rests on is what the main pane shows.
//   - SELECTION PREVIEWS, ENTER COMMITS (5.15's refinement, for cost). Moving
//     the cursor costs a card — one block, built from the row already in hand,
//     with no store read at all. Enter is what pays for a room: the node trail
//     is read then, once, and polled from then on.
//   - CLICKS GO WHERE THEY POINT (13.18). Select-then-enter is the KEYBOARD's
//     law, and it earns its second key: an arrow is how a keyboard looks around,
//     so the look has to be cheap and the commitment separate. A hand that put
//     the pointer on a card and pressed has already looked, and asking it to
//     press again is asking it to say the same thing twice. So one click on a
//     row opens it. The two rows that PERFORM rather than navigate — the
//     `+ new room` door and 5.24's group lid — keep the two-step, because a
//     stray click may not mint a room.
//   - THE COMPOSER BINDS WHAT WAS ENTERED (13.18), never what the cursor is
//     resting on. A preview draws a card, and a card is not a surface — it is a
//     LOOK at one. Rebinding on a look is what left a disabled composer under a
//     reader who had merely walked past settled work, after which the composer
//     refused their click and the map ate the sentence they typed. What a
//     preview owes the reader is the affordance in WORDS, and the card carries
//     it.
//   - ESC POPS SCOPE, never just selection. At home it hands the key back to
//     8.2.21's ladder, which is the app's existing esc chain.
//   - THE AFFORDANCE NEVER LIES (5.20, 5.15's one rule). A surface that takes no
//     draft says so: an entered room over settled work gets a disabled composer
//     that says why, never a live one that swallows a sentence. The prompt glyph
//     previews the composer the ENTERED surface bound, and a row's mark previews
//     the one entering that row would bind, so neither can promise a draft that
//     will not be taken.

// viewKind is what the main pane is currently a lens onto.
type viewKind uint8

const (
	// viewThread is the room's own conversation — the transcript the poll
	// feeds. It is the only view that survives a room switch, because it is
	// what a room switch switches.
	viewThread viewKind = iota
	// viewNode is a task room: the node-anchored trail plus its receipts, which
	// is what 4.6 says a v1 room IS — a view over the same journal, rendered by
	// the same dressed renderers, filtered to one node.
	viewNode
	// viewCard is the lightweight preview a cursor move produces. It is built
	// from the rail row already in hand and reads nothing.
	viewCard
	// viewHome is one of 5.24's four rooms, drawn by internal/tui2/homes' own
	// View. It is the one lens whose rows are not blocks: a notebook fact and a
	// charter are not turns of conversation, and dressing them as messages would
	// be the transcript claiming they were said.
	viewHome
)

// mainView is the main pane's current lens.
type mainView struct {
	kind viewKind
	// node is the graph node a viewNode is anchored to.
	node string
	// rowID is the rail row a viewCard is a preview OF.
	//
	// A card is the one lens whose subject is not a surface the window is
	// standing in, so it is the one lens that cannot be identified by anything
	// else it holds: the transcript is a block built for the frame and the title
	// is a name a second row could share. The id is what the row was already
	// carrying, kept so a keystroke can ask what the reader is looking at
	// ([App.previewingNewRoom]) instead of comparing drawn words.
	rowID string
	// title is the breadcrumb tail this view contributes.
	title string
	// transcript is the block list this view draws. viewThread's is the app's
	// own and is never replaced; the other two own theirs.
	transcript *blocks.Transcript
	// watermark is how far into the node trail this view has read.
	watermark int64
	// teaching says the transcript holds the empty-room card and nothing else
	// (openTaskRoom). The first journaled row clears it, so the line is on
	// screen only while it is true.
	teaching bool

	// card is the rail row the reader entered from. It is kept because the room
	// is repainted whenever the record moves, and 12.14's rule is that an
	// entered room never knows less about a task than the card above it — so
	// the empty-room block is drawn from the same row every time, not from
	// whatever the rail happens to be selecting later.
	card rail.Row
	// messages is the node-anchored trail this room has read, kept whole.
	//
	// It used to live only as blocks in the transcript, which was enough while
	// the room was append-only. It is not enough now: the record a room draws is
	// the trail INTERLEAVED with the graph's own account of the same subtree
	// (record.go), and a part that finishes changes a row that is already on
	// screen — so the transcript is rebuilt from the record rather than grown,
	// and the record has to still exist to be rebuilt from.
	messages []store.Message
	// stamp is what the transcript was last built from (recordStamp). A journal
	// move that did not touch this task costs one string comparison.
	stamp string

	// The nested-record fields (recordpage.go). A record page can be entered
	// FROM another record page — clicking an atomic leaf in a job's tree opens
	// that worker's own page — and esc walks back one level at a time.
	//
	// parent is the page this one was drilled into from, kept whole rather than
	// rebuilt, so walking back costs no read. offset is the scroll position that
	// page was at when it was left, so the reader lands exactly where they were
	// reading (8.1.6). anchored says this page has already been positioned once
	// on entry — settled pages open on their result card, running ones at the
	// live tail — and it is what keeps that from ever happening twice, because
	// manual scrolling is never fought.
	parent   *mainView
	offset   int
	anchored bool
}

// composerBind is what the composer is talking to right now (5.15: "you talk to
// what you're looking at").
//
// note is filled only for a disabled composer and is the whole of 5.20 rule 3's
// "disabled affordances say why". A disabled composer with no words would be
// the same dead end as a modal with no cancel.
type composerBind struct {
	mode rail.ComposerMode
	// node anchors a steered draft. Empty posts to the room itself.
	node string
	// note is what a disabled composer says instead of taking a draft.
	note string
}

// -- the pane ----------------------------------------------------------------

// scopePane is the rail's place in the shell.
//
// It is thin on purpose: [rail.Pane] already joins a model to a view, and this
// type adds only the three things a component may not own — which rendering the
// current terminal width asks for, what a keystroke means, and what focus does
// to the paint. Dimming is a property of the pane (8.3), so focus re-points the
// styler and repaints; nothing per row is re-resolved.
type scopePane struct {
	pane  rail.Pane
	style *tokens.Styler
	// mode reports which of the three renderings this frame wants. It is a
	// function because the answer is the TERMINAL's width, and a pane is only
	// ever told its own rectangle (pane.go's contract).
	mode func() rail.Mode
	// slim reports that this frame drew the collapsed rail's handle rather than
	// the map. It is a function for the same reason mode is: the answer is the
	// SOLVED FRAME's, and a pane is only ever told its own rectangle — a pane
	// that inferred it from being one column wide would be guessing, and would
	// guess wrong the day the handle earns a second column.
	slim func() bool
	// expand is what a click on the handle performs. The handle has exactly one
	// act and this is it (see [App.expandRail]).
	expand func() tea.Cmd
	keys   func(tea.KeyPressMsg) (tea.Cmd, bool)
	// point is what the pointer did to the map. It is a function for the same
	// reason keys is: the ACT belongs to the app — selecting, entering, popping
	// — and the pane's whole job is to say which row was pointed at.
	point func(railPoint) tea.Cmd
}

// railPoint is one pointer gesture over the map, resolved to rows rather than
// to cells. Exactly one field is ever set.
type railPoint struct {
	// row is the model row index a click landed on, or -1 for none.
	row int
	// up says the click was on the way out of the scope (the ‹).
	up bool
	// wheel is -1 for a notch up and +1 for a notch down; 0 when the gesture
	// was not a wheel.
	wheel int
}

var (
	_ tui2.Pane      = (*scopePane)(nil)
	_ tui2.PaneKeys  = (*scopePane)(nil)
	_ tui2.PaneFocus = (*scopePane)(nil)
	_ tui2.PaneMouse = (*scopePane)(nil)
	_ tui2.PaneHover = (*scopePane)(nil)
)

// Render draws the scope at whichever rendering the frame asked for.
func (p *scopePane) Render(width, height int) string {
	if p == nil || width <= 0 || height <= 0 {
		return ""
	}
	if p.mode != nil {
		p.pane.Mode = p.mode()
	}
	// The handle is a RENDERING of the same pane, not a second pane: the model
	// keeps its cursor, its scope stack and its place across a collapse, so
	// expanding costs a flag rather than a rebuild and the reader comes back to
	// the row they left.
	p.pane.Slim = p.slim != nil && p.slim()
	return p.pane.Render(width, height)
}

// Key offers the keystroke to the scope grammar. A key the rail does not claim
// goes back to the shell, which is what keeps ctrl+c and the scroll gestures
// working while the map holds focus.
func (p *scopePane) Key(msg tea.KeyPressMsg) tea.Cmd {
	if p.keys == nil {
		return nil
	}
	cmd, _ := p.keys(msg)
	return cmd
}

// Mouse is 5.15's "j/k (or click) moves the rail selection", plus the way out.
//
// It resolves a cell to a row and hands the row on; it decides nothing about
// what pointing at a row MEANS. That split is what makes click parity a fact
// rather than a promise: the app answers the pointer with the same calls the
// keyboard reaches (Move, Select, Enter, Escape), so a gesture cannot grow a
// behaviour the keyboard does not have.
func (p *scopePane) Mouse(msg tea.MouseMsg, local image.Point) tea.Cmd {
	if p == nil || p.point == nil {
		return nil
	}
	switch event := msg.(type) {
	case tea.MouseWheelMsg:
		switch event.Button {
		case tea.MouseWheelUp:
			return p.point(railPoint{row: -1, wheel: -1})
		case tea.MouseWheelDown:
			return p.point(railPoint{row: -1, wheel: 1})
		}
		return nil
	case tea.MouseClickMsg:
		if event.Button != tea.MouseLeft {
			return nil
		}
		if p.slim != nil && p.slim() {
			// THE WHOLE HANDLE IS THE TARGET, its blank rows included. It is one
			// column wide and the thing on it is at most one cell; a target a
			// reader has to hit exactly would be an affordance only a mouse with
			// good aim can reach, and the column has nothing else on it that a
			// click could have meant instead.
			if p.expand == nil {
				return nil
			}
			return p.expand()
		}
		if p.pane.ScopeUpAt(local.X, local.Y) {
			return p.point(railPoint{row: -1, up: true})
		}
		row, ok := p.pane.RowAt(local.Y)
		if !ok {
			// The empty space under a short rail is not the room at the top of
			// it. A click there is a click on nothing, and answering it with a
			// navigation would be the surface guessing.
			return nil
		}
		return p.point(railPoint{row: row})
	}
	return nil
}

// Hover previews. It never returns a command and never moves the cursor — the
// pointer resting on a row is not the reader being in it (5.14).
func (p *scopePane) Hover(local image.Point, inside bool) bool {
	if p == nil {
		return false
	}
	return p.pane.Hover(local.X, local.Y, inside)
}

// Focus re-points the view's styler. The contrast law (tokens.Legal) forbids a
// dimmed foreground on a raised band, so an unfocused rail marks its selection
// with the accent rail instead of a band — the view already knows how, and this
// is how it is told which state it is in.
func (p *scopePane) Focus(focused bool) {
	if p.pane.View == nil || p.style == nil {
		return
	}
	if focused {
		p.pane.View.SetStyler(p.style.WithFocus(tokens.FocusNormal))
		return
	}
	p.pane.View.SetStyler(p.style.WithFocus(tokens.FocusDimmed))
}

// -- wiring ------------------------------------------------------------------

// buildScope assembles the source, the model, the view and the pane, and binds
// the composer to whatever the cursor starts on.
func (a *App) buildScope() {
	a.source = newScopeSource(a.backend, a.session, a.now)
	// The thread index BEFORE the first build, so the very first frame's rail
	// carries left-at lines rather than a list of bare names that fills in when
	// something else happens to open a door.
	a.source.setThreads(a.readThreads())
	a.source.refresh(0, true)
	a.railModel = rail.New(a.source)
	a.hudModel = rail.New(hudSource{a.source})
	a.railView = rail.NewView(a.style)
	a.hudView = rail.NewView(a.style)
	a.scope = &scopePane{
		pane:  rail.Pane{Model: a.railModel, View: a.railView},
		style:  a.style,
		mode:   a.railMode,
		slim:   a.railSlim,
		expand: a.expandRail,
		keys:   a.scopeKey,
		point:  a.scopePoint,
	}
	a.scope.Focus(false)
	a.bind(a.railModel.Preview(), false)
}

// railMode is the breakpoint decision, made on the TERMINAL's width rather than
// on the pane's. At and above [tokens.RailAtWidth] the pane is a 28-column
// column and would otherwise measure itself into the narrow rendering; below it
// the pane IS the main column and the same rows, keys and selection render as a
// full-pane list (5.15, Part 9.12).
func (a *App) railMode() rail.Mode {
	return rail.ModeFor(a.termWidth)
}

// wide reports that this frame draws the rail as a column beside the
// transcript. It is the same question the layout solver answers, asked of the
// same number, and it is what decides whether the bounded HUD has a job (8.2.8:
// the HUD is the NARROW fallback, never a second rail).
func (a *App) wide() bool {
	return a.termWidth >= tokens.RailAtWidth
}

// railSlim reports that the solved frame drew the handle rather than the map.
//
// It asks the SHELL and not [App.railState], because the two are different
// questions: the flag is what the reader asked for and this is what the width
// could pay for. A terminal too narrow for 28 columns draws the handle without
// anybody having chosen it, and — this is the half that matters — without the
// stored preference being touched, so widening the window brings the column
// back rather than needing the chord again.
func (a *App) railSlim() bool {
	return a.shell != nil && a.shell.RailSlim()
}

// -- the scope grammar -------------------------------------------------------

// scopeKey is the map's keyboard. It reports whether it claimed the key.
//
// The vocabulary is small on purpose: 5.15 kills the focus-zone carousel, so
// there is one cursor and four gestures over it. Digits jump because a rail is
// a numbered list to the eye whether or not it draws the numbers, and reaching
// row seven by pressing j six times is the interaction a list is supposed to
// save you.
func (a *App) scopeKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if a.railModel == nil {
		return nil, false
	}
	key := msg.String()
	switch key {
	case "j", "down":
		return a.applyScope(a.railModel.Move(1)), true
	case "k", "up":
		return a.applyScope(a.railModel.Move(-1)), true
	case "home", "g":
		return a.applyScope(a.railModel.Select(0)), true
	case "end", "G":
		return a.applyScope(a.railModel.Select(a.railModel.Len() - 1)), true
	case "enter":
		// The group is a LID and not a room: enter expands it in place rather
		// than descending, because 5.24 collapses it so live work keeps the top
		// of the rail and a reader opening it wants the four rows, not a fifth
		// surface between them and the rooms.
		if a.railModel.Selected().ID == homes.GroupRowID {
			return a.toggleHomes(), true
		}
		return a.applyScope(a.railModel.Enter()), true
	case "esc":
		event := a.railModel.Escape()
		if event.Empty() {
			// Nothing to pop: the map is at home, so the key belongs to the
			// app's own ladder (8.2.21) and the map hands the keyboard back to
			// the conversation — and, now that §6 makes the rail a drawer, shuts
			// it on the way. Esc is "put this away" everywhere else in the
			// product (an overlay, a scope, a turn); a rail that stayed standing
			// after esc had emptied it would be the one surface where the key
			// meant "look elsewhere" instead.
			//
			// IT PUTS AWAY ONE STEP, to the handle rather than to nothing.
			// Esc is a retreat and not a decision — the reader is going
			// back to the conversation, not declaring that they never want
			// the sidebar again — and the handle is exactly that
			// distinction made visible: the column is gone and the one
			// signal it carries is not.
			return a.setRail(tui2.RailSlim), true
		}
		return a.applyScope(event), true
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		// A DIGIT COUNTS PLACES, NOT LINES. The rail is a numbered list to the
		// eye whether or not it draws the numbers, and the eye does not number
		// the section headings — nobody looks at `work` and counts it as an
		// item. So the digit walks the rows a cursor may rest on, and a heading
		// costs a reader nothing, exactly as it costs them nothing when they
		// press j past it ([rail.Model.Move]).
		return a.applyScope(a.railModel.Select(nthPlace(a.railModel.Rows(), int(key[0]-'1')))), true
	}
	return nil, false
}

// nthPlace is the model index of the n-th row a cursor may rest on, counting
// from zero. A list with fewer places than that returns the last row's index,
// which [rail.Model.Select] then clamps and snaps like any other.
func nthPlace(rows []rail.Row, n int) int {
	for i := range rows {
		if !rows[i].Kind.Selectable() {
			continue
		}
		if n == 0 {
			return i
		}
		n--
	}
	return len(rows) - 1
}

// scopePoint is the map's pointer, and it is the keyboard's grammar reached by
// a different hand. Every branch below ends in a call scopeKey also makes.
//
// The one decision this function makes on its own is what ONE CLICK means, and
// 13.18 settles it against the way it used to read. It used to take 5.15's
// select/open split literally — first click previews, second click enters — on
// the argument that a pointer should not be able to commit to a room the reader
// had not seen a preview of. Measured against a hand, that argument is upside
// down: a preview is what the KEYBOARD needs, because an arrow is how a keyboard
// looks around and the look must not cost a room. A pointer does its looking
// with the eye, on the card that is already drawn beside the row; by the time it
// presses, the reader has decided. The old law answered that decision with three
// cells of card and a composer bound to something they had not asked for, and
// the reader's report was the plainest kind: clicking a task did nothing.
//
// So a click is an ENTER, on the same call enter makes. The exceptions are the
// two rows that PERFORM rather than navigate — `+ new room` mints a room and the
// group lid opens and shuts — where the second click is not ceremony but the
// difference between pointing at something and doing it.
func (a *App) scopePoint(pt railPoint) tea.Cmd {
	if a.railModel == nil {
		return nil
	}
	switch {
	case pt.wheel != 0:
		// A wheel over the map moves the selection, not a viewport: the rail
		// has no scroll of its own — the fold is what accounts for rows that do
		// not fit (rail/fold.go) — so the only thing a notch can honestly move
		// is the cursor.
		a.focusScope(true)
		return a.applyScope(a.railModel.Move(pt.wheel))
	case pt.up:
		// The header is the breadcrumb tail and is the way out (5.15).
		event := a.railModel.Escape()
		if event.Empty() {
			return nil
		}
		a.focusScope(true)
		return a.applyScope(event)
	case pt.row < 0:
		return nil
	}

	rows := a.railModel.Rows()
	if pt.row >= len(rows) {
		// The map moved under the click — a poll landed between the paint the
		// hand aimed at and the press. Answering with a navigation would be the
		// surface acting on a row that is no longer there.
		return nil
	}
	// Pointing at the map is talking to the map: the keyboard comes with the
	// pointer, so the next j or enter lands where the eye already is. This is
	// the shell's focus rule (a click focuses what it hit) stated in the app's
	// own terms, because the rail's focus is the app's flag and not the shell's.
	// It is read BEFORE the focus moves, or every first click would look like a
	// second one to the two rows below that still care.
	second := a.railFocus && a.railModel.Cursor() == pt.row
	a.focusScope(true)

	if id := rows[pt.row].ID; id == rowNewRoomID || id == homes.GroupRowID {
		if !second {
			return a.applyScope(a.railModel.Select(pt.row))
		}
		if id == homes.GroupRowID {
			return a.toggleHomes()
		}
		return a.applyScope(a.railModel.Enter())
	}

	// One click, one room. The select is made and NOT applied: it moves the
	// cursor the enter is about to read, and the card it would have drawn is a
	// frame nobody asked to see — the room replaces it in the same keystroke.
	a.railModel.Select(pt.row)
	return a.applyScope(a.railModel.Enter())
}

// scopeToggle is the chord the shell records the scope gesture under. It is
// synthesized rather than named through a setter because the shell exposes the
// gesture as a binding and not as state — and a second door into one flag is how
// a surface ends up with two answers to "is the map open".
var scopeToggle = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

// setScope opens or closes the scope map and moves the keyboard with it.
//
// In a WIDE frame the rail is always drawn (4.3: persistent, not toggle-hidden)
// and this only decides who has the keyboard. In a NARROW frame the same call
// swaps the main pane to the scope list, because there the rail is not a column
// beside anything — it IS the main pane, and focusing something that is not on
// screen would be a keystroke with no visible effect (5.20).
func (a *App) setScope(on bool) tea.Cmd {
	var cmd tea.Cmd
	if a.scopeOpen != on {
		a.scopeOpen = on
		_, cmd = a.shell.Update(scopeToggle)
	}
	a.focusScope(on)
	return cmd
}

// focusScope moves the keyboard between the conversation and the map, and tells
// both panes so each paints the state it is actually in.
func (a *App) focusScope(on bool) {
	if a.railFocus == on {
		return
	}
	a.railFocus = on
	a.scope.Focus(on)
	a.composer.Focus(!on)
	a.refresh()
}

// applyScope folds one rail event into the surface: what the main pane shows,
// what the composer binds, and where the breadcrumb says the reader is.
func (a *App) applyScope(event rail.Event) tea.Cmd {
	if event.Empty() {
		return nil
	}
	// A POP IS NOT AN OPEN. EventScopePopped means "the cursor is back on the
	// row it descended from" (rail's own words), and that row is the one the
	// reader has just chosen to leave. Committing on it re-opened the room they
	// escaped from: esc out of a task left the main pane pointing at that task's
	// room — openTaskRoom sees the same node and returns early, so the pane
	// stayed there at home, for the rest of the session — and esc out of a home
	// bounced straight back in, because the homes branch calls Enter again on a
	// commit. Both were caught in one screenshot pass.
	//
	// 5.15 settles it without ambiguity: "selection previews; enter opens", and
	// "esc pops scope, never just selection". A pop is a movement of the map, so
	// what it produces is the preview any other movement produces.
	commit := event.Kind == rail.EventOpened || event.Kind == rail.EventScopeEntered
	return a.bind(event, commit)
}

// bind is the one place a rail event becomes surface state.
//
// commit separates the two halves of 5.15's select/open split: a preview draws a
// card, and only a commitment pays for a room.
//
// THE COMPOSER FOLLOWS THE SURFACE, NOT THE CURSOR (13.18). Every branch that
// leaves the main pane on a CARD leaves [App.composerBind] exactly where it was,
// and it is the only rule in this function that has to be read across branches
// rather than inside one — so it is stated here: a card is a look at a surface
// and not a surface, and a look may not rebind the mouth. The branches that do
// rebind are the ones that put a real surface in the main pane — the room's own
// thread, a home, an entered task room — because there the composer and what is
// on screen are the same object. The bug this closes was reported as "clicking a
// task does nothing": a preview of settled work bound a DISABLED composer, and
// from that moment [App.focusConversation] refused the reader's clicks and the
// map ate the letters they typed. The words the preview owed them ride on the
// card instead, where they name the key rather than take the keyboard.
func (a *App) bind(event rail.Event, commit bool) tea.Cmd {
	row := a.railModel.Selected()
	id := event.RowID
	var cmd tea.Cmd

	switch {
	case id == rowHomeID:
		a.showThread()
		a.composerBind = composerBind{mode: rail.ComposerChat}

	case id == rowNewRoomID:
		if commit {
			cmd = a.openRoomCmd()
		}
		// The composer stays where it was, because the reader is still standing
		// in the room they were in and it still takes their draft. Disabling it
		// here used to survive the mint — nothing on the way through
		// applyRoomOpened bound it back — so a fresh room opened with a mouth
		// that refused to take a word.
		//
		// THE CARD IS THE ONE THAT HAS TO SAY WHERE THE DRAFT GOES, and until
		// 13.19 it did not: the reader who selected this row saw a card and
		// believed they were in the new room, typed, and their words went into
		// the old room — invisibly, because the pane was showing the card. Two
		// answers, and they are both here. The card is a real empty state now
		// ([App.showNewRoomCard]) that names both ways in, and the first
		// printable key is one of them ([App.mintOnType]) — so a person acting
		// on the belief the card used to leave them with is right.
		a.showNewRoomCard(row)

	case id == homes.GroupRowID:
		a.showCard(row, "enter opens the rest of aforge")

	case homes.Owns(id):
		home, _ := homes.ParseScopeID(id)
		// DESCEND ONLY FROM OUTSIDE. A home is reachable by two rows that carry
		// the same id: the RowStep in the group at home, which you enter, and
		// the RowSurface at the top of the home's own scope, which you are
		// already standing on. rail.Model.Enter on a surface row returns
		// EventOpened for that same id — a surface has nothing beneath it — so a
		// branch that entered again on every commit called itself with an
		// identical event forever. It was a real stack overflow two keystrokes
		// from the home rail, and it is why this guard is on the row's KIND
		// rather than on the event: the kind is the thing that actually says
		// whether there is anywhere left to go.
		if commit && row.Kind != rail.RowSurface {
			cmd = a.applyScope(a.railModel.Enter())
		}
		a.showHome(homes.Selection{Home: home})
		a.composerBind = composerBind{mode: home.Composer()}

	case strings.HasPrefix(id, homes.ServiceRowPrefix):
		a.showHome(homes.Selection{Home: homes.HomeServices, Row: id})
		// This case MUST disable the composer rather than leaning on the row's
		// own ComposerNone: composerMode (app.go) coerces None to Chat, which is
		// right for its own reason and is the one row kind it must not reach.
		a.composerBind = composerBind{mode: rail.ComposerDisabled,
			note: "a service is not a conversation — ask aforge about it"}

	case strings.HasPrefix(id, homes.BeliefRowPrefix),
		strings.HasPrefix(id, homes.CharterRowPrefix),
		strings.HasPrefix(id, selfRowPrefix):
		a.showHome(homes.Selection{Home: a.scopeHome(), Row: id})
		a.composerBind = composerBind{mode: rail.ComposerChat}

	case strings.HasPrefix(id, rowRoomPrefix):
		session := strings.TrimPrefix(id, rowRoomPrefix)
		if session == a.session {
			a.showThread()
			a.composerBind = composerBind{mode: rail.ComposerChat}
			break
		}
		if commit {
			a.switchRoom(session)
			a.composerBind = composerBind{mode: rail.ComposerChat}
			break
		}
		a.showCard(row, "enter opens this room")

	case strings.HasPrefix(id, rowTaskPrefix):
		node := strings.TrimPrefix(id, rowTaskPrefix)
		if !commit {
			a.showCard(row, "")
			break
		}
		cmd = a.openTaskRoom(row, node)
		a.composerBind = bindWork(row, node)

	default:
		a.showCard(row, "")
	}

	a.handOverTheKeyboard(commit)
	a.refresh()
	return cmd
}

// -- typing your way into a fresh room (13.19) --------------------------------

// previewingNewRoom reports whether the main pane is showing the `+ new` card.
//
// It asks the VIEW rather than the rail's cursor, and the difference is the whole
// point: the cursor can be resting on a row whose card is not what is drawn — a
// commit swaps the pane to a room and leaves the cursor where it was — and what
// this question is really about is what the READER CAN SEE. The trap being closed
// was a preview of nothing on screen while the mouth pointed somewhere else, so
// the screen is what has to answer.
func (a *App) previewingNewRoom() bool {
	return a.view != nil && a.view.kind == viewCard && a.view.rowID == rowNewRoomID
}

// mintOnType is the `+ new` card's second door: the first printable key makes the
// room and lands in it.
//
// THE INCIDENT. A reader selected `+ new`, the pane drew the card, and — believing
// they were in the new room, which is what a card with a room's name on it means —
// they typed a sentence and sent it. The composer was still bound to the room
// they had been standing in, deliberately (see [App.bind]), and the pane was
// showing the card, so their words went into a conversation they could not see and
// were answered with that conversation's whole context behind them. Nothing lied
// about a key; the surface simply had no answer for the most natural thing a
// person can do in front of an empty room, and silence was the answer it gave.
//
// The intent is unambiguous — nobody selects `+ new` and starts typing in order to
// speak to the room they just left — so the keystroke is taken as the commitment
// it obviously is. Enter still opens; this only adds the door a person was
// already trying to walk through.
//
// THE ORDERING, which is the part that has to be right:
//
//   - The mint is issued but not waited for. It is an ordinary [tea.Cmd] and the
//     store answers on another goroutine.
//   - The KEYBOARD moves to the composer synchronously, before the letter is
//     delivered. Without this the map still holds it and the next letters would be
//     eaten by j/k/g/G — 13.8's "jack knife kayak" arriving as "ac nife aya", one
//     wave later.
//   - The BINDING becomes a chat synchronously, for the same reason and one more:
//     a composer left disabled by whatever the reader was previewing before would
//     refuse the very key that just minted a room.
//   - The LETTER is delivered synchronously, into that composer. It survives the
//     mint by simply staying where it was put: [App.switchRoom] resets the
//     transcript, the watermarks and the rail, and touches no draft.
//   - A SEND that beats the store is held rather than posted ([mintHold]), so even
//     a paste-and-enter faster than a disk write cannot reach the old room.
//
// The pane deliberately does NOT swap to the old room's thread on the way past.
// The card stands until the new room lands, because the one thing this frame must
// never do again is show a reader a conversation their words are not going into.
func (a *App) mintOnType(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// ONE ROOM PER SENTENCE. The card deliberately stands until the store answers
	// — see the note at the foot of this comment — so without this guard the
	// second letter would find the same frame the first one did and mint again,
	// and a person typing eight characters would leave eight rooms behind them.
	// The gap is the flag: while a mint is in flight the door is already walked
	// through, and every later key is an ordinary letter for the composer.
	if a.composer == nil || a.mint.active || !a.previewingNewRoom() || !typedRune(msg) {
		return nil, false
	}
	mint := a.openRoomCmd()
	if mint == nil {
		// A window with no rooms door. [App.openRoomCmd] has already said so on
		// the status line; the key falls through to whatever would have had it.
		return nil, false
	}
	a.composerBind = composerBind{mode: rail.ComposerChat}
	a.focusScope(false)
	typed := a.composer.Key(msg)
	a.sizeComposer()
	a.shell.Invalidate()
	return tea.Batch(mint, typed), true
}

// typedRune reports whether a keystroke is a person writing a character, as
// opposed to a person navigating.
//
// It is the composer's OWN test for insertion (composer/key.go's default arm:
// text that is not empty becomes a rune in the draft), narrowed by two things the
// composer can afford to be relaxed about and this door cannot. A MODIFIER means
// the key is a chord, and a chord is an instruction rather than a letter — no
// accelerator anywhere in this product should be able to mint a room. A
// NON-PRINTING rune means a key cap that happens to carry text (enter's carriage
// return is the one that matters), and a key cap is not a sentence.
//
// Shift is not a modifier for this purpose: a capital letter is a letter.
func typedRune(msg tea.KeyPressMsg) bool {
	if msg.Mod&^tea.ModShift != 0 {
		return false
	}
	text := msg.Key().Text
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// handOverTheKeyboard is 5.15's one rule at the moment it matters most: you
// talk to what you are looking at, and after a commitment what you are looking
// at is a room with a live composer under it.
//
// The bug this closes was reported from a live conversation and is worth
// naming, because "the affordance never lies" is usually about what is drawn
// and this was about what a key does. Entering a room left the keyboard on the
// map while the composer redrew as live, so a sentence typed into it was eaten
// letter by letter by the map's own bindings — "jack knife kayak" arrived as
// "ac nife aya", because j and k are the map's movement keys and g, G and the
// digits are its jumps — and Enter opened a rail row instead of sending. One
// extra ctrl+o fixed it, which is the definition of a state the reader had to
// know about and could not see.
//
// It is deliberately scoped to a COMMIT and to a composer that will actually
// take a draft:
//
//   - A PREVIEW keeps the keyboard on the map. Walking with j and k has to stay
//     walking, or the map would be usable for exactly one row.
//   - A POP keeps the keyboard on the map, because applyScope does not count a
//     pop as a commit: the reader who pressed esc is navigating, not arriving.
//   - A DISABLED composer keeps the keyboard on the map. An entered room over
//     settled work and a service's home take no draft, so handing them the
//     keyboard would move it to a pane that refuses every key — the same
//     invisible dead end in the other direction.
func (a *App) handOverTheKeyboard(commit bool) {
	if !commit || !a.railFocus || a.composerBind.mode == rail.ComposerDisabled {
		return
	}
	a.focusScope(false)
}

// focusConversation is [handOverTheKeyboard] reached by a hand instead of by a
// commitment: a click landed on the conversation side — the transcript or the
// composer region — so that is what the reader is talking to.
//
// 5.14 says you talk to what you are looking at, and 13.14 already read the
// pointer half of that rule in the map's direction ("pointing at the map is
// talking to the map"). This is the same sentence read backwards, and until this
// lane it was the half nobody had written: `scopePoint` moved custody TOWARD the
// map and nothing moved it back, so the surface had a one-way door. Reported
// verbatim, from a live session: "clicking on the typing part or anywhere does
// not seem to go there — I have to press ctrl+o".
//
// The shell was not the missing piece and adding a rule there would not have
// fixed it. `Shell.setFocus` already moves its own LayerID on every click, and
// the composer already REPAINTS as focused because of it — which is why the bug
// was invisible in a screenshot and only findable from a keyboard. What decides
// where a keystroke goes in this surface is [App.railFocus], and the shell has
// never known about it (App.key routes before the shell sees a key at all).
//
// It refuses in exactly one case, and it is [handOverTheKeyboard]'s own: a
// DISABLED composer takes no draft, so handing it the keyboard would move the
// cursor to a pane that refuses every key and leave j/k walking nothing. A
// reader standing in a room over settled work keeps the map when they click the
// transcript, which is the state that room's own composer is describing.
//
// 13.18 narrowed how often that refusal fires, and the narrowing IS the fix a
// reader asked for. It used to fire on a PREVIEW: resting the cursor on settled
// work disabled the composer, and from then on the surface refused every click
// on the conversation side until the reader guessed at ctrl+o. Now only an
// entered surface can disable the mouth, so the refusal happens where a reader
// can see the reason for it.
func (a *App) focusConversation() bool {
	// A page holds the keyboard the way the map does (app.go's setPageFocus), so
	// a click on the conversation side takes it back from a page too — the same
	// one-way door this function exists to close, one lens over.
	if a.pageFocus {
		a.setPageFocus(false)
		a.shell.Invalidate()
	}
	if !a.railFocus {
		return true
	}
	if a.composer == nil || a.composerBind.mode == rail.ComposerDisabled {
		return false
	}
	a.focusScope(false)
	a.shell.Invalidate()
	return true
}

// bindWork is 5.15's one rule applied to a work row.
//
// Every work row in v1 binds the STEER line, not a chat. 4.6 is explicit that a
// v1 task room is a view and not a second agent: its composer posts through the
// existing journal verbs and the one head answers. A `›` on a task row would
// promise a conversation with an orchestrator that does not exist yet — the
// exact lie 5.11 spends a second prompt glyph to prevent. When orchestrators
// become resident-side loops (4.6's v2), a planned task's row earns its chat and
// this function is where that lands.
func bindWork(row rail.Row, node string) composerBind {
	if row.Life.Terminal() {
		return composerBind{mode: rail.ComposerDisabled,
			note: "this work is settled — ask aforge about it"}
	}
	return composerBind{mode: rail.ComposerSteer, node: node}
}

// -- the main pane's three lenses --------------------------------------------

// showThread points the main pane back at the room's own conversation.
func (a *App) showThread() {
	a.view = nil
	a.pane.homes = nil
	a.pane.transcript = a.transcript
	a.shell.Invalidate()
}

// showCard draws the lightweight preview a cursor move produces (5.15).
//
// Everything on it came from the row the rail already handed over, so a
// keystroke costs one block and no store read. note is the affordance line: what
// enter would do, in words, for a row whose room is not open.
func (a *App) showCard(row rail.Row, note string) {
	transcript := blocks.New(80, 24)
	transcript.Append(cardBlock(row, note, a.style))
	a.view = &mainView{kind: viewCard, title: row.Name, rowID: row.ID, transcript: transcript}
	a.pane.homes = nil
	a.pane.transcript = transcript
	a.shell.Invalidate()
}

// showNewRoomCard draws the `+ new` door's own lens: an empty state, not a
// preview of a row.
//
// It is [App.showCard]'s sibling and deliberately not a call to it. Every other
// card is a LOOK AT SOMETHING — a room with a last line in it, a job with a
// status and a cost — and [cardBlock] is the renderer for exactly that: the row's
// name under its lifecycle glyph, its telemetry, its artifact. Pointed at a door,
// that renderer had nothing true to say and said it anyway. The shipped frame
// read `○ + new` over a bare `$—`: a queued-state glyph on something that is not
// work, a label that is a button caption rather than a title, and 10.2.8's honest
// missing-money mark answering a question nobody asked about a room that does not
// exist yet. §15's delete test takes all three away and loses nothing.
//
// What replaces them is what a reader actually needs here, in the order they
// need it: what a fresh room IS, that it will name itself, and the two ways in.
// See [newRoomCardBlock] for the typography.
func (a *App) showNewRoomCard(row rail.Row) {
	transcript := blocks.New(80, 24)
	transcript.Append(&newRoomCardBlock{style: a.style})
	// The TITLE is the card's own word and not the row's. `+ new` is a door's
	// label — a verb with a mark in front of it — and the breadcrumb it feeds
	// says where the reader IS, which is in front of a fresh room (5.14, 12.13.2).
	a.view = &mainView{kind: viewCard, title: newRoomCardTitle, rowID: row.ID, transcript: transcript}
	a.pane.homes = nil
	a.pane.transcript = transcript
	a.shell.Invalidate()
}

// openTaskRoom swaps the main pane to a task's room: the node-anchored trail and
// its receipts, dressed by the same renderers the conversation uses (4.6).
//
// IT OPENS ON THE CARD, NOT ON NOTHING. The room used to open on an empty
// transcript and fill it when readNodeCmd came back, which is right for a task
// that has journaled something and a void for one that has not — and a task
// seconds old has not. A screenshot caught the void: enter on a running atomic
// job produced a completely blank main pane, which is the same picture an
// unwired room draws, and 12.10's warning is exactly that those two must never
// look alike. 5.20's first rule says the surface may not leave a reader
// guessing which of the two they are looking at.
//
// So the room opens holding the card the reader entered from, plus the line
// saying what will appear here and why nothing has. It costs one block and no
// read — every cell comes from the row the rail already handed over — and the
// moment the first real row lands it is cleared away (applyNodeMessages), so
// the teaching is only ever on screen while it is true.
func (a *App) openTaskRoom(row rail.Row, node string) tea.Cmd {
	if node == "" {
		return nil
	}
	if a.view != nil && a.view.kind == viewNode && a.view.node == node {
		return nil
	}
	transcript := blocks.New(80, 24)
	a.view = &mainView{
		kind: viewNode, node: node, title: row.Name,
		transcript: transcript, card: row,
	}
	a.pane.homes = nil
	a.pane.transcript = transcript
	// The room is painted from the RECORD before the trail is asked for, and
	// that ordering is the whole of this wave. The record is already in hand —
	// it is the same snapshot the rail drew the card and the tree from — so a
	// room over work that has journaled a plan, a part or a result opens holding
	// them, in the same frame the key was pressed in. Only the trail costs a
	// read, and it lands underneath when it arrives.
	a.paintRoom()
	a.shell.Invalidate()
	// Three reads, one entry, and each one answers a question the other two
	// cannot: the journal's trail, the job's own PLAN (which the board's
	// snapshot drops the moment the job is filed away — see [Subtrees]), and
	// what the workers under it actually DID, which is a file the executor
	// writes outside the journal entirely (trace.go).
	return tea.Batch(a.readNodeCmd(node, 0), a.readTraceCmd(node), a.readSubtreeCmd(node))
}

// paintRoom rebuilds the open task room from the record.
//
// It is a REBUILD and not an append, because the record is not append-only: a
// part that starts, finishes or fails changes a row the reader is already
// looking at, and a room that could only grow would keep drawing "queued" over
// work that had finished. The cost of that is bounded twice over — the subtree
// is capped at maxSubtreeRows and the trail at messagePage — and it is paid only
// when [recordStamp] says something this task owns actually moved.
//
// The reader's place survives it. A transcript that was following the tail keeps
// following it; one the reader had scrolled back into keeps its offset, because
// the blocks are keyed by ids that do not move (record.go) and the transcript
// anchors on ids rather than on indices.
func (a *App) paintRoom() {
	view := a.view
	if view == nil || view.kind != viewNode || view.transcript == nil {
		return
	}
	record := a.source.workRecordAt(view.node)
	// The ledger is taken BEFORE the stamp and handed to the build below, so the
	// fingerprint and the frame are one read: a receipt that landed between them
	// would repaint on a figure the page did not draw, and the next poll would
	// find the stamp already agreeing and never draw it (record.go's
	// [recordStamp], and 12.14's one-snapshot rule).
	money := a.source.roomSpend(view.node)
	stamp := recordStamp(record, view.messages, money)
	if view.stamp == stamp && view.transcript.Len() > 0 {
		return
	}
	view.stamp = stamp

	// The reader's own fold answers go IN to the build, because in the
	// execution rows they decide which blocks EXIST: an opened batch lays out
	// the calls it collapsed, and an opened result grows the continuation that
	// holds the rest of it (trace.go). Every other block reads them on the way
	// out, through applyFold below, exactly as before.
	rows := roomBlocks(recordInputs{
		record:   record,
		messages: view.messages,
		style:    a.style,
		board:    a.source,
		traces:   a.source.traceFor(view.node, a.traces),
		open:     a.foldOpen,
		treeOpen: a.foldOpenDefault,
		// The page's OWN ledger and not the rail's: a room can be standing over
		// a node the board never read receipts for — a drilled-into part, or a
		// job the home rail's bounded top-up has not reached — and it is the one
		// read that puts money on this page's header and on every row of its
		// tree ([scopeSource.roomSpend]).
		money: money,
		// The ask's other half, read off the command this job was admitted from
		// rather than off the node, and folded rather than led with (record.go).
		context:    a.source.jobContext(view.node),
		models:     a.source.jobModels(view.node),
		nodeModels: a.source.jobModels,
		// The attribution row's seam (5.3): only this side knows what a session
		// is called, and only this side knows which one the reader is standing
		// in. See [App.forThreadRow].
		forThread: a.forThreadRow,
		now:       a.now(),
		clock:     view.transcript.Clock(),
	})
	// AN EMPTY ROOM MUST SAY IT IS EMPTY (12.14 finding 4), and it must say so
	// only while it is TRUE. The teaching line used to appear whenever the
	// message trail was empty, which for a resident-run task is nearly always —
	// so a room over a job with a plan, six parts and seven thousand characters
	// of journaled result said "nothing journaled here yet". It is the record
	// that decides now, and the record is everything the journal holds about
	// this subtree.
	if len(rows) == 0 {
		view.teaching = true
		view.transcript.Truncate(0)
		view.transcript.Append(cardBlock(view.card, "", a.style))
		view.transcript.Append(&noteBlock{id: "empty-room", text: emptyRoomNote, style: a.style})
		a.shell.Invalidate()
		return
	}
	view.teaching = false

	// Truncate rather than Reset, and the difference is the reader's place.
	// Reset re-pins the transcript to the bottom, so a reader who had scrolled
	// back to read what an early part said would be thrown to the tail every
	// time any part of the job moved — a row moving under the eye that is
	// reading it, which is the one motion 8.1.6 forbids outright. Truncate keeps
	// the offset, the follow flag and the anchor, and the anchor is by block id;
	// the ids here do not move, so the rows come back where they were.
	following := view.transcript.Following()
	view.transcript.Truncate(0)
	foldable := false
	for _, block := range rows {
		if message, ok := block.(*messageBlock); ok {
			// A fold that is already open stays open across a repaint, and this
			// is the line that makes 7.2's "state that survives re-render" true:
			// the whole list is rebuilt on every journal move (13.15's decision
			// 2), so without it a row the reader opened would slam shut the next
			// time any part of the job breathed. The reader's own per-row answer
			// outranks the room-wide one; both live in disclose.go.
			a.applyFold(message)
			foldable = foldable || message.collapsible
		}
		view.transcript.Append(block)
	}
	// The accelerator is advertised for the room ON SCREEN (13.10, and app.go's
	// own note on the fold): a task room's rows are the ones a reader can act on
	// while they are in it.
	a.foldable = a.foldable || foldable
	if following {
		view.transcript.GotoBottom()
	}
	// WHERE THE PAGE OPENS is decided once, on its first paint, and never again
	// (recordpage.go): a settled record opens on its result card, a running one
	// at its live tail, and every repaint after that leaves the reader exactly
	// where they are.
	a.anchorRecord(view)
	a.shell.Invalidate()
}

// emptyRoomNote is what a task room says before its subtree has journaled
// anything. It states the two facts a reader needs and invents no third: that
// nothing is here YET, and what will be here when it is.
const emptyRoomNote = "nothing journaled here yet — this room fills with what this task and its parts say, as they say it"

// noteBlock is chrome about an absence.
//
// The note used to ride inside the card's body, which drew it at the card's own
// tier and ran it the whole width of the lens. Both are wrong for what it is.
// 5.13 gives the primary tier to SPEECH and the dimmest to chrome, and nobody
// said this — it is the surface admitting it has nothing to show yet, which is
// the same kind of thing as a fold hint. And a line of prose that runs a
// 200-column terminal end to end is a line the eye loses its place returning
// to, which is what a measure is for ([tokens.ProseMeasure]).
//
// It is its own block rather than a second tier inside the card because a
// [blocks.TextBlock] paints its body at one state by design; a block that could
// tier its own lines would be the transcript growing a second markdown
// renderer. One small block is cheaper than that, and it clears with the card
// the moment the room's first real row lands.
type noteBlock struct {
	id    string
	text  string
	style *tokens.Styler

	width    int
	measured bool
	rows     []string
}

var _ blocks.Block = (*noteBlock)(nil)

// ID is the anchor and cache key.
func (b *noteBlock) ID() string { return b.id }

// IsFinalized is always true: a note about an absence has nothing left to do.
func (b *noteBlock) IsFinalized() bool { return true }

// SettledRows is every row.
func (b *noteBlock) SettledRows(width int) int { return len(b.Rows(width)) }

// Version never moves. The note is replaced, never edited: the room either has
// nothing in it or it does not.
func (b *noteBlock) Version() uint64 { return 0 }

// End is completed — the block is not a turn that could have been cut.
func (b *noteBlock) End() blocks.EndState { return blocks.EndCompleted }

// Rows wraps the note at the readable measure, indented to the lens's left edge
// like every other body in the room, and opens with the blank row that separates
// it from the card above (5.13: cards separated by whitespace, not boxes).
func (b *noteBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return b.rows
	}
	measure := width
	if measure > tokens.ProseMeasure {
		measure = tokens.ProseMeasure
	}
	rows := append(b.rows[:0], "")
	rows = prose{style: b.style, base: tokens.TextTertiary}.rows(rows, b.text, measure, bodyIndent)
	b.rows, b.width, b.measured = rows, width, true
	return b.rows
}

// switchRoom is the thread switcher (12.1.3, 5.24's visible door).
//
// A room switch resets the conversation because it IS a different conversation:
// the transcript, the read watermark and the journal claim all belong to the
// room they were read for, and carrying any of them across would put one room's
// tail in another room's window.
func (a *App) switchRoom(session string) {
	session = strings.TrimSpace(session)
	if session == "" || session == a.session {
		return
	}
	a.session = session
	a.transcript.Reset()
	a.watermark = 0
	a.journal = 0
	a.turn = liveTurn{}
	a.status.session = session
	a.status.turns = 0
	a.source.SetSession(session)
	a.source.refresh(a.journal, true)
	a.railModel.Refresh()
	a.railModel.SelectID(rowRoomPrefix + session)
	a.showThread()
}

// openRoomCmd opens an empty room and switches to it.
//
// It goes through store.OpenSession because that is the ONE door that makes a
// room exist before anything has been said in it (12.1.3 item 3). A switcher
// that "created" a room by pointing the window at a fresh id would be showing an
// empty transcript for a room the store has never heard of, and the room would
// vanish the moment the window closed.
//
// A backend that can tell an empty room from a conversation ([RoomReuser]) is
// asked first, and hands back the empty unnamed room already standing rather
// than minting a second one beside it. That is not a weaker "new": an empty room
// has nothing in it to be older than a fresh one, and two of them are the same
// row printed twice — which is the whole of how five "untitled room" rows came
// to exist (rail-rooms grooming A2).
func (a *App) openRoomCmd() tea.Cmd {
	if a.source == nil || a.source.rooms == nil {
		a.status.err = "this window cannot open rooms"
		return nil
	}
	rooms := a.source.rooms
	id := newRoomID(a.now())
	// THE GAP OPENS HERE, so it is declared here and nowhere else. Every door
	// that mints a room comes through this function — the rail row, the palette,
	// the switcher, and now the first letter typed at the card — and between this
	// return and [App.applyRoomOpened] the window is still standing in the OLD
	// room. A send that arrives in that gap would land there, which is the
	// incident this whole lane is about, one race further down. See [mintHold].
	a.mint.open()
	return func() tea.Msg {
		if reuser, ok := rooms.(RoomReuser); ok {
			opened, _, err := reuser.OpenOrReuseSession(id, "tui")
			return roomOpenedMsg{session: opened, err: err}
		}
		opened, err := rooms.OpenSession(id, "", "tui")
		return roomOpenedMsg{session: opened, err: err}
	}
}

// mintHold is a room being made, and the words said while it was being made.
//
// A MINT IS ASYNCHRONOUS AND A KEYSTROKE IS NOT. [App.openRoomCmd] hands Bubble
// Tea a command and the store answers on another goroutine, so there is a window
// — one frame, usually, and no promise of that — in which the reader believes
// they are in the new room, the composer takes their draft, and [App.session] is
// still the old room. The DRAFT itself survives that window without help: nothing
// on the way through [App.switchRoom] clears the composer's buffer, so the words
// are simply still there when the new room arrives. A SEND does not survive it,
// because a send reads the session, so a send that arrives early is HELD here and
// performed when the room lands.
//
// It holds [composer.Send] rather than text so the two send doors — a plain draft
// and one carrying attachments — are one queue. Holding only strings would have
// dropped the pictures.
type mintHold struct {
	active bool
	held   []composer.Send
}

// open marks a mint in flight.
func (m *mintHold) open() { m.active = true }

// take keeps a send until the room lands, and reports whether it did. A window
// with no mint in flight holds nothing and the send goes out at once, which is
// every send this product has ever made except the ones inside the gap.
func (m *mintHold) take(send composer.Send) bool {
	if !m.active {
		return false
	}
	m.held = append(m.held, send)
	return true
}

// drain closes the gap and hands back what was said inside it.
func (m *mintHold) drain() []composer.Send {
	m.active = false
	held := m.held
	m.held = nil
	return held
}

// roomOpenedMsg is one minted room, folded in on the render goroutine.
type roomOpenedMsg struct {
	session store.Session
	err     error
}

// applyRoomOpened lands a minted room and moves the window into it.
//
// It is also where the gap closes, and the ORDER inside it is the whole of the
// ordering design: the window is moved into the new room FIRST, and only then are
// the held sends performed — so a draft the reader submitted while the store was
// still working posts into the room they were typing to, not the one they were
// standing in. The reverse order would be the incident with an extra step.
func (a *App) applyRoomOpened(msg roomOpenedMsg) tea.Cmd {
	held := a.mint.drain()
	if msg.err != nil {
		a.status.err = msg.err.Error()
		// THE WORDS COME BACK. A mint that failed leaves the window in the old
		// room, and performing the held draft there is exactly what was refused a
		// moment ago — so the sentence goes back into the draft instead, where the
		// reader can read the failure beside it and decide. It is [App.failSend]'s
		// own rule: a notice whose words have been destroyed is worse than none.
		a.restoreHeld(held)
		a.shell.Invalidate()
		return nil
	}
	a.status.err = ""
	a.switchRoom(msg.session.ID)
	// A minted room is a room to speak in, so the keyboard goes back to the
	// composer and — in a narrow frame — the main pane goes back to the
	// conversation it just opened.
	scope := a.setScope(false)
	a.refresh()
	cmds := make([]tea.Cmd, 0, len(held)+2)
	cmds = append(cmds, scope, a.startPoll())
	for _, send := range held {
		cmds = append(cmds, a.submitSend(send))
	}
	return tea.Batch(cmds...)
}

// restoreHeld puts words that were never sent back into the draft.
//
// The composer refuses if the reader has already started typing something else
// ([composer.Model.Restore]), which is the right refusal: their new sentence
// outranks the one the store lost.
func (a *App) restoreHeld(held []composer.Send) {
	texts := make([]string, 0, len(held))
	for _, send := range held {
		if text := strings.TrimSpace(send.Text); text != "" {
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		return
	}
	if r, ok := a.composer.(interface{ Restore(string) bool }); ok {
		r.Restore(strings.Join(texts, "\n"))
	}
}

// newRoomID mints an id for a new room. It is never drawn — 13.3.4 is the rule
// that a session id does not reach a cell — so it only has to be unique and
// sortable, which a timestamp with the window's own clock already is.
func newRoomID(at time.Time) string { return "chat-" + at.Format("20060102-150405.000000") }

// -- the node room's feed ----------------------------------------------------

// subtreeReadMsg is one entered job's full plan, folded or not.
type subtreeReadMsg struct {
	node  string
	nodes []store.Node
}

// readSubtreeCmd reads the plan of the job a reader has just entered.
//
// It is the answer to "I still don't see any tree hierarchy when I click on a
// task", and the emphasis is on CLICK: the read happens on entry and nowhere
// else. See [Subtrees] for why the board's own snapshot cannot answer — folding
// is what happens to every job shortly after it settles, and a folded job's
// parts are not in it.
func (a *App) readSubtreeCmd(node string) tea.Cmd {
	if a.source == nil || a.source.subtrees == nil || node == "" {
		return nil
	}
	subtrees := a.source.subtrees
	return func() tea.Msg {
		nodes, err := subtrees.SubtreeNodes(node)
		if err != nil {
			return subtreeReadMsg{node: node}
		}
		return subtreeReadMsg{node: node, nodes: nodes}
	}
}

// applySubtreeRead puts the plan back on the board and redraws everything built
// from it.
//
// It rebuilds the SCOPE and not only the room, because the card on the rail is
// the same fact seen from one column over: a room that had drawn four parts
// beside a card still saying `atomic` would be 12.14's finding 1 exactly — an
// entered room and its own preview disagreeing about the work.
func (a *App) applySubtreeRead(msg subtreeReadMsg) {
	if a.source == nil || !a.source.rememberSubtree(msg.node, msg.nodes) {
		return
	}
	a.source.refresh(a.journal, true)
	a.railModel.Refresh()
	if a.view != nil && a.view.kind == viewNode && a.view.node == msg.node {
		// The stamp fingerprints the record, and the record just gained rows the
		// journal position cannot see. Clearing it makes the next paint
		// unconditional and the one after it cheap again.
		a.view.stamp = ""
		a.paintRoom()
	}
	a.refresh()
	a.shell.Invalidate()
}

// nodeMessagesMsg is one read of a task room's trail.
//
// read is the highest sequence the gather actually looked at, which is not the
// highest it kept: rows it drops still have to move the watermark or the next
// poll fetches them again forever.
type nodeMessagesMsg struct {
	node     string
	messages []store.Message
	read     int64
	err      error
}

// readNodeCmd reads a task room's trail after a watermark, off the render
// goroutine.
//
// IT READS THE SUBTREE AND NOT THE ROOT. A planned job's root node usually says
// nothing at all — its parts do the work and its workers do the talking — so a
// room that read only the root drew a title, a status line and nothing else,
// which is the shape of a room with no conversation in it rather than the shape
// of a job with three workers in it. 4.6 says a task room IS a view over the
// same journal filtered to one node; the node it is filtered to is the SUBTREE
// that node roots, because that is what "this task" means to a reader.
//
// One command, N reads, on the cycles the journal moved and only while the room
// is open. The fan-out is bounded by the scope the rail already built, which is
// itself capped at maxSubtreeRows, so this cannot grow with the graph. Merging
// happens here, in sequence order, because the journal's own numbering is the
// only ordering that means anything across nodes.
func (a *App) readNodeCmd(node string, after int64) tea.Cmd {
	if a.source == nil || a.source.graph == nil || node == "" {
		return nil
	}
	graph := a.source.graph
	nodes := a.source.subtreeNodes(node)
	return func() tea.Msg {
		var merged []store.Message
		var read int64
		for _, id := range nodes {
			messages, err := graph.NodeMessages(id, after, messagePage)
			if err != nil {
				return nodeMessagesMsg{node: node, err: err}
			}
			for _, message := range messages {
				if message.Seq > read {
					read = message.Seq
				}
				// The one class of row this gather must drop. It reads N nodes,
				// and a steer is written once per node, so a person who typed
				// "also make sure it uses metric units" once into a job with four
				// workers mid-turn opened that job and found their own sentence in
				// it four times over — consecutively, under their own name, each
				// copy prefixed with a word for a thing they have never heard of.
				//
				// One saying is not four events, and their words are already in
				// the room they typed them in. What belongs here is what the steer
				// DID, which arrives on its own as the settlement filed on the
				// card. The watermark still advances past these rows: they are
				// read and discarded, never re-read.
				if mailboxCopy(message) {
					continue
				}
				merged = append(merged, message)
			}
		}
		sort.SliceStable(merged, func(i, j int) bool { return merged[i].Seq < merged[j].Seq })
		return nodeMessagesMsg{node: node, messages: merged, read: read}
	}
}

// applyNodeMessages folds a task room's rows into its own transcript.
//
// It is the same absorb the conversation does, minus the live turn: a task room
// has no streamed preview of its own in v1, because the one head streams into
// the room it is answering and that room is the conversation.
func (a *App) applyNodeMessages(msg nodeMessagesMsg) {
	if msg.err != nil {
		a.status.err = msg.err.Error()
		a.shell.Invalidate()
		return
	}
	if a.view == nil || a.view.kind != viewNode || a.view.node != msg.node {
		return
	}
	if msg.read > a.view.watermark {
		a.view.watermark = msg.read
	}
	for i := range msg.messages {
		message := msg.messages[i]
		if message.Seq > a.view.watermark {
			a.view.watermark = message.Seq
		}
		sanitizeMessage(&message)
		a.view.absorb(message)
	}
	a.paintRoom()
}

// absorb keeps one journaled row in the room's own record, in sequence order and
// without duplicates.
//
// The dedup is against the trail rather than against the transcript, because the
// transcript is now a rendering of the trail and not the place it is kept: a
// steer that landed through applySteer and then came back on the next poll is one
// row that arrived twice, and the record has to hold it once.
func (v *mainView) absorb(message store.Message) {
	at := sort.Search(len(v.messages), func(i int) bool {
		return v.messages[i].Seq >= message.Seq
	})
	if at < len(v.messages) && v.messages[at].Seq == message.Seq {
		return
	}
	v.messages = append(v.messages, store.Message{})
	copy(v.messages[at+1:], v.messages[at:])
	v.messages[at] = message
}

// mailboxCopy reports whether one row is a worker's copy of a steer rather than
// something said in a room.
//
// A redirection has no shared mailbox to go in: the steering poll reads a node's
// own messages, so the broadcast writes one node-anchored copy per worker still
// mid-turn, session-less on purpose so no room draws it (resident's
// BroadcastRedirection, thread.Record). That is delivery, and it is the whole
// reason those three fields sit together this way — nothing a person types
// arrives session-less. An amendment aimed at one node carries the room it was
// typed in, and so does a steer typed at a node directly; both stay visible.
func mailboxCopy(message store.Message) bool {
	return message.Role == store.RoleUser && message.NodeID != "" && message.SessionID == ""
}

// -- the preview card --------------------------------------------------------

// cardBlock draws one rail row as a settled transcript block: the 5.9 anatomy,
// at transcript width, with the affordance line under it when there is one.
//
// It reads nothing. Every cell comes from the row the rail handed over, which is
// what makes a cursor move cost a block instead of a query.
func cardBlock(row rail.Row, note string, style *tokens.Styler) blocks.Block {
	head := blocks.Header{
		Title: row.Name,
		Glyph: row.Attention().Glyph(),
		State: blocks.StateSettled,
	}
	if row.Life.Terminal() {
		head.State = blocks.StateSettled
	}
	block := blocks.NewText("scope-card", head)
	block.Styler = style
	block.BodyState = blocks.StateSettled
	// The card's words start where every other body in the room starts: column
	// 0 is the gutter the glyph hangs in, and 5.13's two cells are where speech
	// begins. Without this the card was the one block whose body ran flush to
	// the left edge, so a preview and the room it previews disagreed about
	// their own left margin.
	block.BodyIndent = blocks.BodyIndent

	lines := make([]string, 0, 4)
	if status := strings.TrimSpace(row.Status); status != "" {
		lines = append(lines, status)
	}
	if len(row.WaitsOn) > 0 {
		lines = append(lines, waitsWord+strings.Join(row.WaitsOn, ", "))
	}
	if cells := cardTelemetry(row); cells != "" {
		lines = append(lines, cells)
	}
	if !row.Artifact.Empty() {
		lines = append(lines, row.Artifact.Path)
	}
	if note != "" {
		lines = append(lines, note)
	}
	if len(lines) == 0 {
		lines = append(lines, "nothing to report yet")
	}
	block.Write(strings.Join(lines, "\n"))
	block.Finalize(blocks.EndCompleted)
	return block
}

// cardTelemetry is line 3 of the card (5.9), with money always present: an
// absent cost renders as the missing glyph rather than as zero, because 10.2.8
// is explicit that a number which has not arrived and a number that is zero are
// different facts.
func cardTelemetry(row rail.Row) string {
	cells := make([]string, 0, 4)
	if row.Meta.Model != "" {
		cells = append(cells, row.Meta.Model)
	}
	if row.Meta.HasCost {
		cells = append(cells, tokens.Money(row.Meta.Cost))
	} else {
		cells = append(cells, "$"+tokens.GlyphMissing)
	}
	if row.Meta.HasElapsed {
		cells = append(cells, tokens.Elapsed(row.Meta.Elapsed))
	}
	// NO SHAPE CELL. It used to end `· atomic` or `· 3 parts`, off Telemetry
	// fields the rail retired — so the read was dead as well as wrong. Both
	// words are §14's banned worker-count phrasing ("a single-part job says
	// nothing about its shape"), and the census the card already carries says
	// the multiplicity that is worth saying (§15: never count what is visible).
	return strings.Join(cells, " "+tokens.GlyphSeparator+" ")
}

// -- the fresh room's empty state --------------------------------------------

// The `+ new` door's card, in the words a person standing in front of an empty
// room actually needs.
//
// THE COPY IS THREE FACTS AND NOTHING ELSE, in the order the reader needs them:
// what this is, that it will name itself, and how to go in. Everything the row
// dump used to carry — the lifecycle glyph, the `$—`, the `+` from the door's own
// label — is a fact about a room that does not exist yet, which is to say not a
// fact.
//
// THE TWO WAYS IN ARE BOTH REAL, and that is the whole reason there are two. The
// card used to name one key and the surface honoured only that key; a person who
// took the other way — believing the card meant they had arrived, and typing —
// was speaking into the room they had left. Both rows below are live doors now.
const (
	// newRoomCardTitle is the ruled word at the top: the THING, not the door.
	// §16's CASE keeps it lowercase, like every other section word on the surface.
	newRoomCardTitle = "a fresh room"
	// newRoomCardNote is the one paragraph. It says what a room is, because the
	// product's unit of work is a conversation and this is the frame where that
	// is worth one sentence, and it promises the naming rather than leaving a
	// reader to discover that `untitled room` is temporary.
	newRoomCardNote = "a conversation of its own, with no history behind it. " +
		"it takes its name from the first exchange in it."
	// The two ways in. The verbs are the acts and the keys are annotation
	// (§16's verb·key chip, drawn through internal/tui2/keychip), and the notes
	// behind them say what each way actually does with the words in the draft —
	// which is the difference the reader was never told and paid for.
	newRoomTypeVerb = "start typing"
	newRoomTypeNote = "the room is made and takes the draft"
	newRoomOpenVerb = "open"
	newRoomOpenKey  = "enter"
	newRoomOpenNote = "arrive with nothing said"
)

// newRoomCardBlock is that card as a transcript block.
//
// It is its own type for [threadBreakBlock]'s reason: a [blocks.TextBlock] paints
// one body at one state, and this card is a composed frame — a titled rule, a
// paragraph at the readable measure, and two two-tier chip rows — whose parts sit
// at three different tiers and shed independently under width pressure.
type newRoomCardBlock struct {
	style *tokens.Styler

	width    int
	measured bool
	rows     []string
}

var _ blocks.Block = (*newRoomCardBlock)(nil)

// ID is the anchor and cache key. It is a constant because a transcript holds
// exactly one: the card is the whole of this lens.
func (b *newRoomCardBlock) ID() string { return "new-room-card" }

// IsFinalized is always true: an empty state has nothing left to do.
func (b *newRoomCardBlock) IsFinalized() bool { return true }

// SettledRows is every row.
func (b *newRoomCardBlock) SettledRows(width int) int { return len(b.Rows(width)) }

// Version never moves. The card is rebuilt, never edited.
func (b *newRoomCardBlock) Version() uint64 { return 0 }

// End is completed — a card is not a turn that could have been cut.
func (b *newRoomCardBlock) End() blocks.EndState { return blocks.EndCompleted }

// Rows lays the card out at width.
//
// THE HIERARCHY IS TYPOGRAPHIC AND THERE IS NO BOX. Four bands separated by
// blank rows, which is §16's answer to separation and the same rhythm the thread
// break and the taught empty state already keep: the titled rule, the paragraph
// at [tokens.ProseMeasure] so a wide terminal does not run prose to column 200,
// a blank, and the two ways in. Under width pressure each band degrades on its
// own — the rule falls back to a bare hairline ([blocks.Ruled]), the paragraph
// rewraps, and the notes behind the chips leave as one column so two doors are
// never taught in two different formats.
func (b *newRoomCardBlock) Rows(width int) []string {
	if width < 1 {
		width = 1
	}
	if b.measured && b.width == width {
		return b.rows
	}
	measure := width
	if measure > tokens.ProseMeasure {
		measure = tokens.ProseMeasure
	}
	rows := append(b.rows[:0], "")
	rows = append(rows, blocks.Ruled{Title: newRoomCardTitle, State: blocks.StateChrome}.
		Render(width, b.styler()))
	rows = append(rows, "")
	rows = prose{style: b.style, base: tokens.TextTertiary}.rows(rows, newRoomCardNote, measure, bodyIndent)
	rows = append(rows, "")

	ways := []registry.Chip{
		registry.ChipFor(newRoomTypeVerb, ""),
		registry.ChipFor(newRoomOpenVerb, newRoomOpenKey),
	}
	notes := []string{newRoomTypeNote, newRoomOpenNote}
	// The notes are ONE COLUMN and they leave as one, exactly as the taught empty
	// state's descriptions do: dropping only the row whose sentence happened not
	// to fit would teach two doors in two formats and leave the reader deciding
	// what the difference meant.
	described := true
	for i := range ways {
		if bodyIndent+keychip.Width(ways[i:i+1])+blocks.Width(cardNoteTail(notes[i])) > width {
			described = false
			break
		}
	}
	for i := range ways {
		note := ""
		if described {
			note = notes[i]
		}
		rows = append(rows, b.wayRow(ways[i], note, width))
	}
	b.rows, b.width, b.measured = rows, width, true
	return b.rows
}

// cardNoteTail is a note behind the telemetry separator (5.17), which is how it
// is measured as well as how it is drawn.
func cardNoteTail(note string) string {
	if note == "" {
		return ""
	}
	return " " + tokens.GlyphSeparator + " " + note
}

// wayRow draws one way in: the verb·key chip at the body's own left edge, and —
// when the column fits — what that way does behind the separator. The chip is
// only ever cut, never dropped: a row that named no verb would name no door.
func (b *newRoomCardBlock) wayRow(chip registry.Chip, note string, width int) string {
	room := width - bodyIndent
	if room < 1 {
		return ""
	}
	var line strings.Builder
	line.WriteString(strings.Repeat(" ", bodyIndent))
	for _, span := range keychip.Of(chip, tokens.TextSecondary) {
		text := blocks.Truncate(span.Text, room)
		line.WriteString(b.paint(text, tokens.Token(span.Tok)))
		room -= blocks.Width(text)
		if room < 1 {
			return line.String()
		}
	}
	if tail := cardNoteTail(note); tail != "" {
		line.WriteString(b.paint(blocks.Truncate(tail, room), tokens.TextTertiary))
	}
	return line.String()
}

// paint draws one span, or returns it unchanged for a block built without a
// profile — the golden harness and every headless test.
func (b *newRoomCardBlock) paint(text string, tier tokens.Token) string {
	if b.style == nil || text == "" {
		return text
	}
	return b.style.PaintToken(text, tier)
}

func (b *newRoomCardBlock) styler() blocks.Styler {
	if b.style == nil {
		return nil
	}
	return b.style
}

// -- the steered draft -------------------------------------------------------

// submit sends one draft to whatever the composer is bound to (5.15).
//
// It is a plain draft — no attachments — expressed as the one send [submitSend]
// already routes, rather than as a second copy of that routing. The two used to
// be written out separately and they had already grown apart once: only one of
// them carried pictures. One door, read off the binding rather than off the
// screen, because the binding is what the prompt glyph promised — a `↦` row's
// draft becomes steering mail anchored to its node and a `›` row's becomes a turn
// in the room. A disabled composer never gets here: it does not take the
// keyboard, so it has no draft to submit.
func (a *App) submit(text string) tea.Cmd {
	// A new attempt ends the last one's failure, whatever becomes of this one.
	// The state is about the most recent send and nothing else, so it is cleared
	// where the next send begins rather than on a timer somebody has to tune.
	a.clearSendFailure()
	return a.submitSend(composer.Send{Text: text})
}

// steerNode is the same door, aimed by the caller rather than by the binding.
// The `@` grammar needs it (5.18): a mention addresses a task the composer is
// not bound to, and the alternative — rebinding the composer to send — would
// teleport the reader's context, which is the one thing 5.18 refuses.
//
// Attachments ride here too, on the same rule postCmd states (attach.go): a
// person steering a worker with a screenshot is showing it the thing they mean,
// and a door that dropped the picture would make the steer line the one surface
// where a file cannot be shown.
func (a *App) steerNode(node, text string, attachments ...composer.Attachment) tea.Cmd {
	text = strings.TrimSpace(text)
	node = strings.TrimSpace(node)
	if text == "" {
		text = attachmentBody(attachments)
	}
	if text == "" || node == "" || a.backend == nil {
		return nil
	}
	backend := a.backend
	message := store.Message{
		SessionID: a.session,
		Role:      store.RoleUser,
		Body:      text,
		NodeID:    node,
	}
	keeper, _ := a.commander.(AttachmentKeeper)
	files := append([]composer.Attachment(nil), attachments...)
	return func() tea.Msg {
		message.Attachments = keepAttachments(keeper, files)
		posted, err := thread.Post(backend, message)
		if err != nil {
			// The outgoing row, for the reason postCmd states: a refused write
			// answers with nothing, and the words are what the reader needs back.
			return steerResultMsg{message: message, err: err}
		}
		return steerResultMsg{message: posted, err: err}
	}
}

// steerResultMsg is one journaled steer.
type steerResultMsg struct {
	message store.Message
	err     error
}

// applySteer lands the steer in the room it was aimed at (5.20 rule 4: no
// dead-air sends). It never opens a turn: steering mail is absorbed between a
// worker's turns, not answered, and an awaiting line over it would promise a
// reply that is not coming.
func (a *App) applySteer(result steerResultMsg) {
	if result.err != nil {
		// A steer that did not land is a failed SEND, not a dead store: the
		// journal is fine, this one write was refused. It says so where the
		// reader is looking — the prompt and the middle zone — and gives the
		// words back (§7).
		a.failSend(result.message.Body, result.err)
		a.refresh()
		return
	}
	a.status.err = ""
	if a.view != nil && a.view.kind == viewNode && a.view.node == result.message.NodeID {
		message := result.message
		sanitizeMessage(&message)
		a.view.absorb(message)
		if result.message.Seq > a.view.watermark {
			a.view.watermark = result.message.Seq
		}
		a.paintRoom()
	}
	a.refresh()
}

// -- the homes (5.24) ----------------------------------------------------------

// selfRowPrefix is what the self room's route rows are keyed under. It is
// spelled here rather than exported from internal/tui2/homes because the routes
// are that package's own vocabulary and this side only needs to recognize one
// prefix; a constant it exported would be a second name for one string.
const selfRowPrefix = "self/"

// showHome points the main pane at one of the four rooms.
//
// It is the third sibling of showThread and showCard, and it is a pane swap
// rather than a mode: the homes View draws its own rows, so what changes is
// which renderer the main pane is holding and nothing about how the surface
// behaves. Every key that worked on a task scope works here unchanged, which is
// the whole point of the homes being rail scopes rather than pages (5.24).
func (a *App) showHome(sel homes.Selection) {
	a.homesSel = sel
	a.view = &mainView{kind: viewHome, title: sel.Home.Word()}
	a.pane.transcript = nil
	a.pane.homes = a.homesPane
	a.shell.Invalidate()
}

// scopeHome is which home the rail is currently inside, for a member row that
// could belong to more than one of them.
func (a *App) scopeHome() homes.Home {
	if a.railModel == nil {
		return a.homesSel.Home
	}
	if home, ok := homes.ParseScopeID(a.railModel.Scope().ID); ok {
		return home
	}
	return a.homesSel.Home
}

// toggleHomes opens or closes 5.24's collapsed group and rebuilds the rail
// around it.
//
// The lid's state belongs to this side rather than to internal/tui2/homes,
// exactly as that package's State field says: it is the RAIL's state, and who
// remembers it is the model that owns the cursor.
func (a *App) toggleHomes() tea.Cmd {
	if a.source == nil || a.railModel == nil {
		return nil
	}
	a.source.homes.Expanded = !a.source.homes.Expanded
	a.source.refresh(a.journal, true)
	a.railModel.Refresh()
	a.refresh()
	return nil
}

// renderHomes draws the selected home's detail pane.
//
// The View's returned slice aliases its own buffer and is valid only until the
// next Render, so the rows are copied — the same price the bounded HUD pays for
// the same reason (app.go's hudRows).
func (a *App) renderHomes(width, height int) []string {
	if a.homesView == nil || a.source == nil || width <= 0 || height <= 0 {
		return nil
	}
	rows := a.homesView.Render(a.source.homes, a.homesSel, width, height)
	out := make([]string, len(rows))
	copy(out, rows)
	return out
}

// refreshHomes keeps the homes' facts current on the cycles the journal moved.
//
// The NOTEBOOK's three sections are read here through [App.fillNotebook], which
// is where the whole of that decision lives: what it reads, how often, and why a
// lens nobody is looking at does not pay for it. The charters and the service
// table are still empty and still deliberately so — they belong to the WORK page
// after this wave's split (notebook-split.md §1), and the room that draws them
// renders an empty list as its own teaching line, so an unwired room and a
// genuinely empty one show the same true thing.
//
// Visitor is wired because it is 5.24's multi-window rule and it disables every
// verb at once: a window that cannot act must not offer to.
func (a *App) refreshHomes() {
	if a.source == nil {
		return
	}
	a.source.homes.Now = a.now()
	a.source.homes.Visitor = ""
	if a.residency.Visitor {
		a.source.homes.Visitor = "visitor window — only the resident may act"
	}
	a.source.homeSource.SetState(a.source.homes)
	a.fillNotebook(false)
}
