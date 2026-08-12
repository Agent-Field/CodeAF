package chat

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/dialogchrome"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/settings"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The overlay plane: three doors, one slot, one rule.
//
// 5.22's discoverability law is that nothing is typed-only, and 5.20 rule 3 is
// that `?` in any room lists what THIS room can do. Both are answered by
// components this package consumes and does not own — internal/tui2/palette for
// the jump palette and the capability sheet, internal/tui2/settings for the
// settings surface — so what lives here is the wiring and nothing else: which
// key raises which door, what catalog the door is handed, and what a chosen row
// means once the door has closed.
//
// The rule the plane keeps: EXACTLY ONE overlay at a time, and closing it puts
// the keyboard back where it was. The compositor already guarantees a raised
// overlay takes both the cells and the clicks, so a second door opened over a
// first would be two components reading one keyboard with only one of them
// visible — the failure mode the z discipline exists to make impossible, and
// the only part of it this side has to hold up.

// overlayKind names which door is open. It is one field rather than three
// booleans for the reason the live turn is one value: "which overlay is up" is
// one question, and answering it from a set of flags is how a surface grows a
// state nobody can hold in their head.
type overlayKind uint8

const (
	// overlayNone is the ordinary surface.
	overlayNone overlayKind = iota
	// overlayPalette is ctrl+space (ctrl+k still works): find any job, jump
	// across scopes, run an action, open a setting.
	overlayPalette
	// overlayCapability is `?`: what this room can do, in words, from the
	// registry rather than from a hand-written list.
	overlayCapability
	// overlaySettings is the settings sheet.
	overlaySettings
	// overlayModel is the model palette (5.10): five role rows, and one role's
	// models beneath. It is raised by the settings sheet's model rows, by the
	// registry's own `/model` entry, and by nothing that bypasses either.
	overlayModel
)

// -- raising and dropping ----------------------------------------------------

// THE DRILL, ACROSS DOORS.
//
// internal/tui2/palette/trail.go states the grammar: a trail that is both where
// you are and the way back, backspace-on-empty as the key, esc closing the whole
// thing from any depth. It landed inside the palette and inside the model
// picker, and it stopped at the edge of each — which meant a reader who reached
// the settings sheet THROUGH the palette had no way back at all, and a reader
// who reached the model picker through the sheet had a trail that started
// halfway along their own path.
//
// The path is one path, and only this file knows it: the components hold words
// and never learn what they open (that is why each takes a trail and a callback
// rather than a reference to its parent). So the words are assembled here, on
// the way in, and read back here, on the way out — a rung is named by its word,
// and [App.reopenStep] is the one place a word becomes a door again.

// paletteStep is the palette's own word at the head of a drilled path. It is
// the palette's, spelled once, so the word a reader clicks to get back to the
// whole catalog is the word that catalog calls itself.
var paletteStep = []string{palette.RootWord}

// openPalette raises the summon palette (ctrl+space, ctrl+k) fresh: a new
// query, no selection, no drill depth.
func (a *App) openPalette() tea.Cmd {
	a.buildPalette()
	a.palette.Reset()
	a.palette.SetCatalog(a.catalog(""))
	return a.raise(overlayPalette, a.palette)
}

// reopenPalette is the palette as the reader left it, which is what a step back
// onto it has to be.
//
// No Reset and no fresh catalog: popping is a RETURN, not a fresh open, and a
// reader who searched, drilled and then walked back into a blank palette would
// have been moved by the surface rather than by themselves (7.2 — the same rule
// [palette.Palette.Pop] keeps one level down).
func (a *App) reopenPalette() tea.Cmd {
	if a.palette == nil {
		// Nothing to return to. A fresh one is honest and is what the word on
		// the trail promised.
		return a.openPalette()
	}
	return a.raise(overlayPalette, a.palette)
}

func (a *App) buildPalette() {
	if a.palette != nil {
		return
	}
	a.palette = palette.New(palette.Options{
		Styler:     a.style,
		Linear:     a.linear,
		Invalidate: a.shell.Invalidate,
		OnChoose:   a.chooseDrilled,
		OnClose:    a.closeOverlay,
	})
}

// reopenStep turns one word of a trail back into the door it named. above is
// the path that word itself hangs from, so a sheet reopened two rungs down
// still knows the rung above it.
func (a *App) reopenStep(word string, above []string) tea.Cmd {
	switch word {
	case palette.RootWord:
		return a.reopenPalette()
	case settings.TrailWord:
		return a.raiseSettings(above, false)
	}
	return nil
}

// backFromSettings is [settings.Model.SetTrail]'s callback: the sheet was asked
// to leave for one of the rungs above it.
func (a *App) backFromSettings(depth int) tea.Cmd {
	if a.settings == nil {
		return nil
	}
	trail := a.settings.Trail()
	if depth < 0 || depth >= len(trail) {
		return nil
	}
	return a.reopenStep(trail[depth], trail[:depth])
}

// backFromModels is the same door for the model picker, which can sit one rung
// deeper again — palette, sheet, models.
func (a *App) backFromModels(depth int) tea.Cmd {
	if a.models == nil {
		return nil
	}
	trail := a.models.Trail()
	if depth < 0 || depth >= len(trail) {
		return nil
	}
	return a.reopenStep(trail[depth], trail[:depth])
}

// openCapability raises the `?` sheet for the room the user is in.
func (a *App) openCapability() tea.Cmd {
	if a.capability == nil {
		a.capability = palette.NewCapability(palette.Options{
			Styler:     a.style,
			Linear:     a.linear,
			Invalidate: a.shell.Invalidate,
			OnChoose:   a.choose,
			OnClose:    a.closeOverlay,
		})
	}
	a.capability.SetCatalog(a.belt(a.roomTitle()))
	return a.raise(overlayCapability, a.capability)
}

// openSettings raises the settings sheet on its own — alt+, , the slash row.
// It wears no trail and answers backspace exactly as it always has.
func (a *App) openSettings() tea.Cmd { return a.raiseSettings(nil, false) }

// raiseSettings raises the sheet, reached through the given path.
//
// The registry comes from the engine, so a window with no head behind it has no
// settings to show — and says so, because [settings.New] renders a nil registry
// as the honest sentence rather than as an empty list. A surface that cannot
// reach the store must not look like a store with nothing in it.
//
// The trail is installed on EVERY raise, including the empty one, because the
// pane is reused: a sheet that kept the path of the door that opened it last
// time would tell a reader who pressed alt+, that they had come from a palette
// they never opened.
func (a *App) raiseSettings(trail []string, refresh bool) tea.Cmd {
	if a.settings == nil {
		a.settings = settings.New(settings.Options{
			Registry:   a.settingsRegistry(),
			Styler:     a.style,
			Linear:     a.linear,
			Invalidate: a.shell.Invalidate,
			OnClose:    a.closeOverlay,
			Now:        a.now,
		})
	}
	a.settings.SetTrail(trail, a.backFromSettings)
	if refresh {
		// The sheet re-reads the registry, because the palette showed values it
		// resolved when its own door opened and a stale reading is the one thing
		// a settings surface must never present as current.
		a.settings.Refresh()
	}
	return a.raise(overlaySettings, a.settings)
}

// settingsRegistry asks the engine for the settings registry, or nil when there
// is no engine to ask.
func (a *App) settingsRegistry() *config.Settings {
	provider, ok := a.commander.(interface{ Settings() *config.Settings })
	if !ok {
		return nil
	}
	return provider.Settings()
}

// raise mounts a pane on the overlay plane and gives it the keyboard.
//
// The keyboard is taken rather than shared: an overlay is modal by construction
// here (the compositor gives it the cells and the clicks), so leaving the
// composer focused underneath would let a keystroke reach a draft the reader
// cannot see.
func (a *App) raise(kind overlayKind, pane tui2.Pane) tea.Cmd {
	a.overlay = kind
	a.shell.SetPane(tui2.LayerOverlay, pane)
	// The boundary, painted. 12.11 shipped the slot and left the paint owed —
	// "a lane that wants it tinted binds a pane to LayerDialogChrome like any
	// other layer" — and this is that binding, made here because this is where
	// a dialog becomes a thing on screen. It is built per raise rather than
	// held: the ring has no state, and a field for it would be one more thing
	// to keep in step with the profile.
	chrome := dialogchrome.New(a.style)
	// §16 OVERLAY DISMISSAL's third door. The close is THIS close — the one esc
	// and the `close esc` chip take — so a dismissing click flushes a pending
	// settings write and restores the keyboard exactly as the key would.
	chrome.SetDismiss(a.closeOverlay)
	a.shell.SetPane(tui2.LayerDialogChrome, chrome)
	a.shell.SetOverlay(true)
	a.composer.Focus(false)
	a.refresh()
	return nil
}

// closeOverlay drops the plane and restores the focus the door took.
func (a *App) closeOverlay() tea.Cmd {
	if a.overlay == overlayNone {
		return nil
	}
	if a.overlay == overlaySettings && a.settings != nil {
		// A debounced write that has not landed yet is a change the reader has
		// already made. Closing the sheet is the last moment it can be honoured,
		// and losing it would make the surface quietly disagree with the file.
		a.settings.Flush()
	}
	a.overlay = overlayNone
	a.shell.SetOverlay(false)
	a.shell.SetPane(tui2.LayerOverlay, nil)
	a.shell.SetPane(tui2.LayerDialogChrome, nil)
	a.composer.Focus(!a.railFocus)
	a.refresh()
	return nil
}

// overlayKey routes a keystroke to the raised door, and reports whether one
// took it. Only ctrl+c gets past — quitting is not an overlay's to refuse.
func (a *App) overlayKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch a.overlay {
	case overlayPalette:
		return a.palette.Key(msg), true
	case overlayCapability:
		return a.capability.Key(msg), true
	case overlaySettings:
		return a.settings.Key(msg), true
	case overlayModel:
		return a.models.Key(msg), true
	}
	return nil, false
}

// -- what a chosen row means -------------------------------------------------

// choose performs one palette result. The palette executes nothing itself — it
// hands back a closed sum of three intents — so this is the only place a row on
// that list becomes an act, and every act it can produce is a door that already
// exists somewhere else in this package.
func (a *App) choose(result palette.Result) tea.Cmd {
	return a.chooseFrom(result, nil)
}

// chooseDrilled is the summon palette's own OnChoose: the same acts, one rung
// deeper in the palette's path.
//
// The `?` sheet keeps [App.choose] and stays trail-less on purpose. It is raised
// OVER the room the reader is in rather than being a place they navigated to,
// so a door it opens has nowhere to go back to that is not simply "the room" —
// and esc already is that.
func (a *App) chooseDrilled(result palette.Result) tea.Cmd {
	return a.chooseFrom(result, paletteStep)
}

// chooseFrom performs one palette result, remembering the path it was chosen
// from so a door it opens can be walked back out of.
//
// Only the two acts that RAISE something take the path. A jump changes the room
// under the overlay and leaves nothing on screen to go back from; a trail on it
// would be a way back to a list the reader has already left.
func (a *App) chooseFrom(result palette.Result, from []string) tea.Cmd {
	switch chosen := result.(type) {
	case palette.JumpToRoom:
		return a.jumpTo(chosen.ID)
	case palette.RunEntry:
		return a.runEntryFrom(chosen.ID, from)
	case palette.OpenSetting:
		return a.openSettingRowFrom(chosen.Key, from)
	}
	return nil
}

// runEntryFrom is [App.runEntry] for a row chosen from a surface that is itself
// a rung of a path. Only the two entries that raise a door of their own care;
// everything else is the one executor, unchanged.
func (a *App) runEntryFrom(id string, from []string) tea.Cmd {
	if len(from) == 0 {
		return a.runEntry(id)
	}
	switch id {
	case settings.EntryID:
		return a.raiseSettings(from, false)
	case modelEntryID:
		return a.raiseModels(from, "")
	}
	return a.runEntry(id)
}

// jumpTo moves the cursor — and therefore the main pane and the composer — to a
// room named by its rail id. It lands the reader exactly where walking there
// with j, k and enter would have — INCLUDING the scope push. A jump that bound
// the room without entering the scope left Depth() at zero, and the esc ladder
// found no rung to pop: every door into a task room (board, notebook, mention,
// card, palette) opened a page the reader could not leave.
func (a *App) jumpTo(id string) tea.Cmd {
	if a.railModel == nil {
		return nil
	}
	if id == rail.HomeScopeID || id == rowHomeID {
		a.railModel.Home()
		event, _ := a.railModel.SelectID(rowHomeID)
		return a.bind(orPreview(a.railModel, event), true)
	}
	if _, found := a.railModel.SelectID(id); found {
		return a.applyScope(a.railModel.Enter())
	}
	// The row is not in the scope on screen. Pop to home and look again — a
	// palette that could name a room it could not reach would be a list of
	// doors half of which open nothing.
	a.railModel.Home()
	if _, found := a.railModel.SelectID(id); found {
		return a.applyScope(a.railModel.Enter())
	}
	return nil
}

// orPreview turns a no-op selection into the current one. SelectID reports an
// empty event when the cursor was already on the row asked for, and a jump to
// where you already are still has to bind the surface — the reader pressed a key
// and is owed the room, not silence.
func orPreview(model *rail.Model, event rail.Event) rail.Event {
	if event.Empty() {
		return model.Preview()
	}
	return event
}

// runEntry performs a registry action chosen from the palette, the `?` sheet or
// the slash line. It is the ONE executor: three surfaces render the catalog and
// exactly one place turns a row of it into an act.
//
// The rule this function has to keep is 5.22's, and it is stronger than "run
// what we can". Every row the reader can see and reach must either DO its verb
// or say why it cannot (5.20 rule 3) — a live-looking row that answers a click
// with silence is worse than no row, because it teaches the reader that the
// surface does not respond to pointing. The default branch below is therefore
// paired with [App.entryReason]: an id that lands there has a sentence there,
// and the parity test in overlay_test.go is what keeps that true as rows are
// added.
func (a *App) runEntry(id string) tea.Cmd {
	switch id {
	case settings.EntryID:
		return a.openSettings()
	case modelEntryID:
		// The registry's own model door, which this surface listed and then did
		// nothing with. A row that named a door and opened none is the exact
		// shape 5.22 rule 5 refuses.
		return a.openModelPicker()
	case helpEntryID:
		// `?` from inside `?` is not a loop: the sheet was raised over the room
		// the reader is in, and raising it again re-reads that room's catalog.
		return a.openCapability()
	case paletteEntryID:
		// The palette listing its own door is not a curiosity — 5.22 admits no
		// typed-only action, and the summon chord is the most typed-only thing
		// this surface has. Choosing it from the `?` sheet is how a reader who
		// found the sheet first learns the chord that would have got them here.
		return a.openPalette()

	// The map. "show tasks" opens it; "focus tasks" also puts the keyboard on
	// it. Two verbs, two different amounts of commitment, one flag — which is
	// why they are not the same row.
	case "slash.graph":
		return a.setScope(true)
	case "slash.tasks":
		cmd := a.setScope(true)
		a.focusScope(true)
		return cmd

	// The four rooms of 5.24, reached by their own names. jumpTo is the rail's
	// SelectID, so a slash lands the reader exactly where walking there with j
	// and k would have — but the group is collapsed by default, and a jump to a
	// row inside a closed lid would find nothing. Opening it first is not a
	// special case for the pointer: it is what the reader would have had to do.
	case "slash.self":
		return a.openHome(homes.HomeSelf)
	case "slash.standing":
		return a.openHome(homes.HomeStanding)

	case "slash.new":
		return a.openRoomCmd()
	case "slash.history":
		// Finished work lives in the palette's own history grouping — the same
		// list, off the same Attention (overlay.go's catalogRooms). The verb
		// says "find finished work in permanent memory"; this is where it is.
		return a.openPalette()
	case "slash.budget":
		// The daily limit is a settings row, and the sheet is its one home
		// (8.2.16). The key is named rather than left to the reader's search so
		// the day openSettingRow's REQUESTED SEAM closes, this lands on the row.
		return a.openSettingRow(config.KeyDailyBudget)

	// THE PLACES TABS, AND THEY ARE PAGES (§7). Each of the three swaps the
	// LENS: the thread, the work board (board.go), the notebook page. They used
	// to name states of this one surface instead — `chat` re-selected the home
	// row and `work` opened the map, which meant the strip's three words
	// described two things and a rail toggle. A tab that does not change what
	// you are looking at is a tab in name only.
	//
	// They are routed HERE, in the one executor, rather than beside the strip
	// that draws them: the tabs, the chords and a palette row that names the
	// same place all arrive at this switch, so a place cannot be reachable one
	// way and not another (5.22).
	case placeThreadID:
		return a.showPage(pageThread)
	case placeBoardID:
		return a.showPage(pageBoard)
	case placeNotebookID, "slash.memory":
		// `/memory` is the notebook's alias, and it lands on the same page: the
		// catalog lists each action once under one canonical verb, and an alias
		// that opened a different surface would be a twin (§8).
		return a.showPage(pageNotebook)

	case "key.thread.receipts":
		return a.toggleReceipts()
	case "key.thread.clear-draft":
		return a.clearDraft()
	case "key.thread.copy-answer":
		return a.copyAnswer()
	case "key.thread.copy-file":
		return a.copyFile()
	case "key.quit", "slash.quit":
		return tea.Quit
	}
	return nil
}

// openHome takes the reader to one of 5.24's four rooms, opening the collapsed
// group on the way if that is what stands between them and it.
func (a *App) openHome(home homes.Home) tea.Cmd {
	if a.railModel == nil {
		return nil
	}
	if a.source != nil && !a.source.homes.Expanded {
		a.toggleHomes()
	}
	return a.jumpTo(home.ScopeID())
}

// helpEntryID is the registry row for the capability sheet, named once for the
// same reason modelEntryID is.
const helpEntryID = "slash.help"

// modelEntryID is the registry row for the model palette. It is named once so
// the door, the reason and the footer cannot drift apart.
const modelEntryID = "slash.model"

// paletteEntryID is the registry row for the summon palette itself, named once
// for the same reason. Its key — the chord app.go routes and every surface that
// reads the registry teaches — is [registry.Entry.Key] on this row and is
// spelled nowhere else.
const paletteEntryID = "key.palette"

// openSettingRow opens the settings sheet ON one setting, from a caller with no
// path behind it (`/budget` typed at the composer).
func (a *App) openSettingRow(key string) tea.Cmd {
	return a.openSettingRowFrom(key, nil)
}

// openSettingRowFrom is the same door, reached through a path.
//
// The seam this used to carry is closed. It read: "the sheet has no select-this-
// key door, so a chosen row opens the sheet rather than the row … but the
// palette's own promise is that picking a setting takes you TO it." The sheet
// has that door now ([settings.Model.Select]), so the promise is kept — a reader
// who searched for one row once does not have to search for it again on the
// screen they asked for.
func (a *App) openSettingRowFrom(key string, from []string) tea.Cmd {
	cmd := a.raiseSettings(from, true)
	if a.settings != nil {
		// After the refresh, never before: Select walks the rows the sheet is
		// actually showing, and the refresh is what rebuilds them.
		a.settings.Select(key)
	}
	return cmd
}

// -- the catalog -------------------------------------------------------------

// catalog is what both palette surfaces draw from, rebuilt at the moment a door
// opens and never per keystroke.
//
// Three of the four groups are already in hand: the rooms are the rail's own
// rows, the actions are the registry's, and the settings are one read of the
// registry the engine holds. THE JOBS ARE NOT, and that is the trade this wave
// made deliberately. The rail draws the newest twenty-four job roots of the
// active snapshot (scope.go's maxTaskRows), which is the right window for a
// column six lines tall and the wrong one for the surface whose whole promise is
// "type three letters and find it" — a palette that could only find what was
// already on screen is a filter over the rail wearing a search field. So the
// door pays for one wider read of its own, on the keystroke that opens it and
// never per frame or per keystroke after: see [App.catalogJobs].
func (a *App) catalog(title string) palette.Catalog {
	return palette.Catalog{
		Scope:    registry.ScopeThread,
		Title:    title,
		Rooms:    a.catalogRooms(),
		Jobs:     a.catalogJobs(),
		Reason:   a.entryReason,
		Settings: a.catalogSettings(),
	}
}

// belt is the `?` sheet's catalog: the same scope, the same reasons, and NONE
// of the places.
//
// [palette.Capability] builds only the action rows — rooms and jobs and settings
// belong to the palette's catalog-of-everything, and a `?` that listed them
// would be answering a question nobody asked in this room. Handing it the full
// catalog was free while every group was already in memory; it stopped being
// free the moment the jobs group became a store read, and paying for a read
// whose rows are discarded on the way in is the kind of cost that only ever
// shows up on the machine with the biggest store.
func (a *App) belt(title string) palette.Catalog {
	return palette.Catalog{
		Scope:  registry.ScopeThread,
		Title:  title,
		Reason: a.entryReason,
	}
}

// Ledger is the wider job read the palette needs and no other surface does.
//
// It is an optional interface on the backend rather than a method on [Backend],
// for the reason scope.go's Graph, Subtrees and Rooms are: a window driven by a
// stub in a test, or by a backend that is only a message log, must still open
// and still render — it simply has no jobs to list, which is an honest empty
// group and not a broken one.
//
// AddressableNodes is the store's own answer to "what do I have about this?",
// and its doc says why it is the right corpus here in as many words: the compact
// reads stop naming a job the moment a territory packs it away, and "membership
// in a territory is a filing decision made hours after a job landed; it was
// never meant to be the thing that decides whether the job can be spoken about."
// A palette is exactly a place where a month-old job must still be findable.
type Ledger interface {
	AddressableNodes() ([]store.Node, error)
}

// catalogJobs is every job the store can still name, live and finished, newest
// first — the palette's `work` and `history` groups.
//
// Newest first is a walk BACKWARDS rather than a sort: AddressableNodes returns
// stable admission order, so the reverse of it is already the order a reader
// wants, and a sort here would be spending an allocation to rediscover a fact
// the query guaranteed.
//
// A job the rail is already offering as a room is still emitted; the palette
// drops it against its own room ids (palette.Catalog.Jobs), because it is the
// side that knows which of the two rows a reader would otherwise see twice.
func (a *App) catalogJobs() []palette.Job {
	ledger, ok := a.backend.(Ledger)
	if !ok {
		return nil
	}
	nodes, err := ledger.AddressableNodes()
	if err != nil || len(nodes) == 0 {
		// A read that failed is not a store with no work in it. Nothing is
		// claimed either way: the group simply does not appear, and the rooms
		// the rail already holds are still listed above it.
		return nil
	}
	byID := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	asks := questionCounts(a.catalogQuestions(), byID)
	usage := a.jobUsage()
	now := a.now()

	out := make([]palette.Job, 0, len(nodes))
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		if node.ID == store.RootID || node.Group == store.TerritoryGroup {
			// The spine root is not a job and a territory is a filing cabinet.
			// Neither is a place the reader can be taken to.
			continue
		}
		if !isJobRoot(node, byID) {
			continue
		}
		job := palette.Job{
			ID:        rowTaskPrefix + node.ID,
			Title:     nodeLabelOf(node),
			Seed:      node.ID,
			Attention: rail.Row{Life: lifeOf(node), Questions: asks[node.ID]}.Attention(),
			Questions: asks[node.ID],
			Summary:   firstLine(node.Brief),
			Receipt:   nodeStatusLine(node),
			Age:       jobAge(node, now),
		}
		if job.Summary == job.Title {
			// The label falls back to the brief when a job was never titled
			// (nodeLabelOf), and a row that says the same sentence twice in two
			// columns has spent the second one saying nothing.
			job.Summary = ""
		}
		if spent, found := usage[node.ID]; found && spent.Cost > 0 {
			job.Cost = tokens.Money(spent.Cost)
		}
		out = append(out, job)
	}
	return out
}

// catalogQuestions reads the open questions once so a job that is blocking on a
// human wears the amber `?N` chip in the palette as well as on the rail. Amber
// only ever means a human is actually needed (5.16), and a list of forty jobs is
// precisely where that has to be true.
func (a *App) catalogQuestions() []store.AgentQuestion {
	graph, ok := a.backend.(Graph)
	if !ok {
		return nil
	}
	questions, err := graph.OpenQuestions("", maxQuestionRead)
	if err != nil {
		return nil
	}
	return questions
}

// jobUsage is what each job root has spent, by job root id. The rail asks the
// same question at every journal move; this is the palette asking it once, at
// the moment its door opens, because the rail's answer is scoped to the rows it
// kept and this list is wider than those.
func (a *App) jobUsage() map[string]store.JobUsage {
	graph, ok := a.backend.(Graph)
	if !ok {
		return nil
	}
	usage, err := graph.TopLevelJobUsage()
	if err != nil {
		return nil
	}
	return usage
}

// jobAge is the row's time cell, and which time it reports depends on what the
// reader is asking. A job still going is asked "how long has this been running";
// a job that has stopped is asked "how long ago was this" — the same cell, two
// questions, and answering the second with a start time would put "3d" beside
// something that finished a minute ago.
//
// A job with neither stamp reports nothing rather than "0s": a queued job has
// not started, and a zero is a reading, not an absence.
func jobAge(node store.Node, now time.Time) string {
	if !node.FinishedAt.IsZero() {
		return tokens.Elapsed(now.Sub(node.FinishedAt))
	}
	if !node.StartedAt.IsZero() {
		return tokens.Elapsed(now.Sub(node.StartedAt))
	}
	return ""
}

// catalogRooms flattens the rail's home scope into jump targets.
//
// Only the rows that ARE rooms become entries: the `+ new` door and the
// collapsed group are navigation, not places, and a palette that offered them
// as rooms would send a reader somewhere that is not a conversation. Terminal
// rooms are not filtered out here — the palette's own grouping puts them under
// history, off their Attention, which is the fact it was given for.
func (a *App) catalogRooms() []palette.Room {
	if a.railModel == nil || a.source == nil {
		return nil
	}
	rows := a.source.home.Rows
	out := make([]palette.Room, 0, len(rows))
	for i := range rows {
		row := rows[i]
		switch {
		case row.Kind == rail.RowSurface, isRoomRow(row.ID), isTaskRow(row.ID),
			homes.Owns(row.ID):
			// The four homes are rooms and jump like rooms (5.24). The GROUP row
			// is not one — it is a lid — so it stays out by not being Owns.
		default:
			continue
		}
		out = append(out, palette.Room{
			ID:        row.ID,
			Title:     row.Name,
			Seed:      row.Seed,
			Attention: row.Attention(),
			Questions: row.Questions,
			Summary:   row.Status,
			Key:       jumpKey(i),
		})
	}
	return out
}

// jumpKey is the digit that reaches a row right now, matching the rail's own
// 1..9 jump. Past nine there is no chord and the row says so by carrying none —
// 5.20 rule 3 applies to a palette row as much as to a footer cell.
func jumpKey(index int) string {
	if index < 0 || index > 8 {
		return ""
	}
	return string(rune('1' + index))
}

func isRoomRow(id string) bool {
	return len(id) > len(rowRoomPrefix) && id[:len(rowRoomPrefix)] == rowRoomPrefix
}

func isTaskRow(id string) bool {
	return len(id) > len(rowTaskPrefix) && id[:len(rowTaskPrefix)] == rowTaskPrefix
}

// entryReason is 5.20 rule 3's "disabled affordances say why", in this
// surface's own words. The registry knows what an action IS; only the room
// knows whether it can be done here and now.
func (a *App) entryReason(entryID string) string {
	switch entryID {
	case "key.thread.receipts":
		if !a.foldable {
			return "nothing folded in this room yet"
		}
	case settings.EntryID:
		if a.settingsRegistry() == nil {
			return "no engine behind this window"
		}
	case modelEntryID:
		// The palette's own whole-surface reason, asked one door earlier so the
		// reader learns it before they press enter rather than after.
		return a.modelDisabled()
	// key.thread.clear-draft deliberately has NO reason, and the omission is the
	// decision: an empty draft is not a room that cannot clear one, it is a
	// buffer with nothing in it a moment ago. The distinction matters here
	// because a disabled row renders its reason INSTEAD of its accelerator, and
	// `?` is reachable from the composer only on an empty draft — so a row
	// refused for emptiness would be a row that is refused every single time
	// anyone can read it, and the one thing the sheet exists to teach (the
	// chord) would be the one thing it never showed. 5.22's sheet teaches
	// accelerators; 5.20 rule 3's reasons are for doors this room cannot open.
	case "key.thread.copy-answer":
		if a.latestAnswer() == "" {
			return "no answer in this room yet"
		}
	case "key.thread.copy-file":
		if a.latestArtifact() == "" {
			return "nothing on disk from this room yet"
		}

	// The four rows this surface does not yet perform. They are in the registry
	// because they are real verbs of the product, and they are refused HERE
	// rather than dropped from the catalog, because 5.20 rule 3 is that a door
	// this room cannot open says so — a reader who has used the old window and
	// looks for /open must be told it is not here, not left to conclude that
	// clicking does nothing on this surface. Each sentence is deleted by the
	// lane that lands the verb; runEntry's default and this list are checked
	// against each other by a test, so neither can be forgotten.
	case "slash.node":
		return "open it from the map instead"
	case "slash.open":
		return "no OS-open on this surface yet"
	case "slash.session":
		return "the footer already says both"
	case "slash.cancel":
		return "steer the room instead, for now"
	case "slash.new":
		if a.source == nil || a.source.rooms == nil {
			return "this window cannot open rooms"
		}

	// Four rows the old window had and this one deliberately does not, each
	// refused in the words of the decision rather than as "unavailable". A
	// reader who learned these chords is owed the reason they stopped working,
	// and three of the four are not gaps at all — they are 5.15 and 8.3 having
	// replaced the thing the chord was for.
	case "key.thread.cycle-focus":
		return "one cursor here — ctrl+o moves it"
	case "key.thread.newline":
		return "typed, not run — alt+enter"
	case "key.thread.narrow-split", "key.thread.widen-split":
		return "the split is fixed here for now"
	case "key.voice":
		return "not on this surface"
	case "key.boost":
		return "not on this surface yet"
	}
	return ""
}

// catalogSettings reads the registry's rows once, at the moment the door opens.
// The palette deliberately does not import internal/config — its rows carry
// live closures, and a render that could read the filesystem is the cost this
// keeps out of a keystroke — so the values are resolved here.
func (a *App) catalogSettings() []palette.SettingRow {
	reg := a.settingsRegistry()
	if reg == nil {
		return nil
	}
	rows := reg.Rows()
	out := make([]palette.SettingRow, 0, len(rows))
	for _, row := range rows {
		pinned, _ := row.PinnedBy()
		out = append(out, palette.SettingRow{
			Key:    row.Key,
			Label:  row.Label,
			Hint:   row.Hint,
			Value:  row.Value(),
			Pinned: pinned,
		})
	}
	return out
}

// roomTitle names the room the `?` sheet is describing: the scope the rail is
// in, which is the room the reader is actually looking at.
func (a *App) roomTitle() string {
	if a.railModel == nil {
		return ""
	}
	crumbs := a.railModel.Breadcrumb()
	if len(crumbs) == 0 {
		return ""
	}
	return crumbs[len(crumbs)-1]
}
