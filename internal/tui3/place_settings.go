package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── THE SETTINGS PLACE ──────────────────────────────────────────────────────
//
// How this machine is set: every row of the profile, under its own inner tab
// bar, with one box that searches across all of them.
//
// THE PANEL ITSELF IS settings.go's AND STAYS THERE. That file is the registry
// join, the value editors, the model picker and the account rows — two thousand
// lines of what a SETTING is — and none of it is about being a place. What is
// here is the place: the handle the registry files, and the dozen answers the
// frame asks of it. The same split every other room keeps
// (docs/design/home-rethink/ARCHITECTURE.md's three layers).
//
// IT IS THE ONE PLACE WITH A SECOND BAR INSIDE IT, and the two are not a
// repetition: the upper one is the seven places and the lower one is this
// place's own sections. The panel is where [placeTabBar] was lifted from, so
// they are drawn by the same geometry and read as one object at two scales.

// placeSettings is this place's handle on the registry (pages.go's [place]
// states the contract and why the handle holds no state of its own).
type placeSettings struct{ placeBase }

func init() { registerPlace(placeSettings{}) }

func (placeSettings) id() page     { return pageSettings }
func (placeSettings) word() string { return "settings" }

func (placeSettings) open(a *app) tea.Cmd {
	a.raiseSettings()
	// THE CLOCK IS ARMED FOR THE BAR AND NOT FOR THIS PANEL. Nothing here is
	// read from disk on a beat — the rows are this machine's settings and they
	// change when somebody changes them — but the tab bar's numbers are
	// recomputed on that beat, and a room that armed no clock stopped the whole
	// bar counting while it was up ([placeSettings.tick]).
	return a.armPlaceClock()
}

// tick re-reads nothing and keeps the beat: see the note over [placeSettings.open].
func (placeSettings) tick(a *app, now time.Time) bool { return true }

func (placeSettings) close(a *app) { a.dropSettings() }

// body is the panel's own tab bar, the rule under it, and one section's rows —
// or, while a submenu is up, the options it is offering.
func (placeSettings) body(a *app, width, room int) []placeRow {
	s := &a.sheet
	pal := a.pal
	rows := make([]placeRow, 0, room)
	rows = append(rows, placeRow{text: sheetTabBar(width, s.tab, pal), hit: sheetHit{kind: sheetHitTabs}})
	rows = append(rows, placeRow{text: pal.dim(rule(width))})
	rows = append(rows, placeRow{})
	room -= len(rows)
	if room < 1 {
		room = 1
	}
	if s.sel != nil {
		body, at := s.selectLines(width, room, pal, a.reasoningFor)
		for i, line := range body {
			hit := sheetHit{}
			if at[i] >= 0 {
				hit = sheetHit{kind: sheetHitOption, index: at[i]}
			}
			rows = append(rows, placeRow{text: line, hit: hit})
		}
		return rows
	}
	body, owner := s.listLines(width, room, pal, a.hoveredSheetRow())
	at := s.cursorLine(owner)
	// THE CURSOR'S ROW IS SCROLLED IN WHOLE. At [tierPhone] it is two lines —
	// the name and the value under it — and a window that pinned only the first
	// would push the value off the bottom edge, leaving a selection band with one
	// end cut off and the fact being changed off screen. The last line is pinned
	// first and the first line second, so a row taller than the window still
	// shows its name.
	if last := s.cursorLastLine(owner, at, width); last != at {
		s.top = listTop(last, s.top, len(body), room)
	}
	s.top = listTop(at, s.top, len(body), room)
	for i := 0; i < room; i++ {
		index := s.top + i
		if index >= len(body) {
			rows = append(rows, placeRow{})
			continue
		}
		hit := sheetHit{}
		if owner[index] >= 0 {
			hit = sheetHit{kind: sheetHitRow, index: owner[index]}
		}
		rows = append(rows, placeRow{text: body[index], hit: hit})
	}
	return rows
}

// stops is every item of the current section the cursor may rest on — the walk
// `↑↓` takes. A heading is a label and a reading is a fact, and neither is
// something `enter` could do anything to ([sheetItem.restful]).
func (placeSettings) stops(a *app) []int {
	out := make([]int, 0, len(a.sheet.items))
	for i, item := range a.sheet.items {
		if item.restful() {
			out = append(out, i)
		}
	}
	return out
}

// cursorAt is the item of the current section the cursor is on (pages.go's
// [place.cursorAt]).
func (placeSettings) cursorAt(a *app) int { return a.sheet.cursor }

// box is the type-to-search box, and a submenu's own box outranks it in the
// order the panel already claims the keyboard in ([app.sheetKey]): the value
// being edited, then the model picker's filter, then the search that crosses
// every section.
func (placeSettings) box(a *app) *editor {
	switch {
	case a.sheet.edit != nil:
		return &a.sheet.edit.box
	case a.sheet.sel != nil:
		return &a.sheet.sel.pick.filter
	}
	return &a.sheet.query
}

// note is what the panel is holding: the submenu's label, the last refusal, or
// the count of rows that differ from a profile nobody has touched.
//
// EVERY ONE OF THESE FOUR LINES DROPS CLAUSES RATHER THAN CUTTING CHARACTERS.
// They are sentences with clauses in them — the foot note is two, separated by
// the surface's own middle dot (`saved to your profile · a project's own
// .aforge/config.json is a hand edit`), and a pinned row's note is a statement
// with the remedy hung off a dash — and a character ruler took sixty columns
// through the middle of a path and through the middle of the word `unset`. So
// they all go through [noteFit], which is the STATEMENT half of the pair the
// foot of every place is fitted by: a note's first clause is what happened and
// its later ones elaborate, so it drops from the end and the answer survives. On
// a line with nothing to drop it is exactly [fit], so the labels keep it too
// rather than each site having to decide.
func (placeSettings) note(a *app, width int) []string {
	pal := a.pal
	switch {
	case a.sheet.sel != nil:
		return []string{" " + pal.dim(noteFit(a.sheet.sel.label, width-2))}
	case a.sheet.edit != nil:
		// THE LABEL IS THE NOTE AND THE VALUE IS THE COMPOSER. The panel used to
		// draw both on one line of its own foot; under the router the box a person
		// is typing in is THE composer, so what is left here is the one thing the
		// box cannot say — which setting this is.
		return []string{" " + pal.dim(noteFit(a.sheet.edit.label, width-2))}
	case a.sheet.msg != "":
		return []string{" " + pal.bad(noteFit(a.sheet.msg, width-2))}
	}
	return []string{" " + pal.dim(noteFit(a.sheet.footNote(), width-2))}
}

func (placeSettings) hint(a *app) string { return a.sheet.keysLine() }

// key is the panel's own grammar (settings.go's [app.sheetKey]): the value being
// edited, the model picker, the section bar, and the search across all of them.
func (placeSettings) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	cmd, _ := a.sheetKey(msg)
	return cmd
}

// owns is the three boxes inside this panel that take the WHOLE keyboard, and
// they are read before the router claims a chord (pages.go's [place.owns]).
//
// A BOX THAT HAS THE KEYBOARD HAS ALL OF IT. The value being edited, the model
// picker's filter and the key box on the accounts tab each claim every key —
// every other key on this panel types into the search box, and a surface that
// let a pasted key narrow a list would be a surface putting half a secret in the
// title bar (connectcaps.go).
func (placeSettings) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := &a.sheet
	switch {
	case s.edit != nil:
		a.sheetEditKey(msg)
	case s.sel != nil:
		a.sheetSelectKey(msg)
	case s.conn.entry != nil && s.onConnections():
		return a.connEntryKey(msg), true
	default:
		return nil, false
	}
	a.touch()
	return nil, true
}
