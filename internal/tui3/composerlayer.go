package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE COMPOSER LAYER — the three facts a task needs before it leaves ──────
//
// SCREEN 2e. With something typed into the composer, `alt+enter` no longer sends
// on the spot: it opens a LAYER over whatever place a person is standing on.
//
//	last 14 days · $34.10                                       ← the place, faded
//	▁▂▁▃▅▂▁▁▇▃▂▅▂▁
//	· opus 4.1        execution                          $21.40
//
//	› cut the opus spend in half without losing the sweep
//	 it will run on its own and tell you when it lands            a task
//	 · in ~/aforge-v2, on master                        alt+w to move it
//	 · execution runs on opus 4.1                          alt+o to change
//	 · it may spend up to $10.00 before it asks              type a number
//	 alt+enter send it off · enter talk about it first · esc back to spend
//
// THE PAGE BEHIND DIMS RATHER THAN BEING COVERED, which is the whole reason this
// is a layer and not an eighth place: you never lose your place, the box does not
// move by a single cell, and the three lines appear in the air that was already
// under it. The fade is the depth ladder this surface already has
// (depthfade.go's [palette.fade]) taken at its FAINTEST stop, so nothing new is
// authored to say "behind".
//
// ── THE THREE FACTS, AND WHY THESE THREE ────────────────────────────────────
//
// Where, on what, how much. They are the only three a task needs settled before
// it leaves, and each is edited ON THE LINE THAT SHOWS IT — the same law the
// money segment in the status line already follows, and the reason there is no
// settings page anywhere in this gesture.
//
// Each of them is also a REAL FIELD of the session the door builds, carried on
// [ErrandOrders] and honoured in cmd/aforge's v3Errand: the workspace it works
// in, the model its work runs on (config's execution slot), and the spend rail
// it stops at. A figure a person set and nothing read would be worse than a
// figure they were never offered.
//
// ── AND A KEY IS DRAWN ONLY WHERE IT DOES SOMETHING ─────────────────────────
//
// SCREEN 3a's clause is in force here as everywhere: `alt+w to move it` is
// absent on a machine with one project, because there is nowhere to move it to,
// and the model line is absent when nothing anywhere can say what execution runs
// on. A layer that named three keys and bound two would be the exact defect this
// design exists to avoid.
//
// ── THE ERRAND STILL LANDS ON HOME ──────────────────────────────────────────
//
// Sending from the spend place still CARRIES YOU TO HOME, because home's column
// is the only surface that draws an errand's answer ([app.showExchanges]) and
// work started somewhere a person cannot watch it is the one failure worse than
// saying no ([app.askHere]). Making the exchange a band the frame draws over
// every place is the router's open question and is a wave of its own; until it
// lands, the manual says plainly where you end up (places.md).

// composerCapDefault is the figure the third line opens on, and it is the tank
// the engine actually applies when nobody names one.
//
// IT IS NOW ONE NUMBER AND NOT TWO. This file used to spell the figure itself,
// with a comment asking whoever changed the engine to remember to change the
// surface — and that is precisely the arrangement the one-source-of-truth law
// forbids, because the reminder is only ever read by somebody who already knew.
// [session.DefaultRunCapUSD] is the figure; this name is kept so the rest of
// the layer reads the way it always did.
const composerCapDefault = session.DefaultRunCapUSD

// composerLayer is the layer's whole state. The zero value is closed.
type composerLayer struct {
	open bool
	// at is the place the layer was opened on, so `esc` goes back to THAT place
	// rather than to whichever one a stray key walked to. Nothing can walk while
	// the layer is up — it holds the keyboard — and holding the id anyway is what
	// lets the foot name the place by its own word.
	at page
	// where is the destination `alt+w` last cycled to, and "" is "wherever the
	// plain door would have sent it" ([app.errandPlace]). It is a PATH and not an
	// index into the list, because the list is read again on every frame and a
	// project that appeared between two keystrokes would otherwise shift the
	// cursor's meaning under the person's hand.
	where string
	// model is the execution model `alt+o` chose, and "" is the launch's own
	// binding — which is the ordinary case and the one the line still reads
	// truthfully, because an unpinned execution slot follows the conversation
	// (internal/session's defaultTaskModel).
	model string
	// capText is the digits typed on the third line, and "" is the default. It is
	// the SPELLING and not the figure: a person half way through typing `2.` has
	// something on the screen that is not yet a number, and a float that rounded
	// their keystrokes back at them would be a box that fights the hand.
	capText string
	// places is everywhere this task can be sent, READ ONCE ON THE WAY IN and
	// held. The list is a walk of the projects root ([app.readWorld]) and a walk
	// is a thing a keystroke may do and a draw may not (ARCHITECTURE.md's fourth
	// law) — and holding it is also what makes `alt+w` a cycle rather than a
	// lottery, because a project that appeared between two presses cannot shift
	// what the next one means.
	places []string
	// pick is the model list, opened by `alt+o` over the execution slot. It is
	// THE picker — the same type, the same rows, the same walk the settings panel
	// puts inside its own frame ([picker.rowsOwned]) — because there is one
	// filterable model list on this surface and this is a third door onto it.
	pick picker
}

// composerSlot is the model slot this layer binds: the EXECUTION slot, which is
// what the work a task hands out runs on (config's modelslots.go, whose prefs
// field for it is `task_model`).
const composerSlot = "work"

// ── opening and closing ─────────────────────────────────────────────────────

// openComposerLayer is the first `alt+enter` over a composer with something in
// it. It answers false where there is nothing to send, which is where the chord
// keeps meaning what it always meant.
//
// THE ONE READING OF THE DISK IS ASKED FOR HERE. The layer states which branch
// the destination is on, and that is a `git status` — so it is asked for on this
// keystroke and on `alt+w`'s, never on a draw (ARCHITECTURE.md's fourth law).
// It is ASKED FOR and not waited on: the command runs off the update loop and
// the line says where the task will run with no branch on it until the answer
// lands, which is the emptiness law (homeband_repo.go's [app.refreshRepoOf]).
func (a *app) openComposerLayer() (tea.Cmd, bool) {
	box := a.placeBox()
	if box == nil || strings.TrimSpace(box.String()) == "" {
		return nil, false
	}
	a.closeStrip()
	a.composer = composerLayer{open: true, at: a.page}
	a.composer.places = a.composerDestinations()
	return a.refreshRepoOf(a.composerWhere(), a.now()), true
}

// closeComposerLayer is `esc`: the layer goes and the place under it is exactly
// as it was, sentence included. Nothing here was applied on the way in, so
// nothing has to be put back.
func (a *app) closeComposerLayer() {
	a.composer = composerLayer{}
}

// composerShowing is whether the layer is up. It is asked by the frame, by the
// router and by the pointer, and it is ONE FIELD — a layer with a second flag
// somewhere would be two answers to "is it up" and one of them would be wrong.
func (a *app) composerShowing() bool { return a.composer.open }

// ── the three facts, read ───────────────────────────────────────────────────

// composerWhere is the workspace this task will run in: the destination `alt+w`
// cycled to, and otherwise where it opened.
func (a *app) composerWhere() string {
	if chosen := strings.TrimSpace(a.composer.where); chosen != "" {
		return chosen
	}
	return a.composerOpensAt()
}

// composerOpensAt is where the layer opens: THE SCOPE CHIP'S OWN ANSWER
// ([app.scopeWorkspace]) and, only where this window is standing in no project
// at all, whatever the plain errand door would have given ([app.errandPlace],
// which falls through to the person's home directory).
//
// THE CHIP IS ONE ROW ABOVE THIS LINE AND THEY MAY NOT DISAGREE. `here
// ~/aforge-v2` over `· in ~` is one frame saying a sentence lands in two
// different places, and it is the reading a person acts on — the whole promise
// of the chip is that a verb always in reach always says where it goes.
func (a *app) composerOpensAt() string {
	if where := a.scopeWorkspace(); where != "" {
		return where
	}
	workspace, _ := a.errandPlace()
	return workspace
}

// composerModel is what the EXECUTION slot runs on, down the same ladder the
// engine walks: the model chosen here, then the profile's own binding
// ([config.TaskModelAt] — the slot's prefs field), then the conversation's
// model, which is what an unbound execution slot follows
// (internal/session's defaultTaskModel).
//
// Empty is a window that cannot say, and the line is then absent rather than
// drawn around a gap — the emptiness law, and the reason `alt+o` is unnamed on
// such a window too.
func (a *app) composerModel() string {
	if chosen := strings.TrimSpace(a.composer.model); chosen != "" {
		return chosen
	}
	if bound := strings.TrimSpace(config.TaskModelAt(a.profileDir)); bound != "" {
		return bound
	}
	return strings.TrimSpace(a.model)
}

// composerCapWord is the figure the third line states, in the spelling the
// person is typing when they are typing one.
func (a *app) composerCapWord() string {
	if text := a.composer.capText; text != "" {
		return "$" + text
	}
	return dollars(composerCapDefault)
}

// composerCapUSD is that figure as a number, for the orders the send carries.
//
// A SPELLING THAT IS NOT YET A FIGURE IS THE DEFAULT. `2.` and “ are both a
// hand part way through a decision, and neither is a cap of nothing — sending
// zero would be sending a task nobody bounded, which is the opposite of what the
// line a person just read said.
func (a *app) composerCapUSD() float64 {
	text := strings.TrimSpace(a.composer.capText)
	if text == "" {
		return composerCapDefault
	}
	amount, err := strconv.ParseFloat(text, 64)
	if err != nil || amount <= 0 {
		return composerCapDefault
	}
	return amount
}

// composerPlaces is everywhere this task can be sent, as the layer read it on
// the way in. A draw asks this and never [app.composerDestinations].
func (a *app) composerPlaces() []string { return a.composer.places }

// composerDestinations is that list, built: THE DESTINATION THE LAYER OPENS ON
// FIRST, then this window's own project, then every project on this disk, each
// once.
//
// The opening destination is first because it is the answer a person gets
// without pressing anything, and a cycle whose first step walked away from where
// you already are would be a key that has to be pressed all the way round to
// undo.
//
// IT ASKS [app.readWorld] AND NOT THE SWITCHER'S CACHE, because the switcher's
// cache belongs to home and is dropped the moment home closes — and this layer
// opens on any of the seven. That function is also the one that answers NOTHING
// over `--host`, which is exactly right here: the projects under this process
// are the laptop's while the session runs on the server, and offering them as
// destinations would be offering somewhere the work cannot go.
func (a *app) composerDestinations() []string {
	world := a.home.world
	if len(world.Projects) == 0 {
		world = a.readWorld()
	}
	out := make([]string, 0, len(world.Projects)+2)
	seen := map[string]bool{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	add(a.composerOpensAt())
	add(a.workspace)
	for _, project := range world.Projects {
		add(project.Path)
	}
	return out
}

// composerMove is `alt+w`: the next destination on the list, round again from
// the last. It answers false where there is only one — a machine with one
// project has nowhere to move a task to, and the clause naming this key is then
// absent from the line ([app.composerRows]).
func (a *app) composerMove() (tea.Cmd, bool) {
	places := a.composerPlaces()
	if len(places) < 2 {
		return nil, false
	}
	here := a.composerWhere()
	next := places[0]
	for i, path := range places {
		if path == here {
			next = places[(i+1)%len(places)]
			break
		}
	}
	a.composer.where = next
	return a.refreshRepoOf(next, a.now()), true
}

// ── the rows the frame draws under the box ──────────────────────────────────

// The sentences the layer says. Each is quoted in the manual exactly as it is
// spelled here, and each is the design's own wording (SCREEN 2e).
const (
	// composerLeadWord is what the layer says about the whole thing, above the
	// three facts: what will happen, in the future tense, because none of it has.
	composerLeadWord = "it will run on its own and tell you when it lands"
	// composerKindWord is the right of that line: WHAT this is. It is the one
	// word that tells the two `enter`s apart before either is pressed.
	composerKindWord = "a task"
	// composerMoveWord, composerModelWord and composerCapEditWord are the right
	// of the three fact lines: how each one is edited, said where it is shown.
	composerMoveWord     = "alt+w to move it"
	composerModelWord    = "alt+o to change"
	composerCapEditWord  = "type a number"
	composerCapSaysWord  = "it may spend up to "
	composerCapAsksWord  = " before it asks"
	composerRunsOnWord   = " runs on "
	composerWhereInWord  = "in "
	composerWhereOnWord  = ", on "
	composerFootSendWord = "alt+enter send it off · enter talk about it first · esc back to "
	// composerPickWord is the foot while the model list is open, in the hint
	// grammar every other legend on this surface is written in.
	composerPickWord = "↑↓ pick · enter use it · esc back"
)

// composerRows is the layer's own rows, drawn under the composer's box and above
// the foot. The frame reserves the room for them before it gives the body any
// (pages.go's [placeFrameWithBar]).
//
// EACH LINE CARRIES ITS OWN KEY AT THE RIGHT EDGE, which is what "edited where
// it is shown" means on a screen with no pointer: the fact and the way to change
// it are one row, and a key with nothing to do is simply not on it.
func (a *app) composerRows(width int, pal palette) []string {
	if !a.composer.open || width < 1 {
		return nil
	}
	rows := []string{composerFlush(" "+pal.muted(composerLeadWord), pal.dim(composerKindWord), width)}
	if where := a.composerWhereLine(); where != "" {
		key := ""
		if len(a.composerPlaces()) > 1 {
			key = a.chords.say(composerMoveWord)
		}
		rows = append(rows, a.composerFact(where, key, width, pal))
	}
	if model := a.composerModel(); model != "" {
		slot, _ := config.ModelSlotFor(composerSlot)
		rows = append(rows, a.composerFact(slot.Label+composerRunsOnWord+model, a.chords.say(composerModelWord), width, pal))
	}
	rows = append(rows,
		a.composerFact(composerCapSaysWord+a.composerCapWord()+composerCapAsksWord, composerCapEditWord, width, pal))
	return rows
}

// composerWhereLine is the first fact: where it runs, and which branch that tree
// is on. A workspace nobody can name draws nothing at all, and a tree with no
// branch — a detached head, or a directory that is not a repository — draws the
// project and stops there rather than trailing a comma.
func (a *app) composerWhereLine() string {
	where := shortPath(a.composerWhere(), a.tilde, 0)
	if where == "" {
		return ""
	}
	line := composerWhereInWord + where
	if branch := a.repoBranchOf(a.composerWhere()); branch != "" {
		line += composerWhereOnWord + branch
	}
	return line
}

// composerFact is one of the three lines: the bullet, the fact, and the key at
// the right edge. The bullet is [tokens.GlyphProseBullet], which is what a line
// with no lifecycle to be queued in wears everywhere on this surface — a memory
// belief, a model row on the spend place, and now a fact about a task that has
// not left yet.
func (a *app) composerFact(fact, key string, width int, pal palette) string {
	left := " " + pal.dim(tokens.GlyphProseBullet) + " " + pal.muted(fact)
	if key == "" {
		return left
	}
	return composerFlush(left, paintHint(key, pal, pal.dim), width)
}

// composerFlush puts one clause against the right edge of a row, and drops it
// rather than crowding the sentence when there is no room for both. It is
// [app.placeChipped]'s arithmetic said for a row the frame builds itself.
func composerFlush(row, right string, width int) string {
	if right == "" {
		return row
	}
	gap := width - ansi.StringWidth(row) - ansi.StringWidth(right) - 1
	if gap < 1 {
		return row
	}
	return row + strings.Repeat(" ", gap) + right
}

// composerFoot is the line under the three facts, in the hint line's own cells:
// the two ways out and the way on. It names the place by its own word, because
// `esc back to spend` is a sentence a person can act on and `esc back` is one
// they have to remember the answer to.
func (a *app) composerFoot() string {
	if a.composer.pick.open {
		return composerPickWord
	}
	word := a.composer.at.word()
	if word == "" {
		word = pageHome.word()
	}
	return composerFootSendWord + word
}

// composerFade is the page behind, one stop down the depth ladder at its
// FAINTEST (depthfade.go states the ladder and why stop 0 is the deepest).
//
// It repaints rather than overlays, and it has to, for [palette.fadeRow]'s
// reason: a row of a place arrives with its own colours already in it, and a hue
// wrapped around the outside of that would be overridden by the first sequence
// inside it. This is that function without its truecolor gate — a terminal with
// no gradient still has a dim tier, and a layer whose page did not recede at all
// would be a layer that reads as a page.
func composerFade(row string, pal palette) string {
	if row == "" {
		return row
	}
	return pal.fade(unpaint(row), 0)
}

// ── the keyboard, while the layer is up ─────────────────────────────────────

// composerLayerKey is EVERY key while the layer is standing, and it takes them
// all: the layer is a decision with four answers on the screen, and a key that
// fell through it would be a key acting on a place the person is looking past.
//
// It is read from [app.placeKeyPress] BEFORE the router's six classes and before
// the place's own `owns`, which is the same arbitration every other layer on
// this surface gets — everything that has deliberately claimed the whole
// keyboard is asked first, and the router is the last claimant rather than the
// first (placekeys.go's header).
func (a *app) composerLayerKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.composer.open {
		return nil, false
	}
	// AND THE MODEL LIST INSIDE THE LAYER IS ASKED BEFORE THE LAYER IS, on the
	// same terms: it is a filterable list a person is typing into, and a chord
	// lifted out of it would be a letter that went missing from their query.
	if a.composer.pick.open {
		return a.composerPickKey(msg), true
	}
	key := msg.String()
	switch key {
	case "esc":
		a.closeComposerLayer()
		a.touch()
		return nil, true

	case "alt+enter":
		return a.composerSend(), true

	case "enter":
		// TALK ABOUT IT INSTEAD, which is the existing door and not a second one:
		// the layer goes, and [app.placeTalk] starts the conversation carrying the
		// sentence exactly as it would have from the place underneath.
		a.closeComposerLayer()
		return a.placeTalk(), true

	case "alt+w":
		asked, moved := a.composerMove()
		if moved {
			a.touch()
		}
		return asked, true

	case "alt+o":
		a.openComposerPicker()
		return nil, true

	case "backspace":
		if text := a.composer.capText; text != "" {
			a.composer.capText = text[:len(text)-1]
			a.touch()
		}
		return nil, true
	}
	// TYPING DIGITS EDITS THE CAP, which is the third line's own key and the one
	// class of printable this layer takes off the composer. The sentence is
	// already written and is on the screen above; what a person types here is an
	// amount of money, so digits and one point are the whole alphabet.
	if len(key) == 1 && (key[0] >= '0' && key[0] <= '9' || key[0] == '.') {
		a.composer.capText += key
		a.touch()
		return nil, true
	}
	// EVERYTHING ELSE IS SWALLOWED. A letter that reached the composer would edit
	// a sentence the layer is drawn under and has already described; a chord that
	// reached the router would walk out of a decision half made.
	return nil, true
}

// composerSend is the second `alt+enter`: the sentence leaves, with the three
// facts on it.
//
// IT GOES THROUGH THE ONE ERRAND DOOR ([app.askHereWith]) and carries you to
// home for [app.placeSend]'s stated reason — an errand's answer is drawn in
// home's column, and minting one somewhere a person cannot watch it is the one
// failure worse than saying no.
func (a *app) composerSend() tea.Cmd {
	box := a.placeBox()
	if box == nil {
		a.closeComposerLayer()
		return nil
	}
	text := strings.TrimSpace(box.String())
	if text == "" {
		a.closeComposerLayer()
		return nil
	}
	orders := ErrandOrders{
		Workspace: a.composerWhere(),
		Model:     strings.TrimSpace(a.composer.model),
		CapUSD:    a.composerCapUSD(),
	}
	a.closeComposerLayer()
	if a.at(pageHome) {
		return a.askHereWith(text, orders)
	}
	box.reset()
	return tea.Batch(a.showPage(pageHome), a.askHereWith(text, orders))
}

// ── the model list, over the execution slot ─────────────────────────────────

// openComposerPicker is `alt+o`: THE model list, asked the execution slot's own
// question.
//
// It is [picker] and not a list of this file's own, for the reason the settings
// panel's slot rows are: there is one filterable model list on this surface, and
// a slot row, /model and this are three doors onto it rather than three lists
// that look alike. The rows come out of it whole ([picker.rowsOwned]) and are
// drawn inside the layer, because a place takes the frame and the bottom-anchored
// overlay would have nowhere under it to sit.
func (a *app) openComposerPicker() {
	a.composer.pick.startFor(a.modelsFor(filterFor(config.ModelSettingKey(composerSlot))),
		a.composerModel(), filterFor(config.ModelSettingKey(composerSlot)))
	a.touch()
}

// composerPickKey is every key while that list is open: the walk and the filter
// are the picker's own ([picker.navigate]), and the two decisions are this
// layer's — which is exactly the split /model and the settings panel already
// keep.
func (a *app) composerPickKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.composer.pick.close()
	case "enter":
		if chosen, ok := a.composer.pick.choice(); ok {
			a.composer.model = chosen.ID
		}
		a.composer.pick.close()
	default:
		a.composer.pick.navigate(msg)
	}
	a.touch()
	return nil
}

// composerPickRows is that list drawn in the body's room, with the hits the
// pointer would need. It is the settings panel's own move ([sheet.selectLines])
// and for its reason: the list belongs inside this frame, so the frame draws it.
// AND THE BODY OF IT IS SHARED WITH HOME'S OWN TARGET LIST, which is a second
// door onto the same picker drawn in the same room (homedraft.go's
// [app.targetPickRows]). Two functions padding one list to one frame would be
// two answers to how many rows it takes.
func (a *app) composerPickRows(width, room int, pal palette) []placeRow {
	return pickerRowsIn(&a.composer.pick, width, room, pal, a.reasoningFor)
}
