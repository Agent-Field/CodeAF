package chat

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/dialogchrome"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
	"github.com/Agent-Field/aforge-v2/internal/tui2/palette"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/settings"
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
	// overlayPalette is ctrl+k: jump across scopes, run an action, open a
	// setting.
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

// openPalette raises the jump palette (ctrl+k).
func (a *App) openPalette() tea.Cmd {
	if a.palette == nil {
		a.palette = palette.New(palette.Options{
			Styler:     a.style,
			Invalidate: a.shell.Invalidate,
			OnChoose:   a.choose,
			OnClose:    a.closeOverlay,
		})
	}
	a.palette.Reset()
	a.palette.SetCatalog(a.catalog(""))
	return a.raise(overlayPalette, a.palette)
}

// openCapability raises the `?` sheet for the room the user is in.
func (a *App) openCapability() tea.Cmd {
	if a.capability == nil {
		a.capability = palette.NewCapability(palette.Options{
			Styler:     a.style,
			Invalidate: a.shell.Invalidate,
			OnChoose:   a.choose,
			OnClose:    a.closeOverlay,
		})
	}
	a.capability.SetCatalog(a.catalog(a.roomTitle()))
	return a.raise(overlayCapability, a.capability)
}

// openSettings raises the settings sheet.
//
// The registry comes from the engine, so a window with no head behind it has no
// settings to show — and says so, because [settings.New] renders a nil registry
// as the honest sentence rather than as an empty list. A surface that cannot
// reach the store must not look like a store with nothing in it.
func (a *App) openSettings() tea.Cmd {
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
	a.shell.SetPane(tui2.LayerDialogChrome, dialogchrome.New(a.style))
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
	switch chosen := result.(type) {
	case palette.JumpToRoom:
		return a.jumpTo(chosen.ID)
	case palette.RunEntry:
		return a.runEntry(chosen.ID)
	case palette.OpenSetting:
		return a.openSettingRow(chosen.Key)
	}
	return nil
}

// jumpTo moves the cursor — and therefore the main pane and the composer — to a
// room named by its rail id. It is the rail's own SelectID and nothing else, so
// a jump lands the reader exactly where walking there with j and k would have.
func (a *App) jumpTo(id string) tea.Cmd {
	if a.railModel == nil {
		return nil
	}
	if id == rail.HomeScopeID || id == rowHomeID {
		a.railModel.Home()
		event, _ := a.railModel.SelectID(rowHomeID)
		return a.bind(orPreview(a.railModel, event), true)
	}
	if event, found := a.railModel.SelectID(id); found {
		return a.bind(orPreview(a.railModel, event), true)
	}
	// The row is not in the scope on screen. Pop to home and look again — a
	// palette that could name a room it could not reach would be a list of
	// doors half of which open nothing.
	a.railModel.Home()
	if event, found := a.railModel.SelectID(id); found {
		return a.bind(orPreview(a.railModel, event), true)
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
	case "slash.notebook", "slash.memory":
		return a.openHome(homes.HomeNotebook)
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

	// The two place chords, which name places this surface has (5.15's home
	// scope and its map) rather than v1's two pages.
	case "key.place-thread":
		return a.jumpTo(rowHomeID)
	case "key.place-board":
		return a.setScope(true)

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

// openSettingRow opens the settings sheet for one setting.
//
// The sheet re-reads the registry first, because the palette showed values it
// resolved when its own door opened and a stale reading is the one thing a
// settings surface must never present as current.
//
// REQUESTED SEAM: the sheet has no "select this key" door, so a chosen row
// opens the sheet rather than the row. Nothing lies — the reader is one search
// away and the values are current — but the palette's own promise is that
// picking a setting takes you TO it.
func (a *App) openSettingRow(string) tea.Cmd {
	cmd := a.openSettings()
	if a.settings != nil {
		a.settings.Refresh()
	}
	return cmd
}

// -- the catalog -------------------------------------------------------------

// catalog is what both palette surfaces draw from, rebuilt at the moment a door
// opens and never per keystroke.
//
// Everything in it is already in hand: the rooms are the rail's own rows, the
// actions are the registry's, and the settings are one read of the registry the
// engine holds. Nothing here reads the graph — the rail did that at the last
// journal move — which is what keeps ctrl+k instant in a busy window.
func (a *App) catalog(title string) palette.Catalog {
	return palette.Catalog{
		Scope:    registry.ScopeThread,
		Title:    title,
		Rooms:    a.catalogRooms(),
		Reason:   a.entryReason,
		Settings: a.catalogSettings(),
	}
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
