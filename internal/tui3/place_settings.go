package tui3

import (
	"strings"
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
func (placeSettings) tick(a *app, now time.Time) (bool, tea.Cmd) { return true, nil }

func (placeSettings) close(a *app) { a.dropSettings() }

// body is the panel's own tab bar, the rule under it, and one section's rows —
// or, while a submenu is up, the options it is offering.
func (placeSettings) body(a *app, width, room int) []placeRow {
	s := &a.sheet
	pal := a.pal
	rows := make([]placeRow, 0, room)
	// NO RULE UNDER THE SECTIONS' BAR. The frame's own rule is four rows up,
	// and a second one inside the body drew the page as two frames stacked
	// (PLACES-AUDIT.md finding 11); the filled chip and one blank row are the
	// whole of what separates the bar from the rows.
	rows = append(rows, placeRow{text: sheetTabBar(width, s.tab, pal), hit: sheetHit{kind: sheetHitTabs}})
	rows = append(rows, placeRow{})
	if s.sel == nil && s.edit == nil && s.conn.entry == nil {
		filter, _, _ := draftBlock(&s.query, pal, width-2, 1, "type to search", "")
		for _, line := range filter {
			rows = append(rows, placeRow{text: " " + line})
		}
	}
	room -= len(rows)
	if room < 1 {
		room = 1
	}
	if s.sel != nil {
		filter, _, _ := draftBlock(&s.sel.pick.filter, pal, width-2, 1, "type to filter", "")
		for _, line := range filter {
			rows = append(rows, placeRow{text: " " + line})
		}
		room = max(1, room-len(filter))
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
// .codeaf/config.json is a hand edit`), and a pinned row's note is a statement
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
		lines := strings.Split(a.sheet.msg, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			out = append(out, " "+pal.bad(noteFit(line, width-2)))
		}
		return out
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
	case s.conn.entry != nil:
		return a.connEntryKey(msg), true
	default:
		return nil, false
	}
	a.touch()
	return nil, true
}

// caretRow is the connections key entry: a box this sheet draws INSIDE the row
// it was opened from (connectcaps.go's keyEntryRowLines), which owns every key
// from the moment it opens ([placeSettings.owns]). The frame's composer is the
// search box at rest while it is open, and a caret parked there — blinking in a
// dim sentence at the foot of a sheet whose live box is rows up inside the list
// — is a cursor in a box that is not the one the words are landing in.
//
// THE BOX IS ASKED FOR ITS OWN ANSWER, not a second arithmetic kept here:
// connect.go's keyBoxLines drew the block the row holds and returns the column
// and the line within it, so the hook re-asks it with the same width, indent
// and list window the body's own draw passed ([placeSettings.body] paints the
// list at the frame's width with [connBoxRows] of the list's room) and finds
// the line it answers among the rows the frame just built — the same painted
// string, because both came from the same call.
func (placeSettings) caretRow(a *app, width int, rows []placeRow) (int, int, bool) {
	entry := a.sheet.conn.entry
	if entry == nil {
		if a.sheet.edit != nil {
			return 0, 0, false
		}
		box, placeholder := &a.sheet.query, "type to search"
		if a.sheet.sel != nil {
			box, placeholder = &a.sheet.sel.pick.filter, "type to filter"
		}
		block, column, at := draftBlock(box, a.pal, width-2, 1, placeholder, "")
		if at >= 0 && at < len(block) {
			for j, row := range rows {
				if row.text == " "+block[at] {
					return j, column + 1, true
				}
			}
		}
		return -1, 0, true
	}
	// THE LIST'S ROOM IS THE ROWS LESS THE SHEET'S OWN TWO — the tab bar and
	// the blank under it ([placeSettings.body]) — so the window the box was
	// drawn in can be read back off the rows themselves.
	room := len(rows) - 2
	if room < 1 {
		return 0, 0, false
	}
	block, column, at := keyBoxLines(entry, a.pal, width, overlayIndent, connBoxRows(room))
	if at < 0 || at >= len(block) {
		// A CHOICE HAS NO BOX — the entry is showing its variable list — so
		// there is nowhere for the caret to live and it is hidden rather than
		// parked on the first name, the same law the /connect panel follows
		// (input.go's keyBox case).
		return -1, 0, true
	}
	for j, row := range rows {
		if strings.HasPrefix(row.text, block[at]) {
			return j, column, true
		}
	}
	// The list window scrolled the box off this frame. The caret is hidden
	// rather than parked in the resting search box, which is not the box the
	// person is typing into.
	return -1, 0, true
}
