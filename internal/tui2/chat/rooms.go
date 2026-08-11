package chat

import (
	"image"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
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
//     rests on is what the main pane shows and what the composer binds.
//   - SELECTION PREVIEWS, ENTER COMMITS (5.15's refinement, for cost). Moving
//     the cursor costs a card — one block, built from the row already in hand,
//     with no store read at all. Enter is what pays for a room: the node trail
//     is read then, once, and polled from then on.
//   - ESC POPS SCOPE, never just selection. At home it hands the key back to
//     8.2.21's ladder, which is the app's existing esc chain.
//   - THE AFFORDANCE NEVER LIES (5.20, 5.15's one rule). A row whose surface
//     this window is not showing does not get a live composer; it gets a
//     disabled one that says which key opens it. A settled row says it is
//     settled. The prompt glyph previews the composer the row bound, and the
//     card's mark previewed the same glyph, so the two cannot disagree.

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
	keys func(tea.KeyPressMsg) (tea.Cmd, bool)
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
	a.source.refresh(0, true)
	a.railModel = rail.New(a.source)
	a.hudModel = rail.New(hudSource{a.source})
	a.railView = rail.NewView(a.style)
	a.hudView = rail.NewView(a.style)
	a.scope = &scopePane{
		pane:  rail.Pane{Model: a.railModel, View: a.railView},
		style: a.style,
		mode:  a.railMode,
		keys:  a.scopeKey,
		point: a.scopePoint,
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
			// the conversation.
			a.focusScope(false)
			return nil, true
		}
		return a.applyScope(event), true
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		return a.applyScope(a.railModel.Select(int(key[0] - '1'))), true
	}
	return nil, false
}

// scopePoint is the map's pointer, and it is the keyboard's grammar reached by
// a different hand. Every branch below ends in a call scopeKey also makes.
//
// The one decision this function makes on its own is what a SECOND click means,
// and it is taken from the enter law rather than invented: 5.15 splits select
// from open — "selection previews; enter opens" — so a click on a row the
// cursor is not on previews it, and a click on the row the cursor is already on
// is the enter. That reads as a double-click to a hand and as the documented
// law to a reader, which is the good case of a convention agreeing with a rule.
// It also means a pointer can never commit to a room the reader has not first
// seen the preview of — one click, one preview, one more click, one room.
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

	// Pointing at the map is talking to the map: the keyboard comes with the
	// pointer, so the next j or enter lands where the eye already is. This is
	// the shell's focus rule (a click focuses what it hit) stated in the app's
	// own terms, because the rail's focus is the app's flag and not the shell's.
	second := a.railFocus && a.railModel.Cursor() == pt.row
	a.focusScope(true)
	if !second {
		return a.applyScope(a.railModel.Select(pt.row))
	}
	if a.railModel.Selected().ID == homes.GroupRowID {
		return a.toggleHomes()
	}
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
// commit separates the two halves of 5.15's select/open split: a preview binds
// the composer and draws a card, and only a commitment pays for a room.
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
		a.showCard(row, "enter opens a fresh room")
		a.composerBind = composerBind{mode: rail.ComposerDisabled,
			note: "press enter to start a new room"}

	case id == homes.GroupRowID:
		a.showCard(row, "enter opens the rest of aforge")
		a.composerBind = composerBind{mode: rail.ComposerDisabled,
			note: "press enter to open these rooms"}

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
		a.composerBind = composerBind{mode: rail.ComposerDisabled,
			note: "press enter to open this room"}

	case strings.HasPrefix(id, rowTaskPrefix):
		node := strings.TrimPrefix(id, rowTaskPrefix)
		if commit {
			cmd = a.openTaskRoom(row, node)
		} else {
			a.showCard(row, "")
		}
		a.composerBind = bindWork(row, node)

	default:
		a.showCard(row, "")
		a.composerBind = composerBind{mode: rail.ComposerDisabled, note: "nothing to say here"}
	}

	a.status.breadcrumb = a.breadcrumb()
	a.handOverTheKeyboard(commit)
	a.refresh()
	return cmd
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
//   - A DISABLED composer keeps the keyboard on the map. `+ new room`, the 5.24
//     group lid and a settled service take no draft, so handing them the
//     keyboard would move it to a pane that refuses every key — the same
//     invisible dead end in the other direction.
func (a *App) handOverTheKeyboard(commit bool) {
	if !commit || !a.railFocus || a.composerBind.mode == rail.ComposerDisabled {
		return
	}
	a.focusScope(false)
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

// breadcrumb is the spatial truth (5.15): where the reader is, in the words
// they navigated by, and never an id (5.14, 13.3.4).
func (a *App) breadcrumb() string {
	if a.railModel == nil {
		return ""
	}
	crumbs := a.railModel.Breadcrumb()
	if a.view != nil && a.view.title != "" {
		crumbs = append(crumbs, a.view.title)
	}
	if len(crumbs) <= 1 {
		return ""
	}
	return strings.Join(crumbs[1:], " "+tokens.GlyphScopeUp+" ")
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
	a.view = &mainView{kind: viewCard, title: row.Name, transcript: transcript}
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
	transcript.Append(cardBlock(row, emptyRoomNote, a.style))
	a.view = &mainView{
		kind: viewNode, node: node, title: row.Name,
		transcript: transcript, teaching: true,
	}
	a.pane.homes = nil
	a.pane.transcript = transcript
	a.shell.Invalidate()
	return a.readNodeCmd(node, 0)
}

// emptyRoomNote is what a task room says before its subtree has journaled
// anything. It states the two facts a reader needs and invents no third: that
// nothing is here YET, and what will be here when it is.
const emptyRoomNote = "nothing journaled here yet — this room fills with what this task and its parts say, as they say it"

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

// openRoomCmd mints an empty room and switches to it.
//
// It goes through store.OpenSession because that is the ONE door that makes a
// room exist before anything has been said in it (12.1.3 item 3). A switcher
// that "created" a room by pointing the window at a fresh id would be showing an
// empty transcript for a room the store has never heard of, and the room would
// vanish the moment the window closed.
func (a *App) openRoomCmd() tea.Cmd {
	if a.source == nil || a.source.rooms == nil {
		a.status.err = "this window cannot open rooms"
		return nil
	}
	rooms := a.source.rooms
	id := newRoomID(a.now())
	return func() tea.Msg {
		opened, err := rooms.OpenSession(id, "", "tui")
		return roomOpenedMsg{session: opened, err: err}
	}
}

// roomOpenedMsg is one minted room, folded in on the render goroutine.
type roomOpenedMsg struct {
	session store.Session
	err     error
}

// applyRoomOpened lands a minted room and moves the window into it.
func (a *App) applyRoomOpened(msg roomOpenedMsg) tea.Cmd {
	if msg.err != nil {
		a.status.err = msg.err.Error()
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
	return tea.Batch(scope, a.startPoll())
}

// newRoomID mints an id for a new room. It is never drawn — 13.3.4 is the rule
// that a session id does not reach a cell — so it only has to be unique and
// sortable, which a timestamp with the window's own clock already is.
func newRoomID(at time.Time) string { return "chat-" + at.Format("20060102-150405.000000") }

// -- the node room's feed ----------------------------------------------------

// nodeMessagesMsg is one read of a task room's trail.
type nodeMessagesMsg struct {
	node     string
	messages []store.Message
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
		for _, id := range nodes {
			messages, err := graph.NodeMessages(id, after, messagePage)
			if err != nil {
				return nodeMessagesMsg{node: node, err: err}
			}
			merged = append(merged, messages...)
		}
		sort.SliceStable(merged, func(i, j int) bool { return merged[i].Seq < merged[j].Seq })
		return nodeMessagesMsg{node: node, messages: merged}
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
	appended := false
	for i := range msg.messages {
		message := msg.messages[i]
		if message.Seq > a.view.watermark {
			a.view.watermark = message.Seq
		}
		if _, exists := a.view.transcript.IndexOf(messageID(message.Seq)); exists {
			continue
		}
		if a.view.teaching {
			// The first real row retires the teaching card. Truncate rather
			// than replace: the card is the only block in the room at this
			// point, and a room that kept it above its first journaled line
			// would be saying "nothing here yet" over the thing that arrived.
			a.view.transcript.Truncate(0)
			a.view.teaching = false
		}
		sanitizeMessage(&message)
		block := newMessageBlock(message, a.style, a.source)
		// The fold's accelerator is offered while the room on screen has
		// something to fold, and a task room's rows are the ones on screen.
		a.foldable = a.foldable || block.collapsible
		a.view.transcript.Append(block)
		appended = true
	}
	if appended {
		a.view.transcript.GotoBottom()
	}
	a.shell.Invalidate()
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

	lines := make([]string, 0, 4)
	if status := strings.TrimSpace(row.Status); status != "" {
		lines = append(lines, status)
	}
	if len(row.WaitsOn) > 0 {
		lines = append(lines, "waits on "+strings.Join(row.WaitsOn, ", "))
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
	switch {
	case row.Meta.Atomic:
		cells = append(cells, "atomic")
	case row.Meta.HasWorkers:
		cells = append(cells, plural(row.Meta.Workers, "part", "parts"))
	}
	return strings.Join(cells, " "+tokens.GlyphSeparator+" ")
}

// -- the steered draft -------------------------------------------------------

// submit sends one draft to whatever the composer is bound to (5.15).
//
// There is exactly one branch and it reads the binding rather than the screen,
// because the binding is what the prompt glyph promised: a `↦` row's draft
// becomes steering mail anchored to its node, and a `›` row's draft becomes a
// turn in the room. A disabled composer never gets here — it does not take the
// keyboard, so it has no draft to submit.
func (a *App) submit(text string) tea.Cmd {
	if a.composerBind.mode == rail.ComposerSteer && a.composerBind.node != "" {
		return a.steerCmd(text)
	}
	return a.postCmd(text)
}

// steerCmd posts a draft to the row the composer is bound to.
//
// A steer is a user message anchored to a node: the same shape the old window's
// steer line posts (internal/tui/node.go), through the same single door, so a
// worker's steering mailbox is one mailbox and not two. Nothing here is a new
// verb — 4.6's v1 promise is that a room's composer uses the verbs that already
// exist, and this is that promise as four fields.
func (a *App) steerCmd(text string) tea.Cmd {
	return a.steerNode(a.composerBind.node, text)
}

// steerNode is the same door, aimed by the caller rather than by the binding.
// The `@` grammar needs it (5.18): a mention addresses a task the composer is
// not bound to, and the alternative — rebinding the composer to send — would
// teleport the reader's context, which is the one thing 5.18 refuses.
func (a *App) steerNode(node, text string) tea.Cmd {
	text = strings.TrimSpace(text)
	node = strings.TrimSpace(node)
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
	return func() tea.Msg {
		posted, err := thread.Post(backend, message)
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
		a.status.err = result.err.Error()
		a.shell.Invalidate()
		return
	}
	a.status.err = ""
	if a.view != nil && a.view.kind == viewNode && a.view.node == result.message.NodeID {
		if _, exists := a.view.transcript.IndexOf(messageID(result.message.Seq)); !exists {
			message := result.message
			sanitizeMessage(&message)
			a.view.transcript.Append(newMessageBlock(message, a.style, a.source))
			a.view.transcript.GotoBottom()
		}
		if result.message.Seq > a.view.watermark {
			a.view.watermark = result.message.Seq
		}
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
// Two of 5.24's fields are wired and the rest are the reads named in
// internal/tui2/homes' state.go, which this surface does not yet make: the
// notebook, the competence map, the charters and the service table each need a
// store read this Backend does not declare. They are left EMPTY rather than
// faked, and the package renders an empty room as its own teaching line — so an
// unwired room and a genuinely empty one show the same true thing, which is the
// property that lets this land in halves.
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
}
